package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/log"
)

// Clients 存储所有 Kafka 客户端实例
var Clients = map[string]*Client{}

// mu 保护 Clients 映射以及"停止+创建+注册"序列，
// 避免文件监视热重载与 kafka.start 并发加载时产生多个消费者实例
var mu sync.Mutex

// Client Kafka 客户端
type Client struct {
	Name    string        `json:"name"`
	Brokers []string      `json:"brokers"`
	GroupID string        `json:"group_id"`
	SASL    SASLConfig    `json:"sasl,omitempty"`
	Topics  []TopicConfig `json:"topics"`
	// AutoCreateTopic 订阅/发布的 topic 不存在时自动创建，为空时默认开启
	AutoCreateTopic *bool `json:"auto_create_topic"`
	// StartOffset 消费起始位置："earliest" 从分区最早的消息开始（重启后可补上历史），
	// 其他值或为空时从最新消息开始（只看新日志）
	StartOffset string `json:"start_offset"`
	// RetentionMS 自动创建 topic 时设置的消息保留时长（毫秒），<=0 时沿用 broker 默认值
	RetentionMS int64 `json:"retention_ms"`
	cancel      context.CancelFunc
	ctx         context.Context
	mu          sync.RWMutex
	// writers 按 topic 复用的生产者，避免每次发布都新建连接
	writers  map[string]*kafkago.Writer
	writerMu sync.Mutex
}

// autoCreate 是否在 topic 不存在时自动创建
func (c *Client) autoCreate() bool {
	return c.AutoCreateTopic == nil || *c.AutoCreateTopic
}

// startOffset 消费起始位置，默认从最新消息开始
func (c *Client) startOffset() int64 {
	if strings.EqualFold(c.StartOffset, "earliest") {
		return kafkago.FirstOffset
	}
	return kafkago.LastOffset
}

