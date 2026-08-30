package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
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

// Client Kafka 客户端
type Client struct {
	Name    string        `json:"name"`
	Brokers []string      `json:"brokers"`
	GroupID string        `json:"group_id"`
	SASL    SASLConfig    `json:"sasl,omitempty"`
	Topics  []TopicConfig `json:"topics"`
	cancel  context.CancelFunc
	ctx     context.Context
	mu      sync.RWMutex
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

// consume 消费指定主题的消息
func (c *Client) consume(topic TopicConfig, dialer *kafkago.Dialer) {
	readerConfig := kafkago.ReaderConfig{
		Brokers:     c.Brokers,
		Topic:       topic.Topic,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
		MaxWait:     1 * time.Second,
		StartOffset: kafkago.LastOffset,
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
			if err == context.Canceled {
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
}

// Publish 发送消息到指定主题
func (c *Client) Publish(topic string, key string, payload interface{}) error {
	var data []byte
	switch v := payload.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	writer := &kafkago.Writer{
		Addr:     kafkago.TCP(c.Brokers...),
		Topic:    topic,
		Balancer: &kafkago.LeastBytes{},
	}

	if c.SASL.Enable && c.SASL.Username != "" && c.SASL.Password != "" {
		writer.Transport = &kafkago.Transport{
			SASL: c.createDialer().SASLMechanism,
			TLS:  &tls.Config{},
		}
	}

	defer writer.Close()

	msg := kafkago.Message{
		Key:   []byte(key),
		Value: data,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := writer.WriteMessages(ctx, msg)
	if err != nil {
		return fmt.Errorf("kafka publish failed: %v", err)
	}

	return nil
}

// Select 获取客户端
func Select(name string) *Client {
	client, ok := Clients[name]
	if !ok {
		return nil
	}
	return client
}

// StopAll 停止所有客户端
func StopAll() {
	for name, client := range Clients {
		client.Stop()
		delete(Clients, name)
	}
}

// logError 用于 kafka-go 的错误日志
func logError(msg string, args ...interface{}) {
	log.Error("[Kafka] "+msg, args...)
}
