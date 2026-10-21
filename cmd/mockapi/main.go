// mockapi 是一个用于本地联调的模拟接口服务。
//
// 用法：go run ./cmd/mockapi -addr 127.0.0.1:8788
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8788", "监听地址")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/users", handleUsers)
	mux.HandleFunc("/users/", handleUser)
	mux.HandleFunc("/echo", handleEcho)
	mux.HandleFunc("/status/", handleStatus)
	mux.HandleFunc("/redirect", handleRedirect)
	mux.HandleFunc("/binary", handleBinary)
	mux.HandleFunc("/slow", handleSlow)
	mux.HandleFunc("/", handleIndex)

	srv := &http.Server{Addr: *addr, Handler: logRequests(mux)}
	log.Printf("mockapi 已启动: http://%s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(start).Round(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "接口不存在", "path": r.URL.Path})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0,
		"message": "NanoAPI mock 服务",
		"endpoints": []string{
			"GET  /users",
			"GET  /users/{id}",
			"POST /echo",
			"GET  /status/{code}",
			"GET  /redirect",
			"GET  /binary",
			"GET  /slow?ms=2000",
		},
	})
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0,
		"data": []map[string]any{
			{"id": 1, "name": "张三", "email": "zhangsan@example.com", "role": "admin"},
			{"id": 2, "name": "李四", "email": "lisi@example.com", "role": "user"},
			{"id": 3, "name": "王五", "email": "wangwu@example.com", "role": "user"},
		},
		"total": 3,
	})
}

func handleUser(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/users/")
	n, err := strconv.Atoi(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "id 必须是数字"})
		return
	}
	if n <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "用户不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0,
		"data": map[string]any{"id": n, "name": fmt.Sprintf("用户%d", n), "email": fmt.Sprintf("user%d@example.com", n)},
	})
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	writeJSON(w, http.StatusOK, map[string]any{
		"code":    0,
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   r.URL.Query(),
		"headers": r.Header,
		"body":    string(body),
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/status/")
	code, err := strconv.Atoi(raw)
	if err != nil || code < 100 || code > 599 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "状态码不合法"})
		return
	}
	writeJSON(w, code, map[string]any{"code": code, "message": http.StatusText(code)})
}

func handleRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/users", http.StatusFound)
}

func handleBinary(w http.ResponseWriter, r *http.Request) {
	// 一小段 PNG 文件头，用于验证二进制响应的识别与展示。
	png := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

func handleSlow(w http.ResponseWriter, r *http.Request) {
	ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
	if ms <= 0 {
		ms = 1000
	}
	if ms > 30000 {
		ms = 30000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": fmt.Sprintf("等待了 %d 毫秒", ms)})
}
