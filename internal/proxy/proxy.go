// Package proxy 实现 HTTP 请求的执行引擎。
//
// 相比通用 HTTP 客户端，这里需要完全掌控 TLS 校验、重定向、超时与耗时采集，
// 因此直接基于 net/http 构建请求，而不是依赖更高层的封装。
package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"nanoapi/internal/model"
)

const (
	// DefaultMaxBodyBytes 是响应体的默认读取上限。
	DefaultMaxBodyBytes = 20 << 20 // 20 MB
	// DefaultTimeoutMS 是默认超时时间。
	DefaultTimeoutMS = 30000
	// maxRedirects 是跟随重定向的最大跳数。
	maxRedirects = 10
	// userAgent 是 NanoAPI 发出的默认 User-Agent。
	userAgent = "NanoAPI/0.1.0"
)

// 错误分类，前端据此展示不同的提示。
const (
	ErrNone           = ""
	ErrTimeout        = "timeout"
	ErrDNS            = "dns"
	ErrRefused        = "refused"
	ErrTLS            = "tls"
	ErrReset          = "reset"
	ErrBadURL         = "badurl"
	ErrNetwork        = "network"
	ErrTooManyRedirec = "redirects"
)

// Options 描述一次待发送的请求。字段内容已完成插值，可直接发出。
type Options struct {
	Method          string
	URL             string
	Params          []model.KV
	Headers         []model.KV
	Auth            model.Auth
	Body            []byte
	ContentType     string
	TimeoutMS       int
	FollowRedirects bool
	InsecureTLS     bool
	MaxBodyBytes    int64
}

// Hop 记录一次重定向。
type Hop struct {
	URL    string `json:"url"`
	Status int    `json:"status"`
}

// Result 是一次请求的完整结果。
//
// 网络层错误不会以 Go error 返回，而是填入 Error 与 ErrorKind，
// 以便前端把「请求失败」和「程序缺陷」区分开。
type Result struct {
	Status         int                `json:"status"`
	StatusText     string             `json:"statusText"`
	Proto          string             `json:"proto"`
	Headers        map[string][]string `json:"headers"`
	Body           string             `json:"body"`
	BodyBase64     bool               `json:"bodyBase64"`
	IsBinary       bool               `json:"isBinary"`
	ContentType    string             `json:"contentType"`
	DurationMS     int64              `json:"durationMs"`
	DNSMS          int64              `json:"dnsMs"`
	ConnectMS      int64              `json:"connectMs"`
	TLSMS          int64              `json:"tlsMs"`
	TTFBMS         int64              `json:"ttfbMs"`
	Size           int64              `json:"size"`
	Redirects      []Hop              `json:"redirects,omitempty"`
	RemoteAddr     string             `json:"remoteAddr,omitempty"`
	Truncated      bool               `json:"truncated"`
	Error          string             `json:"error,omitempty"`
	ErrorKind      string             `json:"errorKind,omitempty"`
	UnresolvedVars []string           `json:"unresolvedVars,omitempty"`
}

