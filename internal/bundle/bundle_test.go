package bundle

import (
	"encoding/json"
	"strings"
	"testing"

	"nanoapi/internal/model"
)

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func sampleBundleData() model.AppData {
	return model.AppData{
		Collections: []*model.CollectionItem{
			{
				ID: "c1", Type: model.ItemFolder, Name: "示例集合",
				Items: []*model.CollectionItem{
					{
						ID: "r1", Type: model.ItemRequest, Name: "列表",
						Request: &model.Request{
							ID: "r1", Name: "列表", Method: "GET", URL: "https://api.example.com/users",
						},
					},
				},
			},
		},
		Environments: []*model.Environment{
			{
				ID: "e1", Name: "开发",
				Values: []model.KV{{Key: "baseUrl", Value: "https://dev.example.com", Enabled: true}},
			},
		},
	}
}

func TestBuildAndMarshalBundle(t *testing.T) {
	data := sampleBundleData()
	bundle := buildBundle(data, "test")
	if bundle.Format != bundleFormat || bundle.Version != bundleVersion {
		t.Fatalf("bundle header = %+v", bundle)
	}
	if bundle.AppVersion == "" || bundle.ExportedAt == 0 {
		t.Fatalf("bundle metadata missing: %+v", bundle)
	}

	raw, err := marshalBundle(bundle)
	if err != nil {
		t.Fatalf("marshalBundle: %v", err)
	}
	parsed, warnings, err := parseBundle(raw)
	if err != nil {
		t.Fatalf("parseBundle: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(parsed.Collections) != 1 || len(parsed.Environments) != 1 {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.Collections[0].Items[0].Request.URL != "https://api.example.com/users" {
		t.Fatalf("request URL lost: %+v", parsed.Collections[0].Items[0].Request)
	}
}

func TestParseBundleRawAppData(t *testing.T) {
	raw := []byte(`{
	  "collections": [{"id":"c1","type":"folder","name":"导入","items":[]}],
	  "environments": [{"id":"e1","name":"开发","values":[]}],
	  "history": [{"id":"h1"}],
	  "settings": {"timeoutMs": 1000}
	}`)
	bundle, warnings, err := parseBundle(raw)
	if err != nil {
		t.Fatalf("parseBundle: %v", err)
	}
	if len(bundle.Collections) != 1 || len(bundle.Environments) != 1 {
		t.Fatalf("bundle = %+v", bundle)
	}
	if !hasWarning(warnings, "未识别的备份格式") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestParseBundleErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"invalid json", `{`, "不是有效的 JSON"},
		{"wrong format", `{"format":"other","collections":[{"id":"c"}]}`, "不支持的备份格式"},
		{"newer version", `{"format":"nanoapi-bundle","version":99,"collections":[{"id":"c"}]}`, "高于当前支持"},
		{"empty", `{"format":"nanoapi-bundle","version":1,"collections":[],"environments":[]}`, "没有可导入"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := parseBundle([]byte(c.raw))
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %q, want %q", err, c.want)
			}
		})
	}
}

func TestMergeCollectionsAssignsFreshIDs(t *testing.T) {
	existing := []*model.CollectionItem{
		{ID: "existing", Type: model.ItemFolder, Name: "已有集合"},
	}
	imported := []*model.CollectionItem{
		{
			ID: "old", Type: model.ItemFolder, Name: "导入集合",
			Items: []*model.CollectionItem{
				{
					ID: "old-req", Type: model.ItemRequest, Name: "旧请求",
					Request: &model.Request{ID: "old-req", Name: "旧请求", Method: "POST", URL: "https://x"},
				},
			},
		},
	}

	out, collections, requests := mergeBundleCollections(existing, imported)
	if len(out) != 2 {
		t.Fatalf("merged length = %d", len(out))
	}
	if collections != 1 || requests != 1 {
		t.Fatalf("counts = %d collections / %d requests", collections, requests)
	}
	if out[0].ID != "existing" {
		t.Fatalf("existing collection was changed: %+v", out[0])
	}
	got := out[1]
	if got.ID == "old" || got.ID == "" {
		t.Fatalf("imported collection ID was not refreshed: %q", got.ID)
	}
	child := got.Items[0]
	if child.ID == "old-req" || child.ID == "" {
		t.Fatalf("imported node ID was not refreshed: %q", child.ID)
	}
	if child.Request.ID == "old-req" || child.Request.ID == "" {
		t.Fatalf("imported request ID was not refreshed: %q", child.Request.ID)
	}
	if child.Request.URL != "https://x" || child.Request.Method != "POST" {
		t.Fatalf("request content changed: %+v", child.Request)
	}
}

