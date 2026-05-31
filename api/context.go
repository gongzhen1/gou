package api

import (
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/yaoapp/gou/process"
)

var (
	contextMap        = sync.Map{}
	contextID  uint64 = 0
	contextMtx sync.Mutex
)

func init() {
	// Request methods
	process.Register("context.req.url", GetURL)
	process.Register("context.req.method", GetMethod)
	process.Register("context.req.headers", GetHeaders)
	process.Register("context.req.header", GetHeader)
	process.Register("context.req.query", GetQuery)
	process.Register("context.req.queryParam", GetQueryParam)
	process.Register("context.req.cookie", GetCookie)
	process.Register("context.req.get", GetRequest)

	// Response methods
	process.Register("context.res.setCookie", SetCookie)
	process.Register("context.res.setStatus", SetStatusCode)
	process.Register("context.res.setHeader", SetHeader)
	process.Register("context.res.addHeader", AddHeader)
	process.Register("context.res.get", GetResponse)
}

func storeGinContext(c *gin.Context) uint64 {
	contextMtx.Lock()
	defer contextMtx.Unlock()
	contextID++
	id := contextID
	contextMap.Store(id, c)
	return id
}

func getGinContextByID(id uint64) *gin.Context {
	v, ok := contextMap.Load(id)
	if !ok {
		return nil
	}
	c, ok := v.(*gin.Context)
	if !ok {
		return nil
	}
	return c
}

func removeGinContext(id uint64) {
	contextMap.Delete(id)
}

func CleanupContext(p *process.Process) {
	if p == nil || p.Global == nil {
		return
	}
	v, has := p.Global["__context_id"]
	if !has {
		return
	}
	id, ok := v.(uint64)
	if !ok {
		return
	}
	removeGinContext(id)
	delete(p.Global, "__context_id")
}

func getGinContext(p *process.Process) *gin.Context {
	if p == nil || p.Global == nil {
		return nil
	}
	v, has := p.Global["__context_id"]
	if !has {
		return nil
	}

	var id uint64
	switch val := v.(type) {
	case uint64:
		id = val
	case float64:
		id = uint64(val)
	default:
		return nil
	}

	return getGinContextByID(id)
}

func GetURL(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil || ctx.Request == nil {
		return nil
	}
	return ctx.Request.URL.String()
}

func GetMethod(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil || ctx.Request == nil {
		return ""
	}
	return ctx.Request.Method
}

func GetHeaders(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil || ctx.Request == nil {
		return nil
	}
	headers := map[string][]string{}
	for key, values := range ctx.Request.Header {
		headers[key] = values
	}
	return headers
}

func GetHeader(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return ""
	}
	if len(p.Args) == 0 {
		return ""
	}
	key, ok := p.Args[0].(string)
	if !ok {
		return ""
	}
	return ctx.GetHeader(key)
}

func GetQuery(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil || ctx.Request == nil {
		return nil
	}
	query := map[string][]string{}
	for key, values := range ctx.Request.URL.Query() {
		query[key] = values
	}
	return query
}

func GetQueryParam(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return ""
	}
	if len(p.Args) == 0 {
		return ""
	}
	key, ok := p.Args[0].(string)
	if !ok {
		return ""
	}
	return ctx.Query(key)
}

func GetCookie(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return ""
	}
	if len(p.Args) == 0 {
		return ""
	}
	name, ok := p.Args[0].(string)
	if !ok {
		return ""
	}
	value, _ := ctx.Cookie(name)
	return value
}

func SetCookie(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return nil
	}
	if len(p.Args) < 2 {
		return nil
	}

	name, ok := p.Args[0].(string)
	if !ok {
		return nil
	}
	value, ok := p.Args[1].(string)
	if !ok {
		return nil
	}

	maxAge := 0
	if len(p.Args) > 2 {
		maxAge, _ = p.Args[2].(int)
	}

	path := "/"
	if len(p.Args) > 3 {
		path, _ = p.Args[3].(string)
	}

	domain := ""
	if len(p.Args) > 4 {
		domain, _ = p.Args[4].(string)
	}

	secure := false
	if len(p.Args) > 5 {
		secure, _ = p.Args[5].(bool)
	}

	httpOnly := true
	if len(p.Args) > 6 {
		httpOnly, _ = p.Args[6].(bool)
	}

	ctx.SetCookie(name, value, maxAge, path, domain, secure, httpOnly)
	return nil
}

func SetStatusCode(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return nil
	}
	if len(p.Args) == 0 {
		return nil
	}
	code, ok := p.Args[0].(int)
	if !ok {
		return nil
	}
	ctx.Writer.WriteHeader(code)
	return nil
}

func SetHeader(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return nil
	}
	if len(p.Args) < 2 {
		return nil
	}
	key, ok := p.Args[0].(string)
	if !ok {
		return nil
	}
	value, ok := p.Args[1].(string)
	if !ok {
		return nil
	}
	ctx.Writer.Header().Set(key, value)
	return nil
}

func AddHeader(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return nil
	}
	if len(p.Args) < 2 {
		return nil
	}
	key, ok := p.Args[0].(string)
	if !ok {
		return nil
	}
	value, ok := p.Args[1].(string)
	if !ok {
		return nil
	}
	ctx.Writer.Header().Add(key, value)
	return nil
}

func GetRequest(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil || ctx.Request == nil {
		return nil
	}
	req := ctx.Request
	return map[string]interface{}{
		"method":     req.Method,
		"url":        req.URL.String(),
		"proto":      req.Proto,
		"host":       req.Host,
		"remoteAddr": req.RemoteAddr,
		"requestURI": req.RequestURI,
	}
}

func GetResponse(p *process.Process) interface{} {
	ctx := getGinContext(p)
	if ctx == nil {
		return nil
	}
	return map[string]interface{}{
		"statusCode": ctx.Writer.Status(),
	}
}
