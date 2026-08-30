package kafka

import (
	"fmt"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

var ProcessHandlers = map[string]process.Handler{
	"publish":      processPublish,
	"createTopic":  processCreateTopic,
	"listTopics":   processListTopics,
	"deleteTopic":  processDeleteTopic,
}

func init() {
	process.RegisterGroup("kafka", ProcessHandlers)
}

// processPublish 发布消息到 Kafka
// 参数: clientName, topic, payload, key(可选)
func processPublish(proc *process.Process) interface{} {
	args := proc.Args
	if len(args) < 3 {
		exception.New("kafka.publish requires at least 3 arguments: clientName, topic, payload", 400).Throw()
	}

	clientName, ok := args[0].(string)
	if !ok {
		exception.New("clientName must be string", 400).Throw()
	}
	topic, ok := args[1].(string)
	if !ok {
		exception.New("topic must be string", 400).Throw()
	}
	payload := args[2]

	key := ""
	if len(args) > 3 {
		if k, ok := args[3].(string); ok {
			key = k
		}
	}

	client := Select(clientName)
	if client == nil {
		exception.New("kafka client not found: %s", 404, clientName).Throw()
	}

	err := client.Publish(topic, key, payload)
	if err != nil {
		exception.New("error: %v", 500, err).Throw()
	}
	return true
}

// processCreateTopic 创建 Kafka 主题
// 参数: clientName, topic, partitions(可选,默认1), replicationFactor(可选,默认1)
func processCreateTopic(proc *process.Process) interface{} {
	args := proc.Args
	if len(args) < 2 {
		exception.New("kafka.createTopic requires at least 2 arguments: clientName, topic", 400).Throw()
	}

	clientName, ok := args[0].(string)
	if !ok {
		exception.New("clientName must be string", 400).Throw()
	}
	topic, ok := args[1].(string)
	if !ok {
		exception.New("topic must be string", 400).Throw()
	}

	partitions := 1
	if len(args) > 2 {
		if p, ok := args[2].(int); ok {
			partitions = p
		}
	}

	replicationFactor := 1
	if len(args) > 3 {
		if r, ok := args[3].(int); ok {
			replicationFactor = r
		}
	}

	client := Select(clientName)
	if client == nil {
		exception.New("kafka client not found: %s", 404, clientName).Throw()
	}

	conn, err := kafkago.Dial("tcp", client.Brokers[0])
	if err != nil {
		exception.New("dial broker failed: %v", 500, err).Throw()
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		exception.New("get controller failed: %v", 500, err).Throw()
	}

	controllerConn, err := kafkago.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		exception.New("dial controller failed: %v", 500, err).Throw()
	}
	defer controllerConn.Close()

	topicConfig := kafkago.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	}

	err = controllerConn.CreateTopics(topicConfig)
	if err != nil {
		exception.New("create topic failed: %v", 500, err).Throw()
	}

	return map[string]interface{}{
		"topic":    topic,
		"partitions": partitions,
		"replication_factor": replicationFactor,
	}
}

// processListTopics 列出 Kafka 主题
// 参数: clientName
func processListTopics(proc *process.Process) interface{} {
	args := proc.Args
	if len(args) < 1 {
		exception.New("kafka.listTopics requires 1 argument: clientName", 400).Throw()
	}

	clientName, ok := args[0].(string)
	if !ok {
		exception.New("clientName must be string", 400).Throw()
	}

	client := Select(clientName)
	if client == nil {
		exception.New("kafka client not found: %s", 404, clientName).Throw()
	}

	conn, err := kafkago.Dial("tcp", client.Brokers[0])
	if err != nil {
		exception.New("dial broker failed: %v", 500, err).Throw()
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		exception.New("list topics failed: %v", 500, err).Throw()
	}

	topicMap := map[string]int{}
	for _, p := range partitions {
		topicMap[p.Topic]++
	}

	topics := []map[string]interface{}{}
	for name, count := range topicMap {
		topics = append(topics, map[string]interface{}{
			"topic":      name,
			"partitions": count,
		})
	}

	return topics
}

// processDeleteTopic 删除 Kafka 主题
// 参数: clientName, topic
func processDeleteTopic(proc *process.Process) interface{} {
	args := proc.Args
	if len(args) < 2 {
		exception.New("kafka.deleteTopic requires at least 2 arguments: clientName, topic", 400).Throw()
	}

	clientName, ok := args[0].(string)
	if !ok {
		exception.New("clientName must be string", 400).Throw()
	}
	topic, ok := args[1].(string)
	if !ok {
		exception.New("topic must be string", 400).Throw()
	}

	client := Select(clientName)
	if client == nil {
		exception.New("kafka client not found: %s", 404, clientName).Throw()
	}

	conn, err := kafkago.Dial("tcp", client.Brokers[0])
	if err != nil {
		exception.New("dial broker failed: %v", 500, err).Throw()
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		exception.New("get controller failed: %v", 500, err).Throw()
	}

	controllerConn, err := kafkago.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		exception.New("dial controller failed: %v", 500, err).Throw()
	}
	defer controllerConn.Close()

	err = controllerConn.DeleteTopics(topic)
	if err != nil {
		exception.New("delete topic failed: %v", 500, err).Throw()
	}

	return true
}