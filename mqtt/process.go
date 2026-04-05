package mqtt

import (
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

var ProcessHandlers = map[string]process.Handler{
	"publish": processPublish,
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
