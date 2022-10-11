package gou

import (
	"strings"

	"github.com/yaoapp/gou/fs"
	"github.com/yaoapp/gou/runtime/bridge"
	"github.com/yaoapp/kun/exception"
)

// FileSystemHandlers 模型运行器
var FileSystemHandlers = map[string]ProcessHandler{
	"readfile":        processReadFile,
	"readfilebuffer":  processReadFileBuffer,
	"writefile":       processWirteFile,
	"writefilebuffer": processWriteFileBuffer,
}

func init() {
	RegisterProcessGroup("fs", FileSystemHandlers)
}

func stor(process *Process) fs.FileSystem {
	name := strings.ToLower(process.Class)
	return fs.MustGet(name)
}

func processReadFile(process *Process) interface{} {
	process.ValidateArgNums(1)
	stor := stor(process)
	file := process.ArgsString(0)
	data, err := fs.ReadFile(stor, file)
	if err != nil {
		exception.New(err.Error(), 500).Throw()
	}
	return string(data)
}

func processReadFileBuffer(process *Process) interface{} {
	process.ValidateArgNums(1)
	stor := stor(process)
	file := process.ArgsString(0)
	data, err := fs.ReadFile(stor, file)
	if err != nil {
		exception.New(err.Error(), 500).Throw()
	}
	return bridge.Uint8Array(data)
}

func processWirteFile(process *Process) interface{} {
	process.ValidateArgNums(3)
	stor := stor(process)
	file := process.ArgsString(0)
	content := process.ArgsString(1)
	pterm := process.ArgsInt(2)
	length, err := fs.WriteFile(stor, file, []byte(content), pterm)
	if err != nil {
		exception.New(err.Error(), 500).Throw()
	}
	return length
}

func processWriteFileBuffer(process *Process) interface{} {
	process.ValidateArgNums(3)
	stor := stor(process)
	file := process.ArgsString(0)
	content := process.Args[1]
	pterm := process.ArgsInt(2)
	data := []byte{}
	switch content.(type) {
	case []byte:
		data = content.([]byte)
		break

	case bridge.Uint8Array:
		data = []byte(content.(bridge.Uint8Array))
		break

	default:
		exception.New("file content type error", 400).Throw()
	}

	length, err := fs.WriteFile(stor, file, data, pterm)
	if err != nil {
		exception.New(err.Error(), 500).Throw()
	}
	return length
}

// func processReadDir(process *Process) interface{} {
// 	return nil
// }

// func processMkdir(process *Process) interface{} {
// 	return nil
// }

// func processMkdirAll(process *Process) interface{} {
// 	return nil
// }

// func processMkdirTemp(process *Process) interface{} {
// 	return nil
// }

// func processChmod(process *Process) interface{} {
// 	return nil
// }

// func processRemove(process *Process) interface{} {
// 	return nil
// }

// func processRemoveAll(process *Process) interface{} {
// 	return nil
// }

// func processMove(process *Process) interface{} {
// 	return nil
// }

// func processCopy(process *Process) interface{} {
// 	return nil
// }

// func processExists(process *Process) interface{} {
// 	return nil
// }

// func processSize(process *Process) interface{} {
// 	return nil
// }

// func processMode(process *Process) interface{} {
// 	return nil
// }

// func processModTime(process *Process) interface{} {
// 	return nil
// }

// func processIsDir(process *Process) interface{} {
// 	return nil
// }

// func processIsFile(process *Process) interface{} {
// 	return nil
// }

// func processBaseName(process *Process) interface{} {
// 	return nil
// }

// func processDirName(process *Process) interface{} {
// 	return nil
// }

// func processExtName(process *Process) interface{} {
// 	return nil
// }

// func processMimeType(process *Process) interface{} {
// 	return nil
// }