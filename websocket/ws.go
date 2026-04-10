package websocket

import (
	"fmt"
	"io"
	"os"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/yaoapp/gou/helper"
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
	"github.com/yaoapp/kun/log"
)

func init() {
	process.Register("websocket.Write", processWrite)
	process.Register("websocket.Close", processClose)
	process.Register("websocket.Broadcast", processBroadcast)
	process.Register("websocket.Direct", processDirect)
	process.Register("websocket.Online", processOnline)
	process.Register("websocket.Clients", processClients)
}

// WebSockets websockets loaded (Alpha)
var WebSockets = map[string]*WebSocket{}

// LoadWebSocket load websockets client
func LoadWebSocket(source string, name string) (*WebSocket, error) {
	var input io.Reader = nil
	if strings.HasPrefix(source, "file://") {
		filename := strings.TrimPrefix(source, "file://")
		file, err := os.Open(filename)
		if err != nil {
			exception.Err(err, 400).Throw()
		}
		defer file.Close()
		input = file
	} else {
		input = strings.NewReader(source)
	}

	config, err := helper.ReadFile(input)
	if err != nil {
		return nil, err
	}

	ws := WebSocket{}
	err = jsoniter.Unmarshal(config, &ws)
	if err != nil {
		return nil, err
	}

	handers := Handlers{
		Connected: ws.onConnected,
		Closed:    ws.onClosed,
		Data:      ws.onData,
		Error:     ws.onError,
	}

	ws.WSClientOption.Name = name
	ws.Client = NewWSClient(ws.WSClientOption, handers)
	WebSockets[name] = &ws
	return WebSockets[name], nil
}

// SelectWebSocket Get WebSocket client by name
func SelectWebSocket(name string) *WebSocket {
	ws, has := WebSockets[name]
	if !has {
		exception.New("WebSocket Client:%s does not load", 500, name).Throw()
	}
	return ws
}

// Open open the websocket connection
func (ws *WebSocket) Open(args ...string) error {
	if len(args) > 0 {
		ws.Client.SetURL(args[0])
	}
	if len(args) > 1 {
		ws.Client.SetProtocols(args[1:]...)
	}

	return ws.Client.Open()
}

// processWrite WebSocket client send message to server
func processWrite(process *process.Process) interface{} {
	process.ValidateArgNums(2)
	name := process.ArgsString(0)
	message := process.ArgsString(1)
	ws := SelectWebSocket(name)
	err := ws.Client.Write([]byte(message))
	if err != nil {
		return map[string]interface{}{"code": 500, "message": err.Error()}
	}
	return nil
}

// processClose WebSocket client close the connection
func processClose(process *process.Process) interface{} {
	process.ValidateArgNums(1)
	name := process.ArgsString(0)
	ws := SelectWebSocket(name)
	err := ws.Client.Close()
	if err != nil {
		return map[string]interface{}{"code": 500, "message": err.Error()}
	}
	return nil
}

func (ws WebSocket) onConnected(option WSClientOption) error {
	if ws.Event.Connected == "" {
		return nil
	}

	p, err := process.Of(ws.Event.Connected, option)
	if err != nil {
		return err
	}

	_, err = p.Exec()
	return err
}

func (ws WebSocket) onClosed(data []byte, err error) []byte {
	if ws.Event.Closed == "" {
		return nil
	}

	var msg = ""
	if data != nil {
		msg = string(data)
	}

	errstr := ""
	if err != nil {
		errstr = err.Error()
	}

	p, err := process.Of(ws.Event.Closed, msg, errstr)
	if err != nil {
		log.Error("ws.Event.Closed Error: %s", err)
		return nil
	}

	res, err := p.Exec()
	if err != nil {
		log.Error("ws.Event.Closed Error: %s", err)
		return nil
	}

	return ws.toBytes(res, "ws.Event.Closed")
}

func (ws WebSocket) onData(data []byte, recvLen int) ([]byte, error) {
	if ws.Event.Data == "" {
		return nil, nil
	}
	p, err := process.Of(ws.Event.Data, string(data), recvLen)
	if err != nil {
		return nil, err
	}
	res, err := p.Exec()
	if err != nil {
		return nil, err
	}
	return ws.toBytes(res, "ws.Event.Data"), nil
}