// Send 发送请求并返回结果。
func Send(ctx context.Context, opt Options) *Result {
	res := &Result{Headers: map[string][]string{}}

	method := strings.ToUpper(strings.TrimSpace(opt.Method))
	if method == "" {
		method = http.MethodGet
	}

	rawURL := strings.TrimSpace(opt.URL)
	if rawURL == "" {
		res.Error = "请填写请求地址"
		res.ErrorKind = ErrBadURL
		return res
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		res.Error = "请求地址无法解析: " + err.Error()
		res.ErrorKind = ErrBadURL
		return res
	}
	if u.Host == "" {
		res.Error = "请求地址缺少主机名"
		res.ErrorKind = ErrBadURL
		return res
	}

	// 查询参数：URL 上原有的参数保留，参数表里的同名项覆盖之。
	query := mergeQuery(u.RawQuery, opt.Params)

	// API Key 认证若配置为注入 query，则并入查询串。
	if opt.Auth.Type == model.AuthAPIKey && opt.Auth.APIKey.AddTo == model.AddToQuery {
		if k := strings.TrimSpace(opt.Auth.APIKey.Key); k != "" {
			query = appendPair(query, k, opt.Auth.APIKey.Value)
		}
	}
	u.RawQuery = query

	var bodyReader io.Reader
	if len(opt.Body) > 0 {
		bodyReader = bytes.NewReader(opt.Body)
	}

	// 超时与链路追踪必须在创建请求之前挂到 context 上，
	// 否则 http.Request 会一直持有没有超时的父 context，超时将完全失效。
	timeout := time.Duration(opt.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultTimeoutMS * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dnsStart, dnsDone, connectStart, connectDone, tlsStart, tlsDone, firstByte time.Time
	var remoteAddr string
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { if dnsStart.IsZero() { dnsStart = time.Now() } },
		DNSDone:             func(httptrace.DNSDoneInfo) { if dnsDone.IsZero() { dnsDone = time.Now() } },
		ConnectStart:        func(string, string) { if connectStart.IsZero() { connectStart = time.Now() } },
		ConnectDone:         func(string, string, error) { if connectDone.IsZero() { connectDone = time.Now() } },
		TLSHandshakeStart:   func() { if tlsStart.IsZero() { tlsStart = time.Now() } },
		TLSHandshakeDone:    func(tls.ConnectionState, error) { if tlsDone.IsZero() { tlsDone = time.Now() } },
		GotConn:             func(info httptrace.GotConnInfo) { if info.Conn != nil { remoteAddr = info.Conn.RemoteAddr().String() } },
		GotFirstResponseByte: func() { firstByte = time.Now() },
	})

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		res.Error = "构造请求失败: " + err.Error()
		res.ErrorKind = ErrBadURL
		return res
	}
	req.Header.Set("User-Agent", userAgent)

	for _, h := range opt.Headers {
		if !h.Enabled || strings.TrimSpace(h.Key) == "" {
			continue
		}
		if strings.EqualFold(h.Key, "Host") {
			req.Host = h.Value
			continue
		}
		req.Header.Set(h.Key, h.Value)
	}
	if opt.ContentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", opt.ContentType)
	}
	applyAuth(req, opt.Auth)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opt.InsecureTLS {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.InsecureSkipVerify = true
	}

	var hops []Hop
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if !opt.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= maxRedirects {
				res.Error = fmt.Sprintf("重定向超过 %d 次，已中止", maxRedirects)
				res.ErrorKind = ErrTooManyRedirec
				return errors.New("stopped after too many redirects")
			}
			if r.Response != nil {
				hop := Hop{Status: r.Response.StatusCode}
				if r.Response.Request != nil {
					hop.URL = r.Response.Request.URL.String()
				}
				hops = append(hops, hop)
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.DurationMS = time.Since(start).Milliseconds()
		res.Error = translateError(err)
		res.ErrorKind = classify(err)
		return res
	}
	defer resp.Body.Close()

	max := opt.MaxBodyBytes
	if max <= 0 {
		max = DefaultMaxBodyBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		res.DurationMS = time.Since(start).Milliseconds()
		res.Error = "读取响应体失败: " + err.Error()
		res.ErrorKind = ErrNetwork
		return res
	}
	if int64(len(data)) > max {
		data = data[:max]
		res.Truncated = true
	}

	res.DurationMS = time.Since(start).Milliseconds()
	if !firstByte.IsZero() {
		res.TTFBMS = firstByte.Sub(start).Milliseconds()
	}
	if !dnsStart.IsZero() && !dnsDone.IsZero() {
		res.DNSMS = dnsDone.Sub(dnsStart).Milliseconds()
	}
	if !connectStart.IsZero() && !connectDone.IsZero() {
		res.ConnectMS = connectDone.Sub(connectStart).Milliseconds()
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() {
		res.TLSMS = tlsDone.Sub(tlsStart).Milliseconds()
	}

	res.Status = resp.StatusCode
	res.StatusText = statusText(resp)
	res.Proto = resp.Proto
	res.Headers = map[string][]string(resp.Header)
	res.ContentType = resp.Header.Get("Content-Type")
	res.Size = int64(len(data))
	res.RemoteAddr = remoteAddr
	res.Redirects = hops

	if looksBinary(res.ContentType, data) {
		res.IsBinary = true
		res.BodyBase64 = true
		res.Body = base64.StdEncoding.EncodeToString(data)
	} else {
		res.Body = string(data)
	}
	return res
}

// applyAuth 把认证信息写入请求。
func applyAuth(req *http.Request, auth model.Auth) {
	switch auth.Type {
	case model.AuthBasic:
		req.SetBasicAuth(auth.Basic.Username, auth.Basic.Password)
	case model.AuthBearer:
		if t := strings.TrimSpace(auth.Bearer); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	case model.AuthAPIKey:
		if auth.APIKey.AddTo == model.AddToQuery {
			return
		}
		if k := strings.TrimSpace(auth.APIKey.Key); k != "" {
			req.Header.Set(k, auth.APIKey.Value)
		}
	}
}

