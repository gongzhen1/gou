package mqtt

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqttlib "github.com/eclipse/paho.mqtt.golang"
	"github.com/yaoapp/gou/application"
	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/log"
)

// Clients 存储所有 MQTT 客户端实例
var Clients = map[string]*Client{}

// Client MQTT 客户端
type Client struct {
	Name       string      `json:"name"`
	Broker     string      `json:"broker"`
	ClientID   string      `json:"client_id"`
	Username   string      `json:"username"`
	Password   string      `json:"password"`
	Subscribes []Subscribe `json:"subscribes"`
	client     mqttlib.Client
	stopped    bool
	mu         sync.RWMutex
}

// Subscribe 订阅配置
type Subscribe struct {
	Topic   string `json:"topic"`
	QoS     int    `json:"qos"`
	Process string `json:"process"`
}

// Load 加载单个 MQTT 配置文件
func Load(file string, name string) (*Client, error) {
	// 同名客户端已存在时先停止，避免热重载/重复调用产生多个客户端
	if client := Select(name); client != nil {
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

	// 解析 client_id 中的 $ENV.xxx，支持与前后文拼接（如 websocket_$ENV.HOSTNAME）
	// 多容器共享同一份配置时，可用容器独有的环境变量保证 client_id 唯一，避免互相踢线
	client.ClientID = helper.EnvReplace(client.ClientID)

	// 设置默认值
	if client.ClientID == "" {
		client.ClientID = fmt.Sprintf("yao_mqtt_%s_%d", name, time.Now().Unix())
	}

	// 启动客户端
	err = client.start()
	if err != nil {
		return nil, err
	}

	// 注册到全局
	Clients[name] = client
	log.Info("[MQTT] client %s loaded (broker: %s)", name, client.Broker)
	return client, nil
}

// start 启动 MQTT 客户端并订阅主题
func (c *Client) start() error {
	opts := mqttlib.NewClientOptions()
	opts.AddBroker(c.Broker)
	opts.SetClientID(c.ClientID)
	if c.Username != "" {
		opts.SetUsername(c.Username)
	}
	if c.Password != "" {
		opts.SetPassword(c.Password)
	}
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetConnectTimeout(30 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetOnConnectHandler(func(client mqttlib.Client) {
		// 已停止的客户端（禁用）即使自动重连也不重新订阅，从而不再接收消息
		c.mu.RLock()
		stopped := c.stopped
		c.mu.RUnlock()
		if stopped {
			return
		}
		log.Info("[MQTT] client %s connected", c.Name)
		// 重连后重新订阅
		for _, sub := range c.Subscribes {
			c.subscribe(sub)
		}
	})

	client := mqttlib.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	c.client = client

	// 订阅主题
	for _, sub := range c.Subscribes {
		if err := c.subscribe(sub); err != nil {
			log.Error("[MQTT] client %s subscribe %s failed: %v", c.Name, sub.Topic, err)
		}
	}
	return nil
}

// subscribe 订阅单个主题
func (c *Client) subscribe(sub Subscribe) error {
	if sub.Process == "" {
		return fmt.Errorf("process is required for topic %s", sub.Topic)
	}
	handler := func(client mqttlib.Client, msg mqttlib.Message) {
		// 解析 payload
		var payloadObj interface{}
		if err := json.Unmarshal(msg.Payload(), &payloadObj); err != nil {
			payloadObj = string(msg.Payload())
		}
		// 异步调用 process
		go func() {
			p, err := process.Of(sub.Process, msg.Topic(), payloadObj, time.Now().Unix())
			if err != nil {
				log.Error("[MQTT] process %s error: %v", sub.Process, err)
				return
			}
			if err := p.Execute(); err != nil {
				log.Error("[MQTT] process %s execute error: %v", sub.Process, err)
			}
			p.Release()
		}()
	}
	token := c.client.Subscribe(sub.Topic, byte(sub.QoS), handler)
	token.Wait()
	return token.Error()
}

// Stop 停止客户端
func (c *Client) Stop() {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
		log.Info("[MQTT] client %s stopped", c.Name)
	}
}

// Publish 发送消息
func (c *Client) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("client %s is not connected", c.Name)
	}
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
	token := c.client.Publish(topic, qos, retained, data)
	token.Wait()
	return token.Error()
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
