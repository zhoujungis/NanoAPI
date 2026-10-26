package curl

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"nanoapi/internal/model"
	"nanoapi/internal/proxy"
)

const maxCurlBodyFileBytes = 20 << 20

// CurlImport 是 cURL 导入结果。
type CurlImport struct {
	Request  model.Request `json:"request"`
	Warnings []string      `json:"warnings,omitempty"`
}

// CurlExport 是 cURL 导出结果。
type CurlExport struct {
	Command  string   `json:"command"`
	Warnings []string `json:"warnings,omitempty"`
}

// Parse 把 cURL 命令解析成请求草稿。
func Parse(command string) (*CurlImport, error) {
	return parseCurl(command)
}

// Build 把请求转换成 Bash 风格的 cURL 命令。
func Build(req model.Request, settings model.Settings) (*CurlExport, error) {
	command, warnings, err := buildCurlCommand(req, settings)
	if err != nil {
		return nil, err
	}
	return &CurlExport{Command: command, Warnings: warnings}, nil
}

var curlSupportedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

var curlShortValueFlags = map[byte]bool{
	'X': true, 'H': true, 'd': true, 'F': true, 'u': true,
	'A': true, 'b': true, 'e': true, 'o': true, 'x': true,
	'm': true, 'T': true, 'w': true, 'K': true, 'c': true,
	'D': true, 'E': true, 'r': true, 'y': true, 'Y': true,
	'z': true, 'C': true, 'P': true, 'Q': true, 'U': true,
}

var curlLongValueFlags = map[string]bool{
	"request": true, "header": true, "data": true, "data-raw": true,
	"data-binary": true, "data-urlencode": true, "json": true,
	"form": true, "form-string": true, "user": true, "user-agent": true,
	"cookie": true, "cookie-jar": true, "referer": true, "url": true,
	"output": true, "proxy": true, "proxy-user": true, "proxy-header": true,
	"noproxy": true, "preproxy": true, "socks4": true, "socks4a": true,
	"socks5": true, "socks5-hostname": true, "max-time": true,
	"connect-timeout": true, "retry": true, "retry-delay": true,
	"retry-max-time": true, "upload-file": true, "cert": true, "key": true,
	"cacert": true, "config": true, "resolve": true, "interface": true,
	"write-out": true, "dump-header": true, "range": true, "limit-rate": true,
	"max-filesize": true, "oauth2-bearer": true, "aws-sigv4": true,
	"http-version": true, "tls-max": true, "ciphers": true, "dns-servers": true,
	"connect-to": true, "local-port": true, "unix-socket": true,
	"abstract-unix-socket": true, "stderr": true, "trace": true,
	"trace-ascii": true, "trace-time": true,
}

var curlLongBoolFlags = map[string]bool{
	"compressed": true, "location": true, "location-trusted": true,
	"insecure": true, "head": true, "get": true, "silent": true,
	"show-error": true, "verbose": true, "fail": true, "fail-with-body": true,
	"include": true, "no-buffer": true, "remote-name": true,
	"remote-name-all": true, "globoff": true, "path-as-is": true,
	"http1.1": true, "http2": true, "http2-prior-knowledge": true,
	"tcp-nodelay": true, "raw": true, "progress-bar": true,
	"styled-output": true, "no-progress-meter": true,
}

type curlParser struct {
	method      string
	methodSet   bool
	url         string
	headers     []model.KV
	data        []string
	forms       []model.KV
	files       []model.FileField
	head        bool
	getMode     bool
	jsonMode    bool
	contentType string
	basicUser   string
	basicPass   string
	basicSet    bool
	bearer      string
	bearerSet   bool
	warnings    []string
}

func parseCurl(command string) (*CurlImport, error) {
	tokens, err := splitCurlCommand(command)
	if err != nil {
		return nil, err
	}
	tokens = trimCurlPrefix(tokens)
	if len(tokens) == 0 {
		return nil, errors.New("请输入 cURL 命令")
	}

	p := &curlParser{}
	if err := p.parseTokens(tokens); err != nil {
		return nil, err
	}
	return p.result()
}

