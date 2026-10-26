package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"nanoapi/internal/model"
	"nanoapi/internal/store"
)

const (
	bundleFormat   = "nanoapi-bundle"
	bundleVersion  = 1
	maxBundleBytes = 32 << 20
)

// BundleExportResult 是导出备份后的结果。
type BundleExportResult struct {
	Canceled     bool   `json:"canceled"`
	Path         string `json:"path,omitempty"`
	Collections  int    `json:"collections"`
	Requests     int    `json:"requests"`
	Environments int    `json:"environments"`
}

// BundleImportResult 是导入备份后的结果。
type BundleImportResult struct {
	Canceled     bool     `json:"canceled"`
	Collections  int      `json:"collections"`
	Requests     int      `json:"requests"`
	Environments int      `json:"environments"`
	Warnings     []string `json:"warnings,omitempty"`
}

// dataBundle 是导出文件的 JSON 结构。
// 同时兼容直接导入原始 nanoapi.json（没有 format/version 字段）。
type dataBundle struct {
	Format       string                  `json:"format,omitempty"`
	Version      int                     `json:"version,omitempty"`
	AppVersion   string                  `json:"appVersion,omitempty"`
	ExportedAt   int64                   `json:"exportedAt,omitempty"`
	Collections  []*model.CollectionItem `json:"collections"`
	Environments []*model.Environment    `json:"environments"`
}

// Export 把集合与环境导出为 JSON 备份文件。
func Export(ctx context.Context, st *store.Store, appVersion string) (*BundleExportResult, error) {
	if ctx == nil {
		return nil, errors.New("应用尚未就绪")
	}
	if st == nil {
		return nil, errors.New("数据存储不可用")
	}
	data := st.Snapshot()

	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "导出 NanoAPI 备份",
		DefaultFilename: defaultBundleFilename(),
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开保存对话框失败: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return &BundleExportResult{Canceled: true}, nil
	}
	if filepath.Ext(path) == "" {
		path += ".json"
	}

	raw, err := marshalBundle(buildBundle(data, appVersion))
	if err != nil {
		return nil, err
	}
	if err := writeBundleFile(path, raw); err != nil {
		return nil, err
	}
	return &BundleExportResult{
		Path:         path,
		Collections:  countTopLevelCollections(data.Collections),
		Requests:     countBundleRequests(data.Collections),
		Environments: len(data.Environments),
	}, nil
}

// Import 从 JSON 备份文件导入集合与环境，合并到当前数据中。
func Import(ctx context.Context, st *store.Store) (*BundleImportResult, error) {
	if ctx == nil {
		return nil, errors.New("应用尚未就绪")
	}
	if st == nil {
		return nil, errors.New("数据存储不可用")
	}

	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "导入 NanoAPI 备份",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开文件对话框失败: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return &BundleImportResult{Canceled: true}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("读取备份文件失败: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("请选择一个 JSON 文件")
	}
	if info.Size() > maxBundleBytes {
		return nil, fmt.Errorf("备份文件超过 %d MB 上限", maxBundleBytes>>20)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取备份文件失败: %w", err)
	}

	bundle, warnings, err := parseBundle(raw)
	if err != nil {
		return nil, err
	}

	data := st.Snapshot()
	collections, colCount, reqCount := mergeBundleCollections(data.Collections, bundle.Collections)
	environments, envCount, envWarnings := mergeBundleEnvironments(data.Environments, bundle.Environments)

	st.SetCollections(collections)
	st.SetEnvironments(environments)

	return &BundleImportResult{
		Collections:  colCount,
		Requests:     reqCount,
		Environments: envCount,
		Warnings:     dedupe(append(warnings, envWarnings...)),
	}, nil
}

func buildBundle(data model.AppData, appVersion string) dataBundle {
	return dataBundle{
		Format:       bundleFormat,
		Version:      bundleVersion,
		AppVersion:   appVersion,
		ExportedAt:   time.Now().UnixMilli(),
		Collections:  data.Collections,
		Environments: data.Environments,
	}
}

func marshalBundle(bundle dataBundle) ([]byte, error) {
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成备份内容失败: %w", err)
	}
	return append(raw, '\n'), nil
}

func parseBundle(raw []byte) (dataBundle, []string, error) {
	var bundle dataBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return bundle, nil, fmt.Errorf("备份文件不是有效的 JSON: %w", err)
	}

	var warnings []string
	if bundle.Format == "" {
		warnings = append(warnings, "未识别的备份格式，已按集合/环境数据尝试导入")
	} else if bundle.Format != bundleFormat {
		return bundle, nil, fmt.Errorf("不支持的备份格式: %s", bundle.Format)
	}
	if bundle.Version > bundleVersion {
		return bundle, nil, fmt.Errorf(
			"备份版本 %d 高于当前支持的版本 %d", bundle.Version, bundleVersion,
		)
	}
	if len(bundle.Collections) == 0 && len(bundle.Environments) == 0 {
		return bundle, nil, errors.New("文件中没有可导入的集合或环境")
	}
	return bundle, warnings, nil
}

func defaultBundleFilename() string {
	return "nanoapi-backup-" + time.Now().Format("20060102-150405") + ".json"
}

func countTopLevelCollections(items []*model.CollectionItem) int {
	n := 0
	for _, item := range items {
		if item != nil && item.Type == model.ItemFolder {
			n++
		}
	}
	return n
}