// mergeQuery 把参数表合并进已有的查询串，参数表中的同名键覆盖原值，
// 其余参数保持原有顺序。
func mergeQuery(existing string, params []model.KV) string {
	pairs := parseQueryPairs(existing)
	for _, p := range params {
		if !p.Enabled || strings.TrimSpace(p.Key) == "" {
			continue
		}
		replaced := false
		for i := range pairs {
			if pairs[i].k == p.Key {
				pairs[i].v = p.Value
				replaced = true
				break
			}
		}
		if !replaced {
			pairs = append(pairs, kvPair{k: p.Key, v: p.Value})
		}
	}
	return encodePairs(pairs)
}

// appendPair 往查询串末尾追加一个键值对。
func appendPair(query, key, value string) string {
	pairs := parseQueryPairs(query)
	pairs = append(pairs, kvPair{k: key, v: value})
	return encodePairs(pairs)
}

type kvPair struct{ k, v string }

func parseQueryPairs(raw string) []kvPair {
	if raw == "" {
		return nil
	}
	var out []kvPair
	for _, seg := range strings.Split(raw, "&") {
		if seg == "" {
			continue
		}
		k, v, _ := strings.Cut(seg, "=")
		dk, err := url.QueryUnescape(k)
		if err != nil {
			dk = k
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			dv = v
		}
		out = append(out, kvPair{k: dk, v: dv})
	}
	return out
}

func encodePairs(pairs []kvPair) string {
	if len(pairs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p.k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p.v))
	}
	return b.String()
}

// statusText 从状态行中取出状态描述，例如 "200 OK" 中的 "OK"。
func statusText(resp *http.Response) string {
	s := resp.Status
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return strconv.Itoa(resp.StatusCode)
}

// looksBinary 判断响应体是否应按二进制处理。
func looksBinary(contentType string, data []byte) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case strings.HasPrefix(ct, "image/"),
		strings.HasPrefix(ct, "video/"),
		strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "font/"),
		ct == "application/octet-stream",
		ct == "application/pdf",
		ct == "application/zip",
		ct == "application/gzip",
		ct == "application/x-gzip",
		ct == "application/x-tar",
		ct == "application/x-7z-compressed",
		ct == "application/x-rar-compressed",
		ct == "application/wasm",
		ct == "application/x-protobuf",
		ct == "application/x-msgpack",
		strings.HasPrefix(ct, "application/vnd.ms-"):
		return true
	}
	if !utf8.Valid(data) {
		return true
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// classify 把底层错误归类，便于前端给出针对性提示。
func classify(err error) string {
	if err == nil {
		return ErrNone
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		if uerr.Timeout() {
			return ErrTimeout
		}
		err = uerr.Err
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrDNS
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return ErrTLS
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return ErrTLS
	}
	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		return ErrTLS
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return ErrTimeout
		}
		// 优先用系统错误码判断，各平台措辞不同但错误码一致。
		var errno syscall.Errno
		if errors.As(opErr.Err, &errno) {
			switch errno {
			case syscall.ECONNREFUSED:
				return ErrRefused
			case syscall.ECONNRESET:
				return ErrReset
			case syscall.ETIMEDOUT:
				return ErrTimeout
			}
		}
		// Windows 下 WSAECONNREFUSED 的文案是「actively refused」，
		// 与 Linux 的「connection refused」不同，这里一并覆盖。
		msg := strings.ToLower(opErr.Error())
		switch {
		case strings.Contains(msg, "connection refused"),
			strings.Contains(msg, "actively refused"),
			strings.Contains(msg, "no connection could be made"):
			return ErrRefused
		case strings.Contains(msg, "connection reset"),
			strings.Contains(msg, "connection was aborted"):
			return ErrReset
		case strings.Contains(msg, "no such host"):
			return ErrDNS
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "certificate") ||
		strings.Contains(strings.ToLower(err.Error()), "tls") {
		return ErrTLS
	}
	return ErrNetwork
}

// translateError 把底层错误转成中文提示。
func translateError(err error) string {
	switch classify(err) {
	case ErrTimeout:
		return "请求超时，请检查地址或调大超时时间"
	case ErrDNS:
		return "域名解析失败，请检查域名是否正确或网络是否可用"
	case ErrRefused:
		return "连接被拒绝，目标端口未开放"
	case ErrTLS:
		return "TLS 证书校验失败，可到设置中关闭证书校验后重试"
	case ErrReset:
		return "连接被重置，目标服务主动中断了连接"
	case ErrTooManyRedirec:
		return "重定向次数过多，已中止"
	}
	return err.Error()
}