// SASLConfig SASL 认证配置
type SASLConfig struct {
	Enable    bool   `json:"enable"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Algorithm string `json:"algorithm"` // plain, scram-sha-256, scram-sha-512
}

// TopicConfig 主题订阅配置
type TopicConfig struct {
	Topic   string `json:"topic"`
	Process string `json:"process"`
}

// Load 加载单个 Kafka 配置文件
func Load(file string, name string) (*Client, error) {
	mu.Lock()
	defer mu.Unlock()

	// 同名客户端已存在时先停止，避免热重载/重复调用产生多个消费者
	if client, ok := Clients[name]; ok {
		client.Stop()
		delete(Clients, name)
	}

	data, err := application.App.Read(file)
	if err != nil {
		return nil, err
	}

	client := &Client{Name: name}
	err = application.Parse(file, data, client)
	if err != nil {
		return nil, err
	}

	// 启动客户端
	err = client.start()
	if err != nil {
		return nil, err
	}

	// 注册到全局
	Clients[name] = client
	log.Info("[Kafka] client %s loaded (brokers: %v)", name, client.Brokers)
	return client, nil
}

// start 启动 Kafka 客户端并订阅主题
func (c *Client) start() error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// 验证配置
	if len(c.Brokers) == 0 {
		return fmt.Errorf("brokers is required")
	}
	if len(c.Topics) == 0 {
		return fmt.Errorf("at least one topic is required")
	}

	// 创建传输器(带 SASL 认证)
	dialer := c.createDialer()

	// 订阅的 topic 不存在时先创建。消费者加入消费组时若 topic 还不存在，
	// kafka-go 会分配到 0 个分区，且分区监视器在读取分区失败后直接退出，
	// 之后再也不会重新平衡，表现为「生产正常但消费端永远读不到消息」。
	if c.autoCreate() {
		for _, topic := range c.Topics {
			if err := c.ensureTopic(topic.Topic); err != nil {
				log.Error("[Kafka] client %s: ensure topic %s failed: %v", c.Name, topic.Topic, err)
			}
		}
	}

	// 启动每个主题的消费者
	for _, topic := range c.Topics {
		if topic.Process == "" {
			log.Error("[Kafka] client %s topic %s: process is required", c.Name, topic.Topic)
			continue
		}
		go c.consume(topic, dialer)
	}

	return nil
}

// createDialer 创建 Kafka 拨号器（支持 SASL 认证）
func (c *Client) createDialer() *kafkago.Dialer {
	dialer := &kafkago.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
		TLS:       nil,
	}

	if c.SASL.Enable && c.SASL.Username != "" && c.SASL.Password != "" {
		var mechanism sasl.Mechanism
		var err error

		switch c.SASL.Algorithm {
		case "scram-sha-256":
			mechanism, err = scram.Mechanism(scram.SHA256, c.SASL.Username, c.SASL.Password)
		case "scram-sha-512":
			mechanism, err = scram.Mechanism(scram.SHA512, c.SASL.Username, c.SASL.Password)
		default: // plain
			mechanism = plain.Mechanism{
				Username: c.SASL.Username,
				Password: c.SASL.Password,
			}
		}

		if err != nil {
			log.Error("[Kafka] client %s: create SASL mechanism failed: %v", c.Name, err)
		} else {
			dialer.SASLMechanism = mechanism
			log.Info("[Kafka] client %s: SASL %s authentication enabled", c.Name, c.SASL.Algorithm)
		}
	}

	return dialer
}

// ensureTopic 保证 topic 存在，不存在则创建
// 消费端只读 partition 0，因此固定按 1 分区、1 副本创建
func (c *Client) ensureTopic(topic string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialer := c.createDialer()
	var lastErr error

	for _, broker := range c.Brokers {
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}

		// CreateTopics 是幂等的，topic 已存在时不会报错
		config := kafkago.TopicConfig{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}
		// 带上保留时长，避免新建的 topic 继承 broker 上过短的默认值，
		// 导致历史消息在重启补消费之前就被清理掉
		if c.RetentionMS > 0 {
			config.ConfigEntries = []kafkago.ConfigEntry{
				{ConfigName: "retention.ms", ConfigValue: strconv.FormatInt(c.RetentionMS, 10)},
			}
		}

		err = conn.CreateTopics(config)
		_ = conn.Close()

		if err == nil {
			log.Info("[Kafka] client %s: topic %s is ready", c.Name, topic)
			return nil
		}
		lastErr = err
	}

	return lastErr
}

// consume 消费指定主题的消息
func (c *Client) consume(topic TopicConfig, dialer *kafkago.Dialer) {
	readerConfig := kafkago.ReaderConfig{
		Brokers:     c.Brokers,
		Topic:       topic.Topic,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
		MaxWait:     1 * time.Second,
		StartOffset: c.startOffset(),
		ErrorLogger: kafkago.LoggerFunc(logError),
		// 设置 Partition 为 0，只读取一个分区避免重复
		Partition: 0,
	}

	// 仅当配置了 GroupID 时使用消费者组模式
	if c.GroupID != "" {
		readerConfig.GroupID = c.GroupID
		readerConfig.ReadLagInterval = -1
		readerConfig.WatchPartitionChanges = true
		readerConfig.CommitInterval = 1 * time.Second
	}

	if dialer != nil {
		readerConfig.Dialer = dialer
	}

	reader := kafkago.NewReader(readerConfig)
	defer reader.Close()

	// 简单模式（无消费者组）下 kafka-go 会忽略 ReaderConfig.StartOffset，
	// 每次启动默认从分区开头（FirstOffset）读取，导致重启后重读历史消息。
	// 因此这里按配置手动设置起始偏移。
	if c.GroupID == "" {
		if err := reader.SetOffset(c.startOffset()); err != nil {
			log.Error("[Kafka] client %s: set start offset failed: %v", c.Name, err)
		}
	}

	if c.GroupID != "" {
		log.Info("[Kafka] client %s: start consuming topic %s (group: %s)", c.Name, topic.Topic, c.GroupID)
	} else {
		log.Info("[Kafka] client %s: start consuming topic %s (simple mode)", c.Name, topic.Topic)
	}

	for {
		select {
		case <-c.ctx.Done():
			log.Info("[Kafka] client %s: stop consuming topic %s", c.Name, topic.Topic)
			return
		default:
		}

		msg, err := reader.ReadMessage(c.ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Error("[Kafka] client %s: read message error: %v", c.Name, err)
			time.Sleep(1 * time.Second)
			continue
		}

		// 解析消息负载
		var payloadObj interface{}
		payloadStr := string(msg.Value)
		if err := json.Unmarshal(msg.Value, &payloadObj); err != nil {
			payloadObj = payloadStr
		}

		// 异步调用 process（与 MQTT 一致，传递 topic, payload, timestamp 三个参数）
		go func() {
			p, err := process.Of(topic.Process, msg.Topic, payloadObj, msg.Time.Unix())
			if err != nil {
				log.Error("[Kafka] process %s error: %v", topic.Process, err)
				return
			}
			if err := p.Execute(); err != nil {
				log.Error("[Kafka] process %s execute error: %v", topic.Process, err)
			}
			p.Release()
		}()
	}
}

// Stop 停止客户端
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		log.Info("[Kafka] client %s stopped", c.Name)
	}

	c.writerMu.Lock()
	defer c.writerMu.Unlock()
	for topic, writer := range c.writers {
		if err := writer.Close(); err != nil {
			log.Error("[Kafka] client %s: close writer for topic %s failed: %v", c.Name, topic, err)
		}
		delete(c.writers, topic)
	}
}

// Publish 发送消息到指定主题
func (c *Client) Publish(topic string, key string, payload interface{}) error {
	data, err := encodePayload(payload)
	if err != nil {
		return err
	}
	return c.PublishMessages(topic, kafkago.Message{Key: []byte(key), Value: data})
}

// PublishBatch 批量发送消息到指定主题，keys 与 payloads 一一对应
// 多条消息合并为一次请求写入，避免每条消息一次网络往返，吞吐可提升一个量级
func (c *Client) PublishBatch(topic string, keys []string, payloads []interface{}) error {
	if len(payloads) == 0 {
		return nil
	}

	messages := make([]kafkago.Message, 0, len(payloads))
	for i, payload := range payloads {
		data, err := encodePayload(payload)
		if err != nil {
			return err
		}
		key := ""
		if i < len(keys) {
			key = keys[i]
		}
		messages = append(messages, kafkago.Message{Key: []byte(key), Value: data})
	}

	return c.PublishMessages(topic, messages...)
}

// PublishMessages 发送一条或多条消息到指定主题
func (c *Client) PublishMessages(topic string, messages ...kafkago.Message) error {
	if len(messages) == 0 {
		return nil
	}

	writer := c.writer(topic)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := writer.WriteMessages(ctx, messages...); err != nil {
		return fmt.Errorf("kafka publish failed: %v", err)
	}

	return nil
}

// encodePayload 统一消息体的序列化规则：string/[]byte 原样发送，其它类型转 JSON
func encodePayload(payload interface{}) ([]byte, error) {
	switch v := payload.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return json.Marshal(payload)
	}
}

// writer 获取 topic 对应生产者，同一个 topic 复用同一个 Writer
// 原实现每条消息都新建 Writer，每次都要重新建连并拉取元数据，
// 吞吐被压到 1 条/秒左右
func (c *Client) writer(topic string) *kafkago.Writer {
	c.writerMu.Lock()
	defer c.writerMu.Unlock()

	if c.writers == nil {
		c.writers = map[string]*kafkago.Writer{}
	}
	if writer, ok := c.writers[topic]; ok {
		return writer
	}

	writer := &kafkago.Writer{
		Addr:                   kafkago.TCP(c.Brokers...),
		Topic:                  topic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: c.autoCreate(),
	}

	if c.SASL.Enable && c.SASL.Username != "" && c.SASL.Password != "" {
		writer.Transport = &kafkago.Transport{
			SASL: c.createDialer().SASLMechanism,
			TLS:  &tls.Config{},
		}
	}

	c.writers[topic] = writer
	return writer
}

// Select 获取客户端
func Select(name string) *Client {
	mu.Lock()
	defer mu.Unlock()
	client, ok := Clients[name]
	if !ok {
		return nil
	}
	return client
}

// StopAll 停止所有客户端
func StopAll() {
	mu.Lock()
	defer mu.Unlock()
	for name, client := range Clients {
		client.Stop()
		delete(Clients, name)
	}
}

// logError 用于 kafka-go 的错误日志
func logError(msg string, args ...interface{}) {
	log.Error("[Kafka] "+msg, args...)
}