func TestMergeCollectionsNormalizesInvalidNodes(t *testing.T) {
	imported := []*model.CollectionItem{
		{ID: "a", Type: "", Name: "", Items: nil},
		{ID: "b", Type: "", Name: "", Request: &model.Request{URL: "https://x"}},
		{ID: "c", Type: model.ItemRequest, Name: ""},
	}
	out, collections, requests := mergeBundleCollections(nil, imported)
	if len(out) != 3 {
		t.Fatalf("out = %+v", out)
	}
	if collections != 1 {
		t.Fatalf("collections = %d, want 1 folder", collections)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if out[0].Type != model.ItemFolder || out[0].Items == nil {
		t.Fatalf("empty node not normalized: %+v", out[0])
	}
	if out[1].Type != model.ItemRequest || out[1].Request == nil {
		t.Fatalf("request node not normalized: %+v", out[1])
	}
	if out[2].Request == nil || out[2].Request.Method != "GET" {
		t.Fatalf("missing request not filled: %+v", out[2])
	}
}

func TestMergeEnvironments(t *testing.T) {
	existingGlobal := &model.Environment{
		ID: "g", Name: "全局变量", IsGlobal: true,
		Values: []model.KV{{Key: "token", Value: "old", Enabled: true}},
	}
	existing := []*model.Environment{
		{ID: "dev", Name: "开发", Values: []model.KV{{Key: "baseUrl", Value: "old", Enabled: true}}},
		existingGlobal,
	}
	imported := []*model.Environment{
		{
			ID: "dev-imported", Name: "开发",
			Values: []model.KV{{Key: "baseUrl", Value: "new", Enabled: true}},
		},
		{
			ID: "g-imported", Name: "全局变量", IsGlobal: true,
			Values: []model.KV{
				{Key: "token", Value: "new", Enabled: true},
				{Key: "extra", Value: "1", Enabled: true},
			},
		},
		{ID: "test-imported", Name: "测试", Values: []model.KV{}},
	}

	out, count, warnings := mergeBundleEnvironments(existing, imported)
	if len(out) != 4 {
		t.Fatalf("out length = %d, want 4", len(out))
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if out[0].ID != "dev" {
		t.Fatalf("existing environment changed: %+v", out[0])
	}
	renamed := out[2]
	if renamed.Name != "开发（导入）" || renamed.ID == "dev-imported" || renamed.ID == "" {
		t.Fatalf("renamed environment = %+v", renamed)
	}
	mergedGlobal := out[1]
	if mergedGlobal.Name != "全局变量" || !mergedGlobal.IsGlobal {
		t.Fatalf("global = %+v", mergedGlobal)
	}
	if len(mergedGlobal.Values) != 2 {
		t.Fatalf("global values = %+v", mergedGlobal.Values)
	}
	if mergedGlobal.Values[0].Key != "token" || mergedGlobal.Values[0].Value != "old" {
		t.Fatalf("existing token should win: %+v", mergedGlobal.Values)
	}
	if mergedGlobal.Values[1].Key != "extra" || mergedGlobal.Values[1].Value != "1" {
		t.Fatalf("extra value missing: %+v", mergedGlobal.Values)
	}
	if len(existingGlobal.Values) != 1 {
		t.Fatalf("merge mutated the existing global in place: %+v", existingGlobal.Values)
	}
	if !hasWarning(warnings, "全局变量已合并") {
		t.Fatalf("warnings = %v", warnings)
	}
	if !hasWarning(warnings, "环境名称重复") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestMergeEnvironmentsCreatesGlobalWhenMissing(t *testing.T) {
	imported := []*model.Environment{
		{ID: "g", Name: "全局变量", IsGlobal: true, Values: []model.KV{{Key: "a", Value: "1", Enabled: true}}},
		{ID: "dev", Name: "开发", Values: []model.KV{{Key: "b", Value: "2", Enabled: true}}},
	}
	out, count, warnings := mergeBundleEnvironments(nil, imported)
	if len(out) != 2 || count != 2 {
		t.Fatalf("out = %+v, count = %d", out, count)
	}
	if !out[0].IsGlobal {
		t.Fatalf("global not created: %+v", out[0])
	}
	if out[0].ID == "g" || out[0].ID == "" {
		t.Fatalf("global ID not refreshed: %q", out[0].ID)
	}
	if out[1].Name != "开发" {
		t.Fatalf("environment name = %q", out[1].Name)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestUniqueEnvName(t *testing.T) {
	used := map[string]bool{"开发": true, "开发（导入）": true}
	if got := uniqueEnvName("开发", used); got != "开发（导入 2）" {
		t.Fatalf("uniqueEnvName = %q", got)
	}
	if got := uniqueEnvName("  ", used); got != "导入环境" {
		t.Fatalf("uniqueEnvName empty = %q", got)
	}
}

func TestParseBundleRoundTripPreservesMultipartFiles(t *testing.T) {
	req := model.NewRequest()
	req.Body = model.Body{
		Type:  model.BodyMultipart,
		Form:  []model.KV{{Key: "note", Value: "hello", Enabled: true}},
		Files: []model.FileField{{Key: "file", Path: `C:\tmp\a.txt`, Enabled: true}},
	}
	data := model.AppData{
		Collections: []*model.CollectionItem{
			{ID: "c", Type: model.ItemFolder, Name: "上传", Items: []*model.CollectionItem{
				{ID: "n", Type: model.ItemRequest, Name: "上传文件", Request: &req},
			}},
		},
	}
	raw, err := marshalBundle(buildBundle(data, "test"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := parseBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := parsed.Collections[0].Items[0].Request.Body
	if body.Type != model.BodyMultipart || len(body.Files) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.Files[0].Path != `C:\tmp\a.txt` {
		t.Fatalf("file path = %q", body.Files[0].Path)
	}
}

func TestBundleJSONHasExpectedFields(t *testing.T) {
	raw, err := marshalBundle(buildBundle(sampleBundleData(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"format", "version", "appVersion", "exportedAt", "collections", "environments"} {
		if _, ok := probe[key]; !ok {
			t.Fatalf("missing bundle field %q: %s", key, raw)
		}
	}
}