// splitCurlCommand 按 shell 规则拆分参数，支持引号、反斜杠续行和 Windows 的 ^ 续行。
func splitCurlCommand(input string) ([]string, error) {
	var tokens []string
	var b strings.Builder
	inSingle, inDouble := false, false
	hasToken := false

	flush := func() {
		if hasToken {
			tokens = append(tokens, b.String())
			b.Reset()
			hasToken = false
		}
	}

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		c := runes[i]

		if inSingle {
			if c == '\'' {
				inSingle = false
			} else {
				b.WriteRune(c)
			}
			continue
		}

		if inDouble {
			if c == '"' {
				inDouble = false
				hasToken = true
				continue
			}
			if c == '\\' && i+1 < len(runes) {
				next := runes[i+1]
				if next == '\n' {
					i++
					continue
				}
				if next == '\r' && i+2 < len(runes) && runes[i+2] == '\n' {
					i += 2
					continue
				}
				switch next {
				case '"', '\\', '$', '`':
					b.WriteRune(next)
					i++
					hasToken = true
					continue
				}
			}
			b.WriteRune(c)
			hasToken = true
			continue
		}

		switch c {
		case '\'':
			inSingle = true
			hasToken = true
		case '"':
			inDouble = true
			hasToken = true
		case '\\':
			if i+1 < len(runes) {
				next := runes[i+1]
				if next == '\n' {
					i++
					continue
				}
				if next == '\r' && i+2 < len(runes) && runes[i+2] == '\n' {
					i += 2
					continue
				}
				if next == ' ' || next == '\t' || next == '\'' || next == '"' ||
					next == '\\' || next == '$' || next == '`' {
					b.WriteRune(next)
					i++
					hasToken = true
					continue
				}
			}
			// 非特殊字符前保留反斜杠，兼容未加引号的 Windows 路径。
			b.WriteRune('\\')
			hasToken = true
		case '^':
			if i+1 < len(runes) {
				next := runes[i+1]
				if next == '\n' {
					i++
					continue
				}
				if next == '\r' && i+2 < len(runes) && runes[i+2] == '\n' {
					i += 2
					continue
				}
			}
			b.WriteRune('^')
			hasToken = true
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			b.WriteRune(c)
			hasToken = true
		}
	}

	if inSingle || inDouble {
		return nil, errors.New("cURL 命令中的引号未闭合")
	}
	flush()
	return tokens, nil
}

func trimCurlPrefix(tokens []string) []string {
	if len(tokens) > 0 && tokens[0] == "$" {
		tokens = tokens[1:]
	}
	if len(tokens) > 0 {
		first := strings.ToLower(strings.TrimSpace(tokens[0]))
		if first == "curl" || first == "curl.exe" {
			tokens = tokens[1:]
		}
	}
	return tokens
}

func (p *curlParser) parseTokens(tokens []string) error {
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		if tok == "--" {
			continue
		}
		if strings.HasPrefix(tok, "--") {
			name, val, hasVal := strings.Cut(tok[2:], "=")
			name = strings.ToLower(name)
			if curlLongValueFlags[name] {
				if !hasVal {
					v, err := takeCurlValue(tokens, &i, "--"+name)
					if err != nil {
						return err
					}
					val = v
				}
				p.handleLongValue(name, val)
				continue
			}
			if curlLongBoolFlags[name] {
				p.handleLongBool(name)
				continue
			}
			p.warn("已忽略参数 --" + name)
			continue
		}
		if strings.HasPrefix(tok, "-") && tok != "-" {
			if err := p.handleShortToken(tokens, &i, tok[1:]); err != nil {
				return err
			}
			continue
		}
		if tok == "-" {
			p.warn("不支持从标准输入读取内容")
			continue
		}
		if p.url == "" {
			p.url = tok
		} else {
			p.warn("已忽略额外地址 " + tok)
		}
	}
	return nil
}

func takeCurlValue(tokens []string, i *int, flag string) (string, error) {
	if *i+1 >= len(tokens) {
		return "", fmt.Errorf("参数 %s 缺少值", flag)
	}
	*i++
	return tokens[*i], nil
}

func (p *curlParser) handleShortToken(tokens []string, i *int, rest string) error {
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if curlShortValueFlags[c] {
			val := rest[j+1:]
			if val == "" {
				v, err := takeCurlValue(tokens, i, "-"+string(c))
				if err != nil {
					return err
				}
				val = v
			}
			p.handleShortValue(c, val)
			return nil
		}
		if p.handleShortBool(c) {
			continue
		}
		p.warn("已忽略参数 -" + string(c))
	}
	return nil
}

