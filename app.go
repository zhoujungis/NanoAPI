package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"nanoapi/internal/interp"
	"nanoapi/internal/model"
	"nanoapi/internal/proxy"
	"nanoapi/internal/store"
)

// SendOptions 是前端在发送请求时附带的可选项。
// 布尔字段使用指针，以便区分「未指定，沿用全局设置」与「显式关闭」。
type SendOptions struct {
	TimeoutMS       int   `json:"timeoutMs"`
	FollowRedirects *bool `json:"followRedirects"`
	InsecureTLS     *bool `json:"insecureTls"`
}

// App 是 Wails 绑定到前端的结构体。
type App struct {
	ctx     context.Context
	store   *store.Store
	storeErr error
}

// NewApp 创建 App 并打开数据存储。
func NewApp() *App {
	a := &App{}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	st, err := store.Open(filepath.Join(dir, "NanoAPI", "nanoapi.json"))
	if err != nil {
		a.storeErr = err
		return a
	}
	a.store = st
	return a
}

// startup 在窗口创建时调用，保存可供 runtime 使用的上下文。
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// shutdown 在窗口关闭时调用，确保数据落盘。
func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) ensureStore() (*store.Store, error) {
	if a.store == nil {
		if a.storeErr != nil {
			return nil, fmt.Errorf("数据存储不可用: %w", a.storeErr)
		}
		return nil, fmt.Errorf("数据存储不可用")
	}
	return a.store, nil
}

// LoadAll 返回集合、环境、历史与设置。
func (a *App) LoadAll() (*model.AppData, error) {
	s, err := a.ensureStore()
	if err != nil {
		return nil, err
	}
	d := s.Snapshot()
	return &d, nil
}

// SaveCollections 保存整个集合树。
func (a *App) SaveCollections(cols []*model.CollectionItem) error {
	s, err := a.ensureStore()
	if err != nil {
		return err
	}
	s.SetCollections(cols)
	return nil
}

// SaveEnvironments 保存环境列表。
func (a *App) SaveEnvironments(envs []*model.Environment) error {
	s, err := a.ensureStore()
	if err != nil {
		return err
	}
	s.SetEnvironments(envs)
	return nil
}

// SaveSettings 保存全局设置。
func (a *App) SaveSettings(set model.Settings) error {
	s, err := a.ensureStore()
	if err != nil {
		return err
	}
	s.SetSettings(set)
	return nil
}

// ClearHistory 清空历史记录。
func (a *App) ClearHistory() error {
	s, err := a.ensureStore()
	if err != nil {
		return err
	}
	s.ClearHistory()
	return nil
}

// PickFile 打开系统文件选择框，返回所选文件的绝对路径。
func (a *App) PickFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用尚未就绪")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
}

// AppInfo 返回应用版本与数据文件位置。
func (a *App) AppInfo() map[string]string {
	info := map[string]string{"version": "0.1.0"}
	if a.store != nil {
		info["dataFile"] = a.store.Path()
		info["dataDir"] = filepath.Dir(a.store.Path())
	}
	return info
}

// SendRequest 解析变量后发送请求，并把结果写入历史。
//
// 网络层错误不会作为 Go error 返回，而是记录在 Result.Error 中，
// 以便前端区分「请求失败」与「程序缺陷」。
func (a *App) SendRequest(req model.Request, envID string, opts SendOptions) (*proxy.Result, error) {
	s, err := a.ensureStore()
	if err != nil {
		return nil, err
	}
	data := s.Snapshot()

	vars := buildVars(data.Environments, envID)
	resolved := req
	var unresolved []string
	track := func(in string) string {
		out, un := interp.Resolve(in, vars)
		unresolved = append(unresolved, un...)
		return out
	}

	resolved.URL = track(resolved.URL)
	for i := range resolved.Params {
		resolved.Params[i].Value = track(resolved.Params[i].Value)
	}
	for i := range resolved.Headers {
		resolved.Headers[i].Value = track(resolved.Headers[i].Value)
	}
	switch resolved.Body.Type {
	case model.BodyJSON, model.BodyText:
		resolved.Body.Raw = track(resolved.Body.Raw)
	case model.BodyForm:
		for i := range resolved.Body.Form {
			resolved.Body.Form[i].Value = track(resolved.Body.Form[i].Value)
		}
	}
	resolved.Auth.Bearer = track(resolved.Auth.Bearer)
	resolved.Auth.Basic.Username = track(resolved.Auth.Basic.Username)
	resolved.Auth.Basic.Password = track(resolved.Auth.Basic.Password)
	resolved.Auth.APIKey.Key = track(resolved.Auth.APIKey.Key)
	resolved.Auth.APIKey.Value = track(resolved.Auth.APIKey.Value)

	body, contentType := buildBody(resolved.Body)

	pOpts := proxy.Options{
		Method:          resolved.Method,
		URL:             resolved.URL,
		Params:          resolved.Params,
		Headers:         resolved.Headers,
		Auth:            resolved.Auth,
		Body:            body,
		ContentType:     contentType,
		TimeoutMS:       pickInt(opts.TimeoutMS, data.Settings.TimeoutMS, proxy.DefaultTimeoutMS),
		FollowRedirects: pickBool(opts.FollowRedirects, data.Settings.FollowRedirects),
		InsecureTLS:     pickBool(opts.InsecureTLS, data.Settings.InsecureTLS),
		MaxBodyBytes:    proxy.DefaultMaxBodyBytes,
	}

	res := proxy.Send(context.Background(), pOpts)
	res.UnresolvedVars = dedupe(unresolved)

	s.AddHistory(&model.HistoryEntry{
		ID:         model.NewID(),
		Request:    req,
		Status:     res.Status,
		StatusText: res.StatusText,
		DurationMS: res.DurationMS,
		Size:       res.Size,
		Timestamp:  time.Now().UnixMilli(),
		Error:      res.Error,
	})
	return res, nil
}

// buildVars 组装变量表：全局环境作为底，当前环境覆盖同名变量。
func buildVars(envs []*model.Environment, envID string) interp.Vars {
	vars := interp.Vars{}
	add := func(e *model.Environment) {
		for _, kv := range e.Values {
			if !kv.Enabled || strings.TrimSpace(kv.Key) == "" {
				continue
			}
			vars[kv.Key] = kv.Value
		}
	}
	for _, e := range envs {
		if e.IsGlobal {
			add(e)
		}
	}
	for _, e := range envs {
		if !e.IsGlobal && e.ID == envID {
			add(e)
		}
	}
	return vars
}

// buildBody 根据请求体类型生成实际的字节内容与 Content-Type。
func buildBody(b model.Body) ([]byte, string) {
	switch b.Type {
	case model.BodyJSON:
		return []byte(b.Raw), "application/json"
	case model.BodyText:
		return []byte(b.Raw), "text/plain; charset=utf-8"
	case model.BodyForm:
		form := url.Values{}
		for _, f := range b.Form {
			if !f.Enabled || strings.TrimSpace(f.Key) == "" {
				continue
			}
			form.Set(f.Key, f.Value)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded"
	}
	return nil, ""
}

func pickInt(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func pickBool(v *bool, fallback bool) bool {
	if v != nil {
		return *v
	}
	return fallback
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
