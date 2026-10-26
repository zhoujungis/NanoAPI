package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"nanoapi/internal/bundle"
	"nanoapi/internal/curl"
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

// appVersion 是展示在设置面板中的版本号，发布时通过 -ldflags 注入。
var appVersion = "0.1.0"

const (
	// maxUploadFileBytes 是单个上传文件的大小上限。
	maxUploadFileBytes = 20 << 20
	// maxUploadTotalBytes 是一次 multipart 上传的总大小上限。
	maxUploadTotalBytes = 50 << 20
	// errKindFile 表示构建请求体时读取本地文件失败。
	errKindFile = "file"
)

// App 是 Wails 绑定到前端的结构体。
type App struct {
	ctx      context.Context
	store    *store.Store
	storeErr error
	client   *proxy.Client
}

// NewApp 创建 App 并打开数据存储。
func NewApp() *App {
	// Cookie 会话在本次运行期间共享，用于登录后接口的连续调试。
	a := &App{client: proxy.NewClient()}
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

// SaveTabs 保存当前打开的标签页与活动标签。
func (a *App) SaveTabs(tabs []*model.OpenTab, activeKey string) error {
	s, err := a.ensureStore()
	if err != nil {
		return err
	}
	cleaned := make([]*model.OpenTab, 0, len(tabs))
	for _, tab := range tabs {
		if tab != nil {
			cleaned = append(cleaned, tab)
		}
	}
	s.SetTabs(cleaned, activeKey)
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

// SaveResponseBody 打开系统保存框，把响应体写入用户选择的文件。
// base64Encoded 为 true 时先解码，二进制响应会按原始字节保存。
func (a *App) SaveResponseBody(defaultName, body string, base64Encoded bool) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用尚未就绪")
	}

	data, err := responseBodyBytes(body, base64Encoded)
	if err != nil {
		return "", err
	}

	fallback := "response.txt"
	if base64Encoded {
		fallback = "response.bin"
	}
	name := sanitizeFilename(defaultName, fallback)
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存响应体",
		DefaultFilename: name,
		Filters:         saveFilters(name),
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return path, nil
}

// responseBodyBytes 把前端传来的响应体还原成原始字节。
func responseBodyBytes(body string, base64Encoded bool) ([]byte, error) {
	if !base64Encoded {
		return []byte(body), nil
	}
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("响应体不是有效的 Base64 数据: %w", err)
	}
	return data, nil
}

// sanitizeFilename 只保留文件名部分，并替换 Windows 不允许的字符。
func sanitizeFilename(name, fallback string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, " .")
	if name == "" {
		if fallback == "" {
			return "response.txt"
		}
		return fallback
	}

	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

// saveFilters 根据默认文件名的扩展名生成保存框过滤器。
func saveFilters(name string) []runtime.FileFilter {
	filters := make([]runtime.FileFilter, 0, 2)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if isASCIIAlnum(ext) && len(ext) <= 10 {
		filters = append(filters, runtime.FileFilter{
			DisplayName: strings.ToUpper(ext) + " 文件 (*." + ext + ")",
			Pattern:     "*." + ext,
		})
	}
	return append(filters, runtime.FileFilter{
		DisplayName: "所有文件 (*.*)",
		Pattern:     "*.*",
	})
}

// isASCIIAlnum 判断字符串是否只包含小写字母和数字。
func isASCIIAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// ClearCookies 清空本次会话保留的 Cookie。
func (a *App) ClearCookies() {
	if a.client != nil {
		a.client.ClearCookies()
	}
}

// AppInfo 返回应用版本与数据文件位置。
func (a *App) AppInfo() map[string]string {
	info := map[string]string{"version": appVersion}
	if a.store != nil {
		info["dataFile"] = a.store.Path()
		info["dataDir"] = filepath.Dir(a.store.Path())
	}
	return info
}

// ExportBundle 把集合与环境导出为 JSON 备份文件。
func (a *App) ExportBundle() (*bundle.BundleExportResult, error) {
	s, err := a.ensureStore()
	if err != nil {
		return nil, err
	}
	return bundle.Export(a.ctx, s, appVersion)
}

// ImportBundle 从 JSON 备份文件导入集合与环境。
func (a *App) ImportBundle() (*bundle.BundleImportResult, error) {
	s, err := a.ensureStore()
	if err != nil {
		return nil, err
	}
	return bundle.Import(a.ctx, s)
}

// ParseCurl 把 cURL 命令解析成请求草稿。
func (a *App) ParseCurl(command string) (*curl.CurlImport, error) {
	return curl.Parse(command)
}

// BuildCurl 用当前环境解析变量后生成 Bash 风格的 cURL 命令。
func (a *App) BuildCurl(req model.Request, envID string) (*curl.CurlExport, error) {
	s, err := a.ensureStore()
	if err != nil {
		return nil, err
	}
	data := s.Snapshot()
	resolved, unresolved := resolveRequest(req, buildVars(data.Environments, envID))

	out, err := curl.Build(resolved, data.Settings)
	if err != nil {
		return nil, err
	}
	for _, name := range unresolved {
		out.Warnings = append(out.Warnings, fmt.Sprintf("变量 {{%s}} 未解析，已按原样输出", name))
	}
	out.Warnings = dedupe(out.Warnings)
	return out, nil
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
	resolved, unresolved := resolveRequest(req, vars)

	recordHistory := func(res *proxy.Result) {
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
	}

	body, contentType, err := buildBody(resolved.Body)
	if err != nil {
		res := &proxy.Result{
			Headers:   map[string][]string{},
			Error:     err.Error(),
			ErrorKind: errKindFile,
		}
		res.UnresolvedVars = unresolved
		recordHistory(res)
		return res, nil
	}

	// multipart 的 boundary 由本次请求生成，手写的 Content-Type 必然对不上，直接以自动值为准。
	if resolved.Body.Type == model.BodyMultipart {
		resolved.Headers = dropHeader(resolved.Headers, "Content-Type")
	}

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

	if a.client == nil {
		a.client = proxy.NewClient()
	}
	res := a.client.Send(context.Background(), pOpts)
	res.UnresolvedVars = unresolved
	recordHistory(res)
	return res, nil
}

