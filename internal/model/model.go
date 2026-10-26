// Package model 定义 NanoAPI 的全部数据结构。
// 这些结构同时被 Go 后端与前端（经 JSON 序列化）使用，字段名即前端契约。
package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// KV 表示一条键值对，用于查询参数、请求头、表单字段和环境变量。
type KV struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
	Desc    string `json:"desc,omitempty"`
}

// IsBlank 判断该行是否为尚未填写的占位行。
func (k KV) IsBlank() bool { return k.Key == "" && k.Value == "" }

// Body 类型常量。
const (
	BodyNone      = "none"
	BodyJSON      = "json"
	BodyText      = "text"
	BodyForm      = "form"
	BodyMultipart = "multipart"
)

// FileField 是 multipart 请求中的一个本地文件字段。
type FileField struct {
	Key     string `json:"key"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

// Body 表示请求体。不同类型下只有对应字段有意义。
type Body struct {
	Type  string      `json:"type"`
	Raw   string      `json:"raw,omitempty"`
	Form  []KV        `json:"form,omitempty"`
	Files []FileField `json:"files,omitempty"`
}

// 认证类型常量。
const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthBearer = "bearer"
	AuthAPIKey = "apikey"
)

// API Key 的注入位置。
const (
	AddToHeader = "header"
	AddToQuery  = "query"
)

// BasicAuth 保存 Basic 认证的用户名与密码。
type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// APIKeyAuth 保存 API Key 认证的键、值与注入位置。
type APIKeyAuth struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	AddTo string `json:"addTo"`
}

// Auth 表示请求的认证配置。
type Auth struct {
	Type   string     `json:"type"`
	Basic  BasicAuth  `json:"basic"`
	Bearer string     `json:"bearer"`
	APIKey APIKeyAuth `json:"apiKey"`
}

// Request 表示一个可发送的接口请求。
type Request struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Method  string `json:"method"`
	URL     string `json:"url"`
	Params  []KV   `json:"params,omitempty"`
	Headers []KV   `json:"headers,omitempty"`
	Body    Body   `json:"body"`
	Auth    Auth   `json:"auth"`
}

// 集合树节点类型。
const (
	ItemFolder  = "folder"
	ItemRequest = "request"
)

// CollectionItem 是集合树的节点，可以是文件夹（持有子节点）也可以是请求。
type CollectionItem struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Request *Request          `json:"request,omitempty"`
	Items   []*CollectionItem `json:"items,omitempty"`
}

// Environment 表示一组变量，可被当前环境或全局环境引用。
type Environment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Values   []KV   `json:"values,omitempty"`
	IsGlobal bool   `json:"isGlobal"`
}

// HistoryEntry 是一次请求的历史记录。
type HistoryEntry struct {
	ID         string  `json:"id"`
	Request    Request `json:"request"`
	Status     int     `json:"status"`
	StatusText string  `json:"statusText"`
	DurationMS int64   `json:"durationMs"`
	Size       int64   `json:"size"`
	Timestamp  int64   `json:"timestamp"`
	Error      string  `json:"error,omitempty"`
}

// Settings 保存用户的全局偏好。
type Settings struct {
	TimeoutMS       int    `json:"timeoutMs"`
	FollowRedirects bool   `json:"followRedirects"`
	InsecureTLS     bool   `json:"insecureTls"`
	Theme           string `json:"theme"`
	// ActiveEnvID 记住上次选中的环境，重启后继续使用。
	ActiveEnvID string `json:"activeEnvId,omitempty"`
}

// OpenTab 是一个请求标签页的持久化状态。
// 响应不落盘，避免数据文件随响应体膨胀。
type OpenTab struct {
	Key     string  `json:"key"`
	Request Request `json:"request"`
	Dirty   bool    `json:"dirty"`
}

// AppData 是持久化到磁盘的完整应用状态。
type AppData struct {
	Collections  []*CollectionItem `json:"collections"`
	Environments []*Environment    `json:"environments"`
	History      []*HistoryEntry   `json:"history"`
	Settings     Settings          `json:"settings"`
	Tabs         []*OpenTab        `json:"tabs,omitempty"`
	ActiveTabKey string            `json:"activeTabKey,omitempty"`
}

// DefaultSettings 返回首次启动时使用的默认设置。
func DefaultSettings() Settings {
	return Settings{
		TimeoutMS:       30000,
		FollowRedirects: true,
		InsecureTLS:     false,
		Theme:           "dark",
	}
}

// NewRequest 返回一个带默认值的空请求。
func NewRequest() Request {
	return Request{
		ID:     NewID(),
		Name:   "新建请求",
		Method: "GET",
		Body:   Body{Type: BodyNone},
		Auth:   Auth{Type: AuthNone, APIKey: APIKeyAuth{AddTo: AddToHeader}},
	}
}

// NewID 生成一个随机唯一标识。
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(b)
}

// FindItem 在集合树中按 ID 查找节点，返回其父节点与命中的节点。
// 根级节点的父节点为 nil。
func FindItem(items []*CollectionItem, id string) (parent, found *CollectionItem) {
	for _, it := range items {
		if it == nil {
			continue
		}
		if it.ID == id {
			return nil, it
		}
		// 递归命中时，父节点以更内层的结果为准；
		// 只有当内层没给出父节点（命中的是该层列表的直接子节点）时才用 it。
		if p, f := FindItem(it.Items, id); f != nil {
			if p == nil {
				return it, f
			}
			return p, f
		}
	}
	return nil, nil
}

// RemoveItem 从集合树中删除指定 ID 的节点，返回是否删除成功。
// items 传入指针，删除根级节点后调用方能拿到缩短后的切片。
func RemoveItem(items *[]*CollectionItem, id string) bool {
	list := *items
	for i, it := range list {
		if it == nil {
			continue
		}
		if it.ID == id {
			copy(list[i:], list[i+1:])
			list[len(list)-1] = nil
			*items = list[:len(list)-1]
			return true
		}
		if RemoveItem(&it.Items, id) {
			return true
		}
	}
	return false
}

// CountRequests 统计集合树中的请求数量。
func CountRequests(items []*CollectionItem) int {
	n := 0
	for _, it := range items {
		if it == nil {
			continue
		}
		if it.Type == ItemRequest {
			n++
		}
		n += CountRequests(it.Items)
	}
	return n
}