func countBundleRequests(items []*model.CollectionItem) int {
	return model.CountRequests(items)
}

// mergeBundleCollections 把导入的集合追加到现有集合，并为所有节点生成新 ID。
func mergeBundleCollections(existing, imported []*model.CollectionItem) ([]*model.CollectionItem, int, int) {
	out := append([]*model.CollectionItem{}, existing...)
	cleaned := make([]*model.CollectionItem, 0, len(imported))
	for _, item := range imported {
		if item == nil {
			continue
		}
		assignFreshCollectionIDs(item)
		cleaned = append(cleaned, item)
	}
	out = append(out, cleaned...)
	return out, countTopLevelCollections(cleaned), countBundleRequests(cleaned)
}

func assignFreshCollectionIDs(node *model.CollectionItem) {
	if node == nil {
		return
	}
	node.ID = model.NewID()

	isRequest := node.Type == model.ItemRequest
	if !isRequest && node.Type != model.ItemFolder {
		isRequest = node.Request != nil
	}
	if isRequest {
		node.Type = model.ItemRequest
		if node.Request == nil {
			req := model.NewRequest()
			node.Request = &req
		}
		node.Request.ID = model.NewID()
		if strings.TrimSpace(node.Request.Name) == "" {
			node.Request.Name = node.Name
		}
		if strings.TrimSpace(node.Name) == "" {
			node.Name = node.Request.Name
		}
		if strings.TrimSpace(node.Name) == "" {
			node.Name = "导入的请求"
			node.Request.Name = node.Name
		}
		if node.Request.Method == "" {
			node.Request.Method = "GET"
		}
		if node.Request.Body.Type == "" {
			node.Request.Body.Type = model.BodyNone
		}
		if node.Request.Auth.Type == "" {
			node.Request.Auth.Type = model.AuthNone
		}
		if node.Request.Auth.APIKey.AddTo == "" {
			node.Request.Auth.APIKey.AddTo = model.AddToHeader
		}
		node.Items = nil
		return
	}

	node.Type = model.ItemFolder
	node.Request = nil
	if strings.TrimSpace(node.Name) == "" {
		node.Name = "导入的文件夹"
	}
	if node.Items == nil {
		node.Items = []*model.CollectionItem{}
	}
	for _, child := range node.Items {
		assignFreshCollectionIDs(child)
	}
}

// mergeBundleEnvironments 追加导入的环境，并为环境生成新 ID。
// 全局变量只有一份：已存在时按 key 合并，现有值优先。
func mergeBundleEnvironments(existing, imported []*model.Environment) ([]*model.Environment, int, []string) {
	out := append([]*model.Environment{}, existing...)
	usedNames := map[string]bool{}
	globalIndex := -1
	for i, env := range out {
		if env == nil {
			continue
		}
		if env.IsGlobal {
			if globalIndex < 0 {
				globalIndex = i
			}
			continue
		}
		usedNames[strings.TrimSpace(env.Name)] = true
	}

	count := 0
	renamed := 0
	globalAdded, globalSkipped, globalMerges := 0, 0, 0
	for _, env := range imported {
		if env == nil {
			continue
		}
		if env.Values == nil {
			env.Values = []model.KV{}
		}

		if env.IsGlobal {
			if globalIndex < 0 {
				env.ID = model.NewID()
				if strings.TrimSpace(env.Name) == "" {
					env.Name = "全局变量"
				}
				out = append(out, env)
				globalIndex = len(out) - 1
			} else {
				target := out[globalIndex]
				copied := *target
				copied.Values = append([]model.KV{}, target.Values...)
				added, skipped := mergeGlobalValues(&copied, env)
				out[globalIndex] = &copied
				globalAdded += added
				globalSkipped += skipped
				globalMerges++
			}
			count++
			continue
		}

		originalName := strings.TrimSpace(env.Name)
		env.ID = model.NewID()
		env.Name = uniqueEnvName(env.Name, usedNames)
		if env.Name != originalName {
			renamed++
		}
		out = append(out, env)
		count++
	}

	var warnings []string
	if globalMerges > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"全局变量已合并：新增 %d 个，跳过重复 %d 个", globalAdded, globalSkipped,
		))
	}
	if renamed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 个环境名称重复，已自动重命名", renamed))
	}
	return out, count, dedupe(warnings)
}

func mergeGlobalValues(target *model.Environment, imported *model.Environment) (added, skipped int) {
	keys := map[string]bool{}
	for _, kv := range target.Values {
		key := strings.TrimSpace(kv.Key)
		if key != "" {
			keys[key] = true
		}
	}
	for _, kv := range imported.Values {
		key := strings.TrimSpace(kv.Key)
		if key == "" {
			continue
		}
		if keys[key] {
			skipped++
			continue
		}
		target.Values = append(target.Values, kv)
		keys[key] = true
		added++
	}
	return added, skipped
}

func uniqueEnvName(name string, used map[string]bool) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "导入环境"
	}
	if !used[base] {
		used[base] = true
		return base
	}
	candidate := base + "（导入）"
	if !used[candidate] {
		used[candidate] = true
		return candidate
	}
	for i := 2; ; i++ {
		candidate = fmt.Sprintf("%s（导入 %d）", base, i)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
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

// writeBundleFile 使用临时文件 + 重命名，避免覆盖时留下半截文件。
func writeBundleFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nanoapi-bundle-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入备份文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭备份文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换备份文件失败: %w", err)
	}
	return nil
}