func (p *curlParser) handleShortValue(c byte, val string) {
	switch c {
	case 'X':
		p.setMethod(val)
	case 'H':
		p.addHeader(val)
	case 'd':
		p.addData(val, "data")
	case 'F':
		p.addForm(val, false)
	case 'u':
		p.setBasic(val)
	case 'A':
		p.addHeaderIfMissing("User-Agent", val)
	case 'b':
		p.addCookie(val)
	case 'e':
		p.addHeaderIfMissing("Referer", val)
	case 'T':
		p.warn("暂不支持 -T 原始文件上传，请改用 -F 表单上传")
	case 'x':
		p.warn("已忽略代理参数 -x")
	case 'K':
		p.warn("已忽略 cURL 配置文件 -K")
	default:
		// 其余取值参数（输出、超时、cookie 文件等）不影响请求内容。
	}
}

func (p *curlParser) handleShortBool(c byte) bool {
	switch c {
	case 'I':
		p.head = true
	case 'G':
		p.getMode = true
	case 'k':
		p.warn("已忽略 -k，请在设置中关闭 TLS 证书校验")
	case 'L', 's', 'S', 'v', 'i', 'f', 'N', 'J', 'O', 'j', 'g', 'n', 'q', 'R', 'a', 'B', 'M', '0', '1', '2', '3', '4', '6':
		// 与请求内容无关的开关。
	default:
		return false
	}
	return true
}

func (p *curlParser) handleLongValue(name, val string) {
	switch name {
	case "request":
		p.setMethod(val)
	case "url":
		p.url = strings.TrimSpace(val)
	case "header":
		p.addHeader(val)
	case "data", "data-raw", "data-binary":
		p.addData(val, name)
	case "data-urlencode":
		p.addData(val, name)
	case "json":
		p.jsonMode = true
		p.addData(val, name)
		p.addHeaderIfMissing("Accept", "application/json")
	case "form":
		p.addForm(val, false)
	case "form-string":
		p.addForm(val, true)
	case "user":
		p.setBasic(val)
	case "user-agent":
		p.addHeaderIfMissing("User-Agent", val)
	case "cookie":
		p.addCookie(val)
	case "referer":
		p.addHeaderIfMissing("Referer", val)
	case "oauth2-bearer":
		p.bearer = val
		p.bearerSet = true
	case "upload-file":
		p.warn("暂不支持 --upload-file 原始文件上传，请改用 -F 表单上传")
	case "proxy", "preproxy", "socks4", "socks4a", "socks5", "socks5-hostname":
		p.warn("已忽略代理参数 --" + name)
	case "cert", "key", "cacert":
		p.warn("已忽略客户端证书参数 --" + name)
	case "config":
		p.warn("已忽略 cURL 配置文件 --config")
	case "aws-sigv4":
		p.warn("已忽略 AWS 签名参数 --aws-sigv4")
	default:
		// 超时、输出、cookie 文件等不影响请求内容。
	}
}

func (p *curlParser) handleLongBool(name string) {
	switch name {
	case "head":
		p.head = true
	case "get":
		p.getMode = true
	case "insecure":
		p.warn("已忽略 --insecure，请在设置中关闭 TLS 证书校验")
	default:
		// compressed/location/silent 等与导入内容无关。
	}
}

func (p *curlParser) setMethod(method string) {
	p.method = strings.ToUpper(strings.TrimSpace(method))
	p.methodSet = true
}

func (p *curlParser) addHeader(raw string) {
	key, value, ok := strings.Cut(raw, ":")
	if !ok {
		key = raw
		value = ""
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		p.warn("已忽略空请求头")
		return
	}
	if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") {
		p.warn("已忽略请求头 " + key)
		return
	}
	p.headers = append(p.headers, model.KV{Key: key, Value: value, Enabled: true})
	if strings.EqualFold(key, "Content-Type") {
		p.contentType = value
	}
}

func (p *curlParser) addHeaderIfMissing(key, value string) {
	for _, h := range p.headers {
		if strings.EqualFold(h.Key, key) {
			return
		}
	}
	p.headers = append(p.headers, model.KV{Key: key, Value: value, Enabled: true})
}

