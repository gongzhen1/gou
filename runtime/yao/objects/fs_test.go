package objects

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/gou/fs"
	"github.com/yaoapp/gou/runtime/yao/bridge"
	"rogchap.com/v8go"
)

func TestFSObjectReadFile(t *testing.T) {

	f := testFsFiles(t)
	data := testFsMakeF1(t)

	initTestEngine()
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	fs := &FSOBJ{}
	global := v8go.NewObjectTemplate(iso)
	global.Set("FS", fs.ExportFunction(iso))

	ctx := v8go.NewContext(iso, global)
	defer ctx.Close()

	// ReadFile
	v, err := ctx.RunScript(fmt.Sprintf(`
	function ReadFile() {
		var fs = new FS("system")
		var data = fs.ReadFile("%s");
		return data
	}
	ReadFile()
	`, f["F1"]), "")

	if err != nil {
		t.Fatal(err)
	}

	res, err := bridge.ToInterface(v)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, string(data), res)

	// ReadFileBuffer
	v, err = ctx.RunScript(fmt.Sprintf(`
		function ReadFileBuffer() {
			var fs = new FS("system")
			var data = fs.ReadFileBuffer("%s");
			return data
		}
		ReadFileBuffer()
		`, f["F1"]), "")

	assert.True(t, v.IsUint8Array())

	res, err = bridge.ToInterface(v)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, data, res)

	// ReadFileBuffer
	v, err = ctx.RunScript(fmt.Sprintf(`
	function ReadFileBuffer() {
		var fs = new FS("system")
		var data = fs.ReadFileBuffer("%s");
		return data
	}
	ReadFileBuffer()
	`, f["F1"]), "")

	if err != nil {
		t.Fatal(err)
	}

	res, err = bridge.ToInterface(v)
	assert.True(t, v.IsUint8Array())
	assert.Equal(t, data, res)
}

func TestFSObjectWriteFile(t *testing.T) {

	f := testFsFiles(t)
	data := testFsMakeF1(t)

	initTestEngine()
	iso := v8go.NewIsolate()
	defer iso.Dispose()

	fs := &FSOBJ{}
	global := v8go.NewObjectTemplate(iso)
	global.Set("FS", fs.ExportFunction(iso))

	ctx := v8go.NewContext(iso, global)
	defer ctx.Close()

	// WriteFile
	v, err := ctx.RunScript(fmt.Sprintf(`
	function WriteFile() {
		var fs = new FS("system")
		return fs.WriteFile("%s", "%s", 0644);
	}
	WriteFile()
	`, f["F1"], string(data)), "")

	if err != nil {
		t.Fatal(err)
	}

	res, err := bridge.ToInterface(v)
	if err != nil {
		t.Fatal(err)
	}

	// WriteFileBuffer
	v, err = ctx.RunScript(fmt.Sprintf(`
		function WriteFileBuffer() {
			var fs = new FS("system")
			var data = fs.ReadFileBuffer("%s")
			return fs.WriteFileBuffer("%s", data, 0644);
		}
		WriteFileBuffer()
		`, f["F1"], f["F2"]), "")

	if err != nil {
		t.Fatal(err)
	}

	res, err = bridge.ToInterface(v)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(data), res)

}

func testFsMakeF1(t *testing.T) []byte {
	data := testFsData(t)
	f := testFsFiles(t)

	// Write
	_, err := fs.WriteFile(fs.FileSystems["system"], f["F1"], data, 0644)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func testFsData(t *testing.T) []byte {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	rand.Seed(time.Now().UnixNano())
	n := rand.Intn(10) + 1
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return []byte(fmt.Sprintf("HELLO WORLD %s", string(b)))
}

func testFsFiles(t *testing.T) map[string]string {

	root := filepath.Join(os.Getenv("GOU_TEST_APP_ROOT"), "data")
	return map[string]string{
		"root":     root,
		"F1":       filepath.Join(root, "f1.file"),
		"F2":       filepath.Join(root, "f2.file"),
		"F3":       filepath.Join(root, "f3.js"),
		"D1_F1":    filepath.Join(root, "d1", "f1.file"),
		"D1_F2":    filepath.Join(root, "d1", "f2.file"),
		"D2_F1":    filepath.Join(root, "d2", "f1.file"),
		"D2_F2":    filepath.Join(root, "d2", "f2.file"),
		"D1_D2_F1": filepath.Join(root, "d1", "d2", "f1.file"),
		"D1_D2_F2": filepath.Join(root, "d1", "d2", "f2.file"),
		"D1":       filepath.Join(root, "d1"),
		"D2":       filepath.Join(root, "d2"),
		"D1_D2":    filepath.Join(root, "d1", "d2"),
	}

}

func testFsClear(stor fs.FileSystem, t *testing.T) {

	root := filepath.Join(os.Getenv("GOU_TEST_APP_ROOT"), "data")
	err := os.RemoveAll(root)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	err = fs.MkdirAll(stor, root, int(os.ModePerm))
	if err != nil {
		t.Fatal(err)
	}
}