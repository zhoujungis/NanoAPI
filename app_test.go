package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nanoapi/internal/model"
	"nanoapi/internal/proxy"
	"nanoapi/internal/store"
)

func TestBuildVarsGlobalThenEnvironmentOverride(t *testing.T) {
	envs := []*model.Environment{
		{
			ID: "global", IsGlobal: true,
			Values: []model.KV{
				{Key: "baseUrl", Value: "https://global.example.com", Enabled: true},
				{Key: "token", Value: "global-token", Enabled: true},
			},
		},
		{
			ID: "dev", Name: "开发",
			Values: []model.KV{
				{Key: "baseUrl", Value: "https://dev.example.com", Enabled: true},
				{Key: "disabled", Value: "x", Enabled: false},
				{Key: "  ", Value: "blank", Enabled: true},
			},
		},
	}

	vars := buildVars(envs, "dev")
	if vars["baseUrl"] != "https://dev.example.com" {
		t.Fatalf("baseUrl = %q, want environment value", vars["baseUrl"])
	}
	if vars["token"] != "global-token" {
		t.Fatalf("token = %q, want global value", vars["token"])
	}
	if _, ok := vars["disabled"]; ok {
		t.Fatal("disabled variable should be skipped")
	}
	if len(vars) != 2 {
		t.Fatalf("vars = %v, want 2 entries", vars)
	}
}

func TestBuildVarsIgnoresOtherEnvironments(t *testing.T) {
	envs := []*model.Environment{
		{ID: "a", Values: []model.KV{{Key: "x", Value: "a", Enabled: true}}},
		{ID: "b", Values: []model.KV{{Key: "x", Value: "b", Enabled: true}}},
	}

	vars := buildVars(envs, "b")
	if vars["x"] != "b" {
		t.Fatalf("x = %q, want b", vars["x"])
	}
	if len(vars) != 1 {
		t.Fatalf("vars = %v, want only the active environment", vars)
	}
}