func (p *curlParser) addCookie(value string) {
	if strings.HasPrefix(strings.TrimSpace(value), "@") {
		p.warn("暂不支持从 Cookie 文件读取，请改用 -H 'Cookie: ...'")
		return
	}
	p.addHeaderIfMissing("Cookie", value)
}

func (p *curlParser) addData(value, name string) {
	trimmed := strings.TrimSpace(value)

	// curl 的 -d/--data-binary 会把 @文件 当作文件内容读取；
	// --data-raw 则连 @ 一起原样发送。
	if (name == "data" || name == "data-binary") && strings.HasPrefix(trimmed, "@") {
		path := strings.TrimSpace(trimmed[1:])
		if path == "" || path == "-" {
			p.warn("暂不支持从标准输入读取请求体")
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			p.warn("读取请求体文件失败: " + err.Error())
			return
		}
		if info.IsDir() {
			p.warn("请求体路径是一个目录: " + path)
			return
		}
		if info.Size() > maxCurlBodyFileBytes {
			p.warn(fmt.Sprintf("请求体文件超过 %d MB 上限，已跳过", maxCurlBodyFileBytes>>20))
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			p.warn("读取请求体文件失败: " + err.Error())
			return
		}
		if !utf8.Valid(data) {
			p.warn("请求体文件不是 UTF-8 文本，已跳过")
			return
		}
		p.data = append(p.data, string(data))
		return
	}

	if name == "data-urlencode" {
		if strings.HasPrefix(trimmed, "@") {
			p.warn("暂不支持从文件读取请求体（--data-urlencode @文件），请直接粘贴内容")
			return
		}
		p.warn("--data-urlencode 内容已按原样导入，请确认编码是否符合预期")
	}
	p.data = append(p.data, value)
}

func (p *curlParser) addForm(spec string, literal bool) {
	key, value, ok := strings.Cut(spec, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		p.warn("已忽略格式不正确的表单字段: " + spec)
		return
	}

	if !literal && strings.HasPrefix(value, "@") {
		path := strings.TrimSpace(value[1:])
		cleaned, stripped := stripCurlFormOptions(path)
		if stripped {
			p.warn("已忽略文件字段 " + key + " 的附加选项")
		}
		path = strings.TrimSpace(cleaned)
		if path == "" || path == "-" {
			p.warn("已忽略没有有效路径的文件字段: " + key)
			return
		}
		p.files = append(p.files, model.FileField{Key: key, Path: path, Enabled: true})
		return
	}
	if !literal && strings.HasPrefix(value, "<") {
		p.warn("暂不支持从文件读取文本字段（-F " + key + "=<文件）")
		return
	}

	if !literal {
		cleaned, stripped := stripCurlFormOptions(value)
		if stripped {
			p.warn("已忽略表单字段 " + key + " 的附加选项")
		}
		value = cleaned
	}
	p.forms = append(p.forms, model.KV{Key: key, Value: value, Enabled: true})
}