func (ws WebSocket) onError(err error) {
	if ws.Event.Error == "" {
		return
	}

	p, err := process.Of(ws.Event.Error, err.Error())
	if err != nil {
		log.Error("ws.Event.Error Error: %s", err.Error())
	}

	_, err = p.Exec()
	if err != nil {
		log.Error("ws.Event.Error Error: %s", err.Error())
	}
}

func (ws WebSocket) toBytes(value interface{}, name string) []byte {
	if value == nil {
		return nil
	}

	switch value.(type) {
	case []byte:
		return value.([]byte)

	case string:
		if value.(string) == "" {
			return nil
		}
		return []byte(value.(string))

	default:
		v, err := jsoniter.Marshal(value)
		if err != nil {
			log.Error("%s Error: %s", name, err.Error())
		}
		return v
	}
}

// LoadWebSocketServer load websocket servers
func LoadWebSocketServer(source string, name string) (*Upgrader, error) {
	var input io.Reader = nil
	if strings.HasPrefix(source, "file://") {
		filename := strings.TrimPrefix(source, "file://")
		file, err := os.Open(filename)
		if err != nil {
			exception.Err(err, 400).Throw()
		}
		defer file.Close()
		input = file
	} else {
		input = strings.NewReader(source)
	}

	config, err := helper.ReadFile(input)
	if err != nil {
		return nil, err
	}
	ws, err := NewUpgrader(name, config)
	if err != nil {
		return nil, err
	}

	// SetHandler
	ws.SetHandler(func(message []byte, client int) ([]byte, error) {
		p, err := process.Of(ws.Process, string(message), client)
		if err != nil {
			log.Error("Websocket Handler: %s %s", name, err.Error())
			return nil, err
		}
		response, err := p.Exec()
		if err != nil {
			log.Error("Websocket Handler Result: %s %s", name, err.Error())
			return nil, err
		}
		if response == nil {
			return nil, nil
		}

		switch response.(type) {
		case string:
			return []byte(response.(string)), nil
		case []byte:
			return response.([]byte), nil
		default:
			message := fmt.Sprintf("Websocket: %s handler response message dose not support(%#v)", name, response)
			log.Error(message)
			return nil, fmt.Errorf(message)
		}
	})

	return ws, nil
}

// NewProcess 创建运行器
func NewProcess(name string, args ...interface{}) *process.Process {
	process := &process.Process{Name: name, Args: args, Global: map[string]interface{}{}}
	return process
}

// SelectWebSocketServer Get WebSocket server by name
func SelectWebSocketServer(name string) *Upgrader {
	ws, has := Upgraders[name]
	if !has {
		exception.New("WebSocket:%s does not load", 500, name).Throw()
	}
	return ws
}

// processBroadcast WebSocket Server broadcast the message
// Broadcast name hello
func processBroadcast(process *process.Process) interface{} {
	process.ValidateArgNums(2)
	name := process.ArgsString(0)
	message := process.ArgsString(1)
	ws := SelectWebSocketServer(name)
	ws.Broadcast([]byte(message))
	return nil
}

// processDirect send the message to the client directly
// Direct name 32 hello
func processDirect(process *process.Process) interface{} {
	process.ValidateArgNums(3)
	name := process.ArgsString(0)
	id := process.ArgsInt(1)
	message := process.ArgsString(2)
	ws := SelectWebSocketServer(name)
	ws.Direct(uint32(id), []byte(message))
	return nil
}

// processOnline count the online client's nums
// Online name
func processOnline(process *process.Process) interface{} {
	process.ValidateArgNums(1)
	name := process.ArgsString(0)
	ws := SelectWebSocketServer(name)
	return ws.Online()
}

// processClients return the online clients
// Clients name
func processClients(process *process.Process) interface{} {
	process.ValidateArgNums(1)
	name := process.ArgsString(0)
	ws := SelectWebSocketServer(name)
	return ws.Clients()
}
