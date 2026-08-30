package mqtt

import (
	"fmt"

	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
)

var ProcessHandlers = map[string]process.Handler{
	"publish": processPublish,
	"stop":    processStop,
	"start":   processStart,
}

func init() {
	process.RegisterGroup("mqtt", ProcessHandlers)
}

// processPublish 发布消息
// 参数: clientName, topic, payload, qos(可选), retained(可选)
func processPublish(proc *process.Process) interface{} {
	args := proc.Args
	if len(args) < 3 {
		exception.New("mqtt.publish requires at least 3 arguments: clientName, topic, payload", 400).Throw()
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

	qos := 0
	if len(args) > 3 {
		switch v := args[3].(type) {
		case int:
			qos = v
		case float64:
			qos = int(v)
		}
	}
	retained := false
	if len(args) > 4 {
		switch v := args[4].(type) {
		case bool:
			retained = v
		case int:
			retained = v != 0
		}
	}

	client := Select(clientName)
	if client == nil {
		exception.New("mqtt client not found: %s", 404, clientName).Throw()
	}

	err := client.Publish(topic, byte(qos), retained, payload)
	if err != nil {
		exception.New("error: %v", 500, err).Throw()
	}
	return true
}

// processStop 停止 MQTT 客户端
// 参数: clientName
func processStop(proc *process.Process) interface{} {
	name := proc.ArgsString(0)
	client := Select(name)
	if client == nil {
		return nil
	}
	client.Stop()
	delete(Clients, name)
	log.Info("[MQTT] client %s stopped", name)
	return nil
}

// processStart 启动 MQTT 客户端
// 参数: clientName
func processStart(proc *process.Process) interface{} {
	name := proc.ArgsString(0)
	// 停止已有客户端
	if client := Select(name); client != nil {
		client.Stop()
		delete(Clients, name)
	}
	_, err := Load(fmt.Sprintf("/mqs/%s.mqtt.yao", name), name)
	if err != nil {
		exception.New("mqtt start failed: %v", 500, err).Throw()
	}
	log.Info("[MQTT] client %s started", name)
	return true
}