func stripCurlFormOptions(s string) (string, bool) {
	idx := -1
	lower := strings.ToLower(s)
	for _, marker := range []string{";type=", ";filename=", ";headers=", ";encoder="} {
		if i := strings.Index(lower, marker); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx >= 0 {
		return s[:idx], true
	}
	return s, false
}

func (p *curlParser) setBasic(value string) {
	user, pass, _ := strings.Cut(value, ":")
	p.basicUser = user
	p.basicPass = pass
	p.basicSet = true
}

func (p *curlParser) warn(msg string) {
	if msg != "" {
		p.warnings = append(p.warnings, msg)
	}
}

func (p *curlParser) hasHeader(key string) bool {
	for _, h := range p.headers {
		if strings.EqualFold(h.Key, key) {
			return true
		}
	}
	return false
}

func (p *curlParser) removeHeader(key string) {
	out := p.headers[:0]
	for _, h := range p.headers {
		if strings.EqualFold(h.Key, key) {
			continue
		}
		out = append(out, h)
	}
	p.headers = out
}

func (p *curlParser) result() (*CurlImport, error) {
	if p.url == "" {
		return nil, errors.New("没有找到请求地址")
	}

	method := p.method
	if !p.methodSet {
		switch {
		case p.head:
			method = "HEAD"
		case len(p.forms) > 0 || len(p.files) > 0:
			method = "POST"
		case len(p.data) > 0 && !p.getMode:
			method = "POST"
		default:
			method = "GET"
		}
	}
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)
	if !curlSupportedMethods[method] {
		p.warn("不支持请求方法 " + method + "，已改用 GET")
		method = "GET"
	}

	rawBody := strings.Join(p.data, "&")
	if p.getMode {
		if len(p.forms) > 0 || len(p.files) > 0 {
			p.warn("已忽略 -G：它不适用于 multipart 表单")
		} else if rawBody != "" {
			p.url = appendQueryToURL(p.url, rawBody)
			rawBody = ""
			p.data = nil
		}
	}

	body := model.Body{Type: model.BodyNone}
	if len(p.forms) > 0 || len(p.files) > 0 {
		if len(p.data) > 0 {
			p.warn("同时存在 -d 与 -F，已按 multipart 处理")
		}
		body = model.Body{Type: model.BodyMultipart, Form: p.forms, Files: p.files}
		if p.hasHeader("Content-Type") {
			p.removeHeader("Content-Type")
			p.warn("multipart 的 Content-Type 由发送时自动生成，已移除手工设置")
		}
	} else if len(p.data) > 0 {
		bodyType := model.BodyText
		lowerCT := strings.ToLower(p.contentType)
		isJSON := p.jsonMode || strings.Contains(lowerCT, "application/json") || strings.Contains(lowerCT, "+json")
		if isJSON {
			bodyType = model.BodyJSON
		}
		body = model.Body{Type: bodyType, Raw: rawBody}
		if !p.hasHeader("Content-Type") {
			if p.jsonMode || isJSON {
				p.addHeaderIfMissing("Content-Type", "application/json")
			} else {
				p.addHeaderIfMissing("Content-Type", "application/x-www-form-urlencoded")
			}
		}
	}

	auth := model.Auth{Type: model.AuthNone, APIKey: model.APIKeyAuth{AddTo: model.AddToHeader}}
	if p.bearerSet {
		auth.Type = model.AuthBearer
		auth.Bearer = p.bearer
	} else if p.basicSet {
		auth.Type = model.AuthBasic
		auth.Basic = model.BasicAuth{Username: p.basicUser, Password: p.basicPass}
	}

	req := model.Request{
		Name:    curlRequestName(p.url),
		Method:  method,
		URL:     p.url,
		Headers: p.headers,
		Body:    body,
		Auth:    auth,
	}
	return &CurlImport{Request: req, Warnings: dedupe(p.warnings)}, nil
}

func curlRequestName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "导入的请求"
	}
	if path := strings.Trim(u.Path, "/"); path != "" {
		return path
	}
	if u.Host != "" {
		return u.Host
	}
	return "导入的请求"
}