func TestBuildBody(t *testing.T) {
	body, ct, err := buildBody(model.Body{Type: model.BodyJSON, Raw: `{"a":1}`})
	if err != nil || string(body) != `{"a":1}` || ct != "application/json" {
		t.Fatalf("json body = %q, content type = %q, err = %v", body, ct, err)
	}

	body, ct, err = buildBody(model.Body{Type: model.BodyText, Raw: "hello"})
	if err != nil || string(body) != "hello" || ct != "text/plain; charset=utf-8" {
		t.Fatalf("text body = %q, content type = %q, err = %v", body, ct, err)
	}

	body, ct, err = buildBody(model.Body{
		Type: model.BodyForm,
		Form: []model.KV{
			{Key: "name", Value: "张三", Enabled: true},
			{Key: "skip", Value: "x", Enabled: false},
			{Key: "", Value: "blank", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("form body: %v", err)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("form body is not valid query: %v", err)
	}
	if form.Get("name") != "张三" {
		t.Fatalf("form name = %q", form.Get("name"))
	}
	if _, ok := form["skip"]; ok {
		t.Fatal("disabled form field should be skipped")
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Fatalf("form content type = %q", ct)
	}

	body, ct, err = buildBody(model.Body{Type: model.BodyNone})
	if err != nil || body != nil || ct != "" {
		t.Fatalf("none body = %q / %q / %v, want empty", body, ct, err)
	}
}

func TestBuildMultipartBody(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("file-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, ct, err := buildBody(model.Body{
		Type: model.BodyMultipart,
		Form: []model.KV{
			{Key: "name", Value: "张三", Enabled: true},
			{Key: "skip", Value: "x", Enabled: false},
		},
		Files: []model.FileField{
			{Key: "upload", Path: filePath, Enabled: true},
			{Key: "skipFile", Path: filePath, Enabled: false},
			{Key: "blank", Path: "", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("buildBody: %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", ct)
	}

	mr := multipart.NewReader(
		bytes.NewReader(body),
		strings.TrimPrefix(ct, "multipart/form-data; boundary="),
	)
	form, err := mr.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	defer form.RemoveAll()

	if got := form.Value["name"]; len(got) != 1 || got[0] != "张三" {
		t.Fatalf("form value name = %v", got)
	}
	if _, ok := form.Value["skip"]; ok {
		t.Fatal("disabled form field should be skipped")
	}

	files := form.File["upload"]
	if len(files) != 1 {
		t.Fatalf("upload files = %v", files)
	}
	if files[0].Filename != "note.txt" {
		t.Fatalf("filename = %q", files[0].Filename)
	}
	f, err := files[0].Open()
	if err != nil {
		t.Fatalf("open uploaded file: %v", err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != "file-content" {
		t.Fatalf("uploaded content = %q", data)
	}
	if len(form.File["skipFile"]) != 0 || len(form.File["blank"]) != 0 {
		t.Fatalf("disabled/blank files should be skipped: %v", form.File)
	}
}

func TestCloneRequestForResolveCopiesSlices(t *testing.T) {
	req := model.Request{
		Params:  []model.KV{{Key: "p", Value: "{{v}}", Enabled: true}},
		Headers: []model.KV{{Key: "h", Value: "{{v}}", Enabled: true}},
		Body: model.Body{
			Type:  model.BodyMultipart,
			Form:  []model.KV{{Key: "f", Value: "{{v}}", Enabled: true}},
			Files: []model.FileField{{Key: "file", Path: "{{dir}}/a.txt", Enabled: true}},
		},
	}

	got := cloneRequestForResolve(req)
	got.Params[0].Value = "changed"
	got.Headers[0].Value = "changed"
	got.Body.Form[0].Value = "changed"
	got.Body.Files[0].Path = "changed"

	if req.Params[0].Value != "{{v}}" ||
		req.Headers[0].Value != "{{v}}" ||
		req.Body.Form[0].Value != "{{v}}" ||
		req.Body.Files[0].Path != "{{dir}}/a.txt" {
		t.Fatalf("original request was mutated: %+v", req)
	}
}

func TestDropHeader(t *testing.T) {
	headers := []model.KV{
		{Key: "Content-Type", Value: "text/plain", Enabled: true},
		{Key: "X-Token", Value: "abc", Enabled: true},
	}
	got := dropHeader(headers, "content-type")
	if len(got) != 1 || got[0].Key != "X-Token" {
		t.Fatalf("dropHeader = %+v", got)
	}
	if len(headers) != 2 || headers[0].Key != "Content-Type" {
		t.Fatal("dropHeader mutated the input slice")
	}
}

func TestSendRequestKeepsTemplatesInHistory(t *testing.T) {
	var gotQuery, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("p")
		gotHeader = r.Header.Get("X-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "nanoapi.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	st.SetEnvironments([]*model.Environment{{
		ID: "global", IsGlobal: true,
		Values: []model.KV{{Key: "token", Value: "resolved", Enabled: true}},
	}})

	app := &App{store: st, client: proxy.NewClient()}
	req := model.Request{
		ID:      "req-1",
		Name:    "带变量",
		Method:  "GET",
		URL:     srv.URL,
		Params:  []model.KV{{Key: "p", Value: "{{token}}", Enabled: true}},
		Headers: []model.KV{{Key: "X-Token", Value: "{{token}}", Enabled: true}},
		Body:    model.Body{Type: model.BodyNone},
		Auth:    model.Auth{Type: model.AuthNone},
	}

	res, err := app.SendRequest(req, "", SendOptions{})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("request failed: %s", res.Error)
	}
	if gotQuery != "resolved" || gotHeader != "resolved" {
		t.Fatalf("server got query=%q header=%q, want resolved", gotQuery, gotHeader)
	}

	history := st.Snapshot().History
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	saved := history[0].Request
	if saved.Params[0].Value != "{{token}}" || saved.Headers[0].Value != "{{token}}" {
		t.Fatalf("history stored resolved values: params=%+v headers=%+v", saved.Params, saved.Headers)
	}
}

func TestSendRequestMultipartUpload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "upload.txt")
	if err := os.WriteFile(filePath, []byte("upload-body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotType, gotNote, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotNote = r.FormValue("note")
		f, _, err := r.FormFile("file")
		if err == nil {
			data, _ := io.ReadAll(f)
			gotFile = string(data)
			f.Close()
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(dir, "nanoapi.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	app := &App{store: st, client: proxy.NewClient()}
	req := model.Request{
		ID:     "upload",
		Name:   "上传文件",
		Method: "POST",
		URL:    srv.URL,
		Headers: []model.KV{
			{Key: "Content-Type", Value: "text/plain", Enabled: true},
		},
		Body: model.Body{
			Type:  model.BodyMultipart,
			Form:  []model.KV{{Key: "note", Value: "hello", Enabled: true}},
			Files: []model.FileField{{Key: "file", Path: filePath, Enabled: true}},
		},
		Auth: model.Auth{Type: model.AuthNone},
	}

	res, err := app.SendRequest(req, "", SendOptions{})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("request failed: %s", res.Error)
	}
	if !strings.HasPrefix(gotType, "multipart/form-data; boundary=") {
		t.Fatalf("server content type = %q, want generated multipart type", gotType)
	}
	if gotNote != "hello" || gotFile != "upload-body" {
		t.Fatalf("server got note=%q file=%q", gotNote, gotFile)
	}

	history := st.Snapshot().History
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	saved := history[0].Request
	if saved.Body.Type != model.BodyMultipart || len(saved.Body.Files) != 1 {
		t.Fatalf("history body = %+v", saved.Body)
	}
	if saved.Body.Files[0].Path != filePath {
		t.Fatalf("history file path = %q", saved.Body.Files[0].Path)
	}
	if saved.Headers[0].Value != "text/plain" {
		t.Fatalf("history header was mutated: %+v", saved.Headers)
	}
}

func TestBuildMultipartBodyReportsMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	_, _, err := buildBody(model.Body{
		Type:  model.BodyMultipart,
		Files: []model.FileField{{Key: "file", Path: missing, Enabled: true}},
	})
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "读取文件") {
		t.Fatalf("error = %q", err)
	}
}

func TestDedupe(t *testing.T) {
	if got := dedupe(nil); got != nil {
		t.Fatalf("dedupe(nil) = %v, want nil", got)
	}
	got := dedupe([]string{"b", "", "a", "b", "a"})
	if len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("dedupe = %v, want [b a]", got)
	}
}

func TestSaveTabs(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "nanoapi.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	app := &App{store: st}
	req := model.NewRequest()
	req.URL = "https://api.example.com/users"
	tabs := []*model.OpenTab{
		nil,
		{Key: "tab-1", Request: req, Dirty: true},
	}
	if err := app.SaveTabs(tabs, "tab-1"); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	snap := st.Snapshot()
	if len(snap.Tabs) != 1 {
		t.Fatalf("tabs = %+v", snap.Tabs)
	}
	if snap.Tabs[0].Key != "tab-1" || !snap.Tabs[0].Dirty {
		t.Fatalf("tab = %+v", snap.Tabs[0])
	}
	if snap.Tabs[0].Request.URL != "https://api.example.com/users" {
		t.Fatalf("tab request = %+v", snap.Tabs[0].Request)
	}
	if snap.ActiveTabKey != "tab-1" {
		t.Fatalf("activeTabKey = %q", snap.ActiveTabKey)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name     string
		fallback string
		want     string
	}{
		{"users.json", "response.txt", "users.json"},
		{`C:\tmp\report.pdf`, "response.txt", "report.pdf"},
		{"a/b/c.txt", "response.txt", "c.txt"},
		{`a:b*c?.json`, "response.txt", "a_b_c_.json"},
		{"trailing.  ", "response.txt", "trailing"},
		{"", "response.bin", "response.bin"},
		{" . ", "response.txt", "response.txt"},
		{"..", "response.txt", "response.txt"},
		{"", "", "response.txt"},
	}
	for _, tc := range cases {
		if got := sanitizeFilename(tc.name, tc.fallback); got != tc.want {
			t.Errorf("sanitizeFilename(%q, %q) = %q, want %q", tc.name, tc.fallback, got, tc.want)
		}
	}

	long := strings.Repeat("报", 130)
	got := sanitizeFilename(long, "response.txt")
	if len([]rune(got)) != 120 {
		t.Fatalf("long filename rune length = %d, want 120", len([]rune(got)))
	}
}

func TestSaveFilters(t *testing.T) {
	filters := saveFilters("users.JSON")
	if len(filters) != 2 {
		t.Fatalf("filters = %+v, want extension + all files", filters)
	}
	if filters[0].Pattern != "*.json" || !strings.Contains(filters[0].DisplayName, "JSON") {
		t.Fatalf("extension filter = %+v", filters[0])
	}
	if filters[1].Pattern != "*.*" {
		t.Fatalf("all-files filter = %+v", filters[1])
	}

	if got := saveFilters("noext"); len(got) != 1 || got[0].Pattern != "*.*" {
		t.Fatalf("filters without extension = %+v", got)
	}
	if got := saveFilters("file.verylongextension"); len(got) != 1 {
		t.Fatalf("long extension filters = %+v", got)
	}
}

func TestResponseBodyBytes(t *testing.T) {
	data, err := responseBodyBytes("hello", false)
	if err != nil || !bytes.Equal(data, []byte("hello")) {
		t.Fatalf("text body = %q, err = %v", data, err)
	}

	raw := []byte{0, 1, 2, 255}
	encoded := base64.StdEncoding.EncodeToString(raw)
	data, err = responseBodyBytes(encoded, true)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatalf("base64 body = %v, err = %v", data, err)
	}

	if _, err := responseBodyBytes("not-base64!", true); err == nil {
		t.Fatal("invalid base64 should return an error")
	}
}

func TestAppBuildCurlResolvesEnvironmentAndWarns(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "nanoapi.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	st.SetEnvironments([]*model.Environment{{
		ID: "global", IsGlobal: true,
		Values: []model.KV{{Key: "baseUrl", Value: "https://api.example.com", Enabled: true}},
	}})
	st.SetSettings(model.Settings{TimeoutMS: 2000, FollowRedirects: false, InsecureTLS: true})

	app := &App{store: st}
	req := model.Request{
		Method: "GET",
		URL:    "{{baseUrl}}/users/{{id}}",
		Headers: []model.KV{
			{Key: "Authorization", Value: "Bearer {{token}}", Enabled: true},
		},
		Body: model.Body{Type: model.BodyNone},
	}
	out, err := app.BuildCurl(req, "")
	if err != nil {
		t.Fatalf("BuildCurl: %v", err)
	}
	if !strings.Contains(out.Command, "https://api.example.com/users/") {
		t.Fatalf("resolved URL missing: %s", out.Command)
	}
	if !strings.Contains(out.Command, "-k") || strings.Contains(out.Command, "-L") {
		t.Fatalf("TLS/redirect flags wrong: %s", out.Command)
	}
	if !strings.Contains(out.Command, "--max-time 2") {
		t.Fatalf("timeout flag missing: %s", out.Command)
	}
	if !hasWarning(out.Warnings, "id") || !hasWarning(out.Warnings, "token") {
		t.Fatalf("missing unresolved warnings: %v", out.Warnings)
	}
}

func hasWarning(warnings []string, substr string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, substr) {
			return true
		}
	}
	return false
}

func TestPickHelpers(t *testing.T) {
	if got := pickInt(0, 15000, 30000); got != 15000 {
		t.Fatalf("pickInt = %d, want 15000", got)
	}
	if got := pickInt(0, 0); got != 0 {
		t.Fatalf("pickInt = %d, want 0", got)
	}

	yes, no := true, false
	if got := pickBool(&no, true); got != false {
		t.Fatalf("pickBool explicit false = %v", got)
	}
	if got := pickBool(&yes, false); got != true {
		t.Fatalf("pickBool explicit true = %v", got)
	}
	if got := pickBool(nil, true); got != true {
		t.Fatalf("pickBool fallback = %v", got)
	}
}