// resolveRequest 复制请求并对可插值字段做统一解析，避免污染原始请求。
func resolveRequest(req model.Request, vars interp.Vars) (model.Request, []string) {
	resolved := cloneRequestForResolve(req)
	var unresolved []string
	track := func(in string) string {
		out, un := interp.Resolve(in, vars)
		unresolved = append(unresolved, un...)
		return out
	}

	resolved.URL = track(resolved.URL)
	for i := range resolved.Params {
		resolved.Params[i].Key = track(resolved.Params[i].Key)
		resolved.Params[i].Value = track(resolved.Params[i].Value)
	}
	for i := range resolved.Headers {
		resolved.Headers[i].Value = track(resolved.Headers[i].Value)
	}
	switch resolved.Body.Type {
	case model.BodyJSON, model.BodyText:
		resolved.Body.Raw = track(resolved.Body.Raw)
	case model.BodyForm, model.BodyMultipart:
		for i := range resolved.Body.Form {
			resolved.Body.Form[i].Key = track(resolved.Body.Form[i].Key)
			resolved.Body.Form[i].Value = track(resolved.Body.Form[i].Value)
		}
		for i := range resolved.Body.Files {
			resolved.Body.Files[i].Key = track(resolved.Body.Files[i].Key)
			resolved.Body.Files[i].Path = track(resolved.Body.Files[i].Path)
		}
	}
	resolved.Auth.Bearer = track(resolved.Auth.Bearer)
	resolved.Auth.Basic.Username = track(resolved.Auth.Basic.Username)
	resolved.Auth.Basic.Password = track(resolved.Auth.Basic.Password)
	resolved.Auth.APIKey.Key = track(resolved.Auth.APIKey.Key)
	resolved.Auth.APIKey.Value = track(resolved.Auth.APIKey.Value)
	return resolved, dedupe(unresolved)
}

// cloneRequestForResolve 复制请求中会被插值改写的切片，避免污染原始请求。
func cloneRequestForResolve(req model.Request) model.Request {
	out := req
	out.Params = append([]model.KV(nil), req.Params...)
	out.Headers = append([]model.KV(nil), req.Headers...)
	out.Body = req.Body
	out.Body.Form = append([]model.KV(nil), req.Body.Form...)
	out.Body.Files = append([]model.FileField(nil), req.Body.Files...)
	return out
}

// dropHeader 返回去掉指定名称后的请求头副本。
func dropHeader(headers []model.KV, name string) []model.KV {
	out := make([]model.KV, 0, len(headers))
	for _, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h.Key), name) {
			continue
		}
		out = append(out, h)
	}
	return out
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
func buildBody(b model.Body) ([]byte, string, error) {
	switch b.Type {
	case model.BodyJSON:
		return []byte(b.Raw), "application/json", nil
	case model.BodyText:
		return []byte(b.Raw), "text/plain; charset=utf-8", nil
	case model.BodyForm:
		form := url.Values{}
		for _, f := range b.Form {
			if !f.Enabled || strings.TrimSpace(f.Key) == "" {
				continue
			}
			form.Set(f.Key, f.Value)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", nil
	case model.BodyMultipart:
		return buildMultipartBody(b)
	}
	return nil, "", nil
}

// buildMultipartBody 把文本字段与本地文件组装成 multipart/form-data。
func buildMultipartBody(b model.Body) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	for _, f := range b.Form {
		if !f.Enabled || strings.TrimSpace(f.Key) == "" {
			continue
		}
		if err := w.WriteField(f.Key, f.Value); err != nil {
			return nil, "", fmt.Errorf("写入表单字段 %s 失败: %w", f.Key, err)
		}
	}

	var total int64
	for _, f := range b.Files {
		if !f.Enabled || strings.TrimSpace(f.Key) == "" || strings.TrimSpace(f.Path) == "" {
			continue
		}
		info, err := os.Stat(f.Path)
		if err != nil {
			return nil, "", fmt.Errorf("读取文件 %s 失败: %w", f.Path, err)
		}
		if info.IsDir() {
			return nil, "", fmt.Errorf("文件路径是一个目录: %s", f.Path)
		}
		if info.Size() > maxUploadFileBytes {
			return nil, "", fmt.Errorf(
				"文件 %s 超过 %d MB 上限",
				filepath.Base(f.Path), maxUploadFileBytes>>20,
			)
		}
		total += info.Size()
		if total > maxUploadTotalBytes {
			return nil, "", fmt.Errorf("上传文件总大小超过 %d MB 上限", maxUploadTotalBytes>>20)
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, "", fmt.Errorf("读取文件 %s 失败: %w", f.Path, err)
		}
		part, err := w.CreateFormFile(f.Key, filepath.Base(f.Path))
		if err != nil {
			return nil, "", fmt.Errorf("创建文件字段 %s 失败: %w", f.Key, err)
		}
		if _, err := part.Write(data); err != nil {
			return nil, "", fmt.Errorf("写入文件 %s 失败: %w", f.Path, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("生成 multipart 请求体失败: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
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