func appendQueryToURL(raw, data string) string {
	if data == "" {
		return raw
	}
	hash := ""
	if i := strings.Index(raw, "#"); i >= 0 {
		hash = raw[i:]
		raw = raw[:i]
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + data + hash
}

// buildCurlCommand 把已解析变量的请求转换成 Bash 风格的 cURL 命令。
func buildCurlCommand(req model.Request, settings model.Settings) (string, []string, error) {
	finalURL, err := proxy.BuildURL(req.URL, req.Params, req.Auth)
	if err != nil {
		return "", nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	var warnings []string
	parts := []string{"curl"}

	if settings.FollowRedirects {
		parts = append(parts, "-L")
	}
	if settings.InsecureTLS {
		parts = append(parts, "-k")
	}
	if settings.TimeoutMS > 0 {
		seconds := strconv.FormatFloat(float64(settings.TimeoutMS)/1000, 'f', -1, 64)
		parts = append(parts, "--max-time", seconds)
	}

	hasBody := curlRequestHasBody(req.Body)
	if method == "HEAD" {
		// cURL 用 -I 才会同时抑制响应体读取，-X HEAD 可能一直等待响应体。
		parts = append(parts, "-I")
	} else if method != "GET" || hasBody {
		parts = append(parts, "-X", method)
	}
	parts = append(parts, shellQuote(finalURL))

	hasContentType := false
	for _, h := range req.Headers {
		if !h.Enabled || strings.TrimSpace(h.Key) == "" {
			continue
		}
		if strings.EqualFold(h.Key, "Content-Length") || strings.EqualFold(h.Key, "Transfer-Encoding") {
			continue
		}
		if req.Body.Type == model.BodyMultipart && strings.EqualFold(h.Key, "Content-Type") {
			continue
		}
		if req.Auth.Type == model.AuthBasic && strings.EqualFold(h.Key, "Authorization") {
			continue
		}
		if req.Auth.Type == model.AuthBearer &&
			strings.TrimSpace(req.Auth.Bearer) != "" &&
			strings.EqualFold(h.Key, "Authorization") {
			continue
		}
		if req.Auth.Type == model.AuthAPIKey &&
			req.Auth.APIKey.AddTo == model.AddToHeader &&
			strings.TrimSpace(req.Auth.APIKey.Key) != "" &&
			strings.EqualFold(h.Key, req.Auth.APIKey.Key) {
			continue
		}
		if strings.EqualFold(h.Key, "Content-Type") {
			hasContentType = true
		}
		parts = append(parts, "-H", shellQuote(h.Key+": "+h.Value))
	}

	if req.Body.Type != model.BodyNone && req.Body.Type != model.BodyMultipart && !hasContentType {
		switch req.Body.Type {
		case model.BodyJSON:
			parts = append(parts, "-H", shellQuote("Content-Type: application/json"))
		case model.BodyText:
			parts = append(parts, "-H", shellQuote("Content-Type: text/plain; charset=utf-8"))
		case model.BodyForm:
			parts = append(parts, "-H", shellQuote("Content-Type: application/x-www-form-urlencoded"))
		}
	}

	switch req.Auth.Type {
	case model.AuthBasic:
		parts = append(parts, "-u", shellQuote(req.Auth.Basic.Username+":"+req.Auth.Basic.Password))
	case model.AuthBearer:
		if token := strings.TrimSpace(req.Auth.Bearer); token != "" {
			parts = append(parts, "-H", shellQuote("Authorization: Bearer "+token))
		}
	case model.AuthAPIKey:
		if req.Auth.APIKey.AddTo == model.AddToHeader {
			if key := strings.TrimSpace(req.Auth.APIKey.Key); key != "" {
				parts = append(parts, "-H", shellQuote(key+": "+req.Auth.APIKey.Value))
			}
		}
	}

	switch req.Body.Type {
	case model.BodyJSON, model.BodyText:
		if req.Body.Raw != "" {
			parts = append(parts, "--data-raw", shellQuote(req.Body.Raw))
		}
	case model.BodyForm:
		form := url.Values{}
		for _, f := range req.Body.Form {
			if !f.Enabled || strings.TrimSpace(f.Key) == "" {
				continue
			}
			form.Set(f.Key, f.Value)
		}
		if encoded := form.Encode(); encoded != "" {
			parts = append(parts, "--data-raw", shellQuote(encoded))
		}
	case model.BodyMultipart:
		fields, files := 0, 0
		for _, f := range req.Body.Form {
			if !f.Enabled || strings.TrimSpace(f.Key) == "" {
				continue
			}
			parts = append(parts, "--form-string", shellQuote(f.Key+"="+f.Value))
			fields++
		}
		for _, f := range req.Body.Files {
			if !f.Enabled || strings.TrimSpace(f.Key) == "" || strings.TrimSpace(f.Path) == "" {
				continue
			}
			parts = append(parts, "-F", shellQuote(f.Key+"=@"+f.Path))
			files++
		}
		if fields == 0 && files == 0 {
			warnings = append(warnings, "multipart 请求没有有效字段或文件")
		}
	}

	return strings.Join(parts, " "), dedupe(warnings), nil
}

func curlRequestHasBody(b model.Body) bool {
	switch b.Type {
	case model.BodyJSON, model.BodyText:
		return b.Raw != ""
	case model.BodyForm:
		for _, f := range b.Form {
			if f.Enabled && strings.TrimSpace(f.Key) != "" {
				return true
			}
		}
	case model.BodyMultipart:
		for _, f := range b.Form {
			if f.Enabled && strings.TrimSpace(f.Key) != "" {
				return true
			}
		}
		for _, f := range b.Files {
			if f.Enabled && strings.TrimSpace(f.Key) != "" && strings.TrimSpace(f.Path) != "" {
				return true
			}
		}
	}
	return false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
