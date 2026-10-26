package curl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nanoapi/internal/model"
)

func mustParseCurl(t *testing.T, command string) *CurlImport {
	t.Helper()
	got, err := parseCurl(command)
	if err != nil {
		t.Fatalf("parseCurl(%q): %v", command, err)
	}
	return got
}

func headerValue(headers []model.KV, key string) (string, bool) {
	for _, h := range headers {
		if strings.EqualFold(h.Key, key) {
			return h.Value, true
		}
	}
	return "", false
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestSplitCurlCommand(t *testing.T) {
	got, err := splitCurlCommand(`curl -X POST 'https://x/users' -H "Accept: application/json" -d '{"name":"张三"}'`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	want := []string{
		"curl", "-X", "POST", "https://x/users",
		"-H", "Accept: application/json", "-d", `{"name":"张三"}`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

func TestSplitCurlCommandLineContinuations(t *testing.T) {
	got, err := splitCurlCommand("curl \\\n  -X POST \\\n  'https://x'")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if strings.Join(got, " ") != "curl -X POST https://x" {
		t.Fatalf("tokens = %#v", got)
	}

	got, err = splitCurlCommand("curl ^\r\n  -H \"accept: application/json\" ^\r\n  https://x")
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if strings.Join(got, " ") != "curl -H accept: application/json https://x" {
		t.Fatalf("tokens = %#v", got)
	}
}

func TestSplitCurlCommandKeepsWindowsPaths(t *testing.T) {
	got, err := splitCurlCommand(`curl -F file=@C:\temp\a.txt`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if got[2] != `file=@C:\temp\a.txt` {
		t.Fatalf("token = %q", got[2])
	}

	got, err = splitCurlCommand(`curl -F "file=@C:\temp\a.txt"`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if got[2] != `file=@C:\temp\a.txt` {
		t.Fatalf("quoted token = %q", got[2])
	}
}

func TestSplitCurlCommandEscapedQuotesAndEmpty(t *testing.T) {
	got, err := splitCurlCommand(`curl -d "{\"a\":1}" -H ""`)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if got[2] != `{"a":1}` {
		t.Fatalf("data token = %q", got[2])
	}
	if got[4] != "" {
		t.Fatalf("empty header token = %q", got[4])
	}
}

func TestSplitCurlCommandUnclosedQuote(t *testing.T) {
	if _, err := splitCurlCommand(`curl 'https://x`); err == nil {
		t.Fatal("expected an unclosed quote error")
	}
}

func TestParseCurlSimpleGet(t *testing.T) {
	got := mustParseCurl(t, `curl 'https://api.example.com/users?page=1'`)
	req := got.Request
	if req.Method != "GET" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.URL != "https://api.example.com/users?page=1" {
		t.Fatalf("url = %q", req.URL)
	}
	if req.ID != "" {
		t.Fatalf("imported request should be a draft, id = %q", req.ID)
	}
	if req.Name != "users" {
		t.Fatalf("name = %q", req.Name)
	}
	if req.Body.Type != model.BodyNone {
		t.Fatalf("body type = %q", req.Body.Type)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestParseCurlJSONPost(t *testing.T) {
	got := mustParseCurl(t, `curl -X POST 'https://api.example.com/users' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json' \
  --data-raw '{"name":"张三"}'`)
	req := got.Request
	if req.Method != "POST" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Body.Type != model.BodyJSON || req.Body.Raw != `{"name":"张三"}` {
		t.Fatalf("body = %+v", req.Body)
	}
	if v, ok := headerValue(req.Headers, "Content-Type"); !ok || v != "application/json" {
		t.Fatalf("content-type = %q ok=%v", v, ok)
	}
	if v, ok := headerValue(req.Headers, "Accept"); !ok || v != "application/json" {
		t.Fatalf("accept = %q ok=%v", v, ok)
	}
}

func TestParseCurlDefaultFormPost(t *testing.T) {
	got := mustParseCurl(t, `curl 'https://x/form' -d 'a=1&b=2'`)
	req := got.Request
	if req.Method != "POST" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Body.Type != model.BodyText || req.Body.Raw != "a=1&b=2" {
		t.Fatalf("body = %+v", req.Body)
	}
	if v, ok := headerValue(req.Headers, "Content-Type"); !ok || v != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type = %q ok=%v", v, ok)
	}
}

func TestParseCurlMultipleDataFlags(t *testing.T) {
	got := mustParseCurl(t, `curl https://x -d 'a=1' -d 'b=2'`)
	if got.Request.Body.Raw != "a=1&b=2" {
		t.Fatalf("body = %q", got.Request.Body.Raw)
	}
}

func TestParseCurlJSONFlag(t *testing.T) {
	got := mustParseCurl(t, `curl --json '{"a":1}' 'https://x'`)
	req := got.Request
	if req.Method != "POST" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Body.Type != model.BodyJSON {
		t.Fatalf("body type = %q", req.Body.Type)
	}
	if v, _ := headerValue(req.Headers, "Content-Type"); v != "application/json" {
		t.Fatalf("content-type = %q", v)
	}
	if v, _ := headerValue(req.Headers, "Accept"); v != "application/json" {
		t.Fatalf("accept = %q", v)
	}
}

func TestParseCurlMultipart(t *testing.T) {
	got := mustParseCurl(t, `curl -X POST 'https://x/upload' \
  -F 'note=hello' \
  -F 'file=@C:\temp\a.txt' \
  -F 'other=@/tmp/b.bin;type=application/octet-stream'`)
	req := got.Request
	if req.Body.Type != model.BodyMultipart {
		t.Fatalf("body type = %q", req.Body.Type)
	}
	if len(req.Body.Form) != 1 || req.Body.Form[0].Key != "note" || req.Body.Form[0].Value != "hello" {
		t.Fatalf("form = %+v", req.Body.Form)
	}
	if len(req.Body.Files) != 2 {
		t.Fatalf("files = %+v", req.Body.Files)
	}
	if req.Body.Files[0].Path != `C:\temp\a.txt` || req.Body.Files[1].Path != "/tmp/b.bin" {
		t.Fatalf("file paths = %+v", req.Body.Files)
	}
	if len(req.Headers) != 0 {
		t.Fatalf("multipart should not keep a manual content-type: %+v", req.Headers)
	}
	if !hasWarning(got.Warnings, "附加选项") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestParseCurlBasicAuth(t *testing.T) {
	got := mustParseCurl(t, `curl -u 'alice:secret' https://x`)
	auth := got.Request.Auth
	if auth.Type != model.AuthBasic || auth.Basic.Username != "alice" || auth.Basic.Password != "secret" {
		t.Fatalf("auth = %+v", auth)
	}
}

func TestParseCurlOAuthBearer(t *testing.T) {
	got := mustParseCurl(t, `curl --oauth2-bearer 'tok-123' https://x`)
	if got.Request.Auth.Type != model.AuthBearer || got.Request.Auth.Bearer != "tok-123" {
		t.Fatalf("auth = %+v", got.Request.Auth)
	}
}

func TestParseCurlCookiesAndHeaders(t *testing.T) {
	got := mustParseCurl(t, `curl https://x -b 'sid=abc' -H 'X-Time: 12:30' -A 'NanoTest/1.0'`)
	req := got.Request
	if v, _ := headerValue(req.Headers, "Cookie"); v != "sid=abc" {
		t.Fatalf("cookie = %q", v)
	}
	if v, _ := headerValue(req.Headers, "X-Time"); v != "12:30" {
		t.Fatalf("x-time = %q", v)
	}
	if v, _ := headerValue(req.Headers, "User-Agent"); v != "NanoTest/1.0" {
		t.Fatalf("user-agent = %q", v)
	}
}

func TestParseCurlGetMode(t *testing.T) {
	got := mustParseCurl(t, `curl -G 'https://x/search' -d 'q=hello&page=1'`)
	req := got.Request
	if req.Method != "GET" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Body.Type != model.BodyNone {
		t.Fatalf("body = %+v", req.Body)
	}
	if req.URL != "https://x/search?q=hello&page=1" {
		t.Fatalf("url = %q", req.URL)
	}
}

func TestParseCurlUnsupportedMethodFallsBackToGet(t *testing.T) {
	got := mustParseCurl(t, `curl -X TRACE https://x`)
	if got.Request.Method != "GET" {
		t.Fatalf("method = %q", got.Request.Method)
	}
	if !hasWarning(got.Warnings, "TRACE") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestParseCurlDropsHopHeaders(t *testing.T) {
	got := mustParseCurl(t, `curl https://x -H 'Content-Length: 10'`)
	if _, ok := headerValue(got.Request.Headers, "Content-Length"); ok {
		t.Fatal("Content-Length should be dropped")
	}
	if !hasWarning(got.Warnings, "Content-Length") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestParseCurlErrors(t *testing.T) {
	if _, err := parseCurl(`curl`); err == nil {
		t.Fatal("expected an error for a missing URL")
	}
	if _, err := parseCurl(`curl https://x -H`); err == nil {
		t.Fatal("expected an error for a missing header value")
	}
	if _, err := parseCurl(`curl https://x -d`); err == nil {
		t.Fatal("expected an error for a missing data value")
	}
}

func TestParseCurlDataFileReadsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := mustParseCurl(t, "curl -d '@"+path+"' https://x")
	if got.Request.Method != "POST" {
		t.Fatalf("method = %q", got.Request.Method)
	}
	if got.Request.Body.Raw != `{"a":1}` {
		t.Fatalf("body = %q", got.Request.Body.Raw)
	}
}

func TestParseCurlDataFileMissingWarns(t *testing.T) {
	got := mustParseCurl(t, `curl -d @missing-body.json https://x`)
	if got.Request.Body.Type != model.BodyNone {
		t.Fatalf("body = %+v", got.Request.Body)
	}
	if !hasWarning(got.Warnings, "读取请求体文件失败") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestParseCurlDataRawKeepsAtSign(t *testing.T) {
	got := mustParseCurl(t, `curl --data-raw '@literal' https://x`)
	if got.Request.Body.Raw != "@literal" {
		t.Fatalf("body = %q", got.Request.Body.Raw)
	}
}

func TestParseCurlAttachedShortValues(t *testing.T) {
	got := mustParseCurl(t, `curl -XPOST https://x -HAccept:application/json -d'a=1'`)
	req := got.Request
	if req.Method != "POST" {
		t.Fatalf("method = %q", req.Method)
	}
	if req.Body.Raw != "a=1" {
		t.Fatalf("body = %q", req.Body.Raw)
	}
	if v, ok := headerValue(req.Headers, "Accept"); !ok || v != "application/json" {
		t.Fatalf("accept = %q ok=%v", v, ok)
	}
}

func TestParseCurlCurlExePrefix(t *testing.T) {
	got := mustParseCurl(t, "curl.exe https://x -H \"accept: application/json\"")
	if got.Request.Method != "GET" || got.Request.URL != "https://x" {
		t.Fatalf("request = %+v", got.Request)
	}
}

func TestBuildCurlGetWithHeadersAndBearer(t *testing.T) {
	req := model.Request{
		Method:  "GET",
		URL:     "https://api.example.com/users",
		Params:  []model.KV{{Key: "page", Value: "1", Enabled: true}},
		Headers: []model.KV{{Key: "Accept", Value: "application/json", Enabled: true}},
		Auth:    model.Auth{Type: model.AuthBearer, Bearer: "tok"},
		Body:    model.Body{Type: model.BodyNone},
	}
	cmd, warnings, err := buildCurlCommand(req, model.Settings{
		TimeoutMS: 15000, FollowRedirects: true,
	})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	for _, want := range []string{
		"curl -L --max-time 15",
		"'https://api.example.com/users?page=1'",
		"-H 'Accept: application/json'",
		"-H 'Authorization: Bearer tok'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q: %s", want, cmd)
		}
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestBuildCurlJSONPost(t *testing.T) {
	req := model.Request{
		Method: "POST",
		URL:    "https://x/users",
		Body:   model.Body{Type: model.BodyJSON, Raw: `{"a":1}`},
		Auth:   model.Auth{Type: model.AuthNone},
	}
	cmd, _, err := buildCurlCommand(req, model.Settings{TimeoutMS: 30000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	for _, want := range []string{
		"-X POST",
		"-H 'Content-Type: application/json'",
		`--data-raw '{"a":1}'`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q: %s", want, cmd)
		}
	}
}

func TestBuildCurlForm(t *testing.T) {
	req := model.Request{
		Method: "POST",
		URL:    "https://x/form",
		Body: model.Body{
			Type: model.BodyForm,
			Form: []model.KV{{Key: "name", Value: "张三", Enabled: true}},
		},
		Auth: model.Auth{Type: model.AuthNone},
	}
	cmd, _, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "-H 'Content-Type: application/x-www-form-urlencoded'") {
		t.Fatalf("missing form content type: %s", cmd)
	}
	if !strings.Contains(cmd, "--data-raw 'name=%E5%BC%A0%E4%B8%89'") {
		t.Fatalf("missing encoded form body: %s", cmd)
	}
}

func TestBuildCurlMultipart(t *testing.T) {
	req := model.Request{
		Method: "POST",
		URL:    "https://x/upload",
		Headers: []model.KV{
			{Key: "Content-Type", Value: "text/plain", Enabled: true},
		},
		Body: model.Body{
			Type:  model.BodyMultipart,
			Form:  []model.KV{{Key: "note", Value: "hello", Enabled: true}},
			Files: []model.FileField{{Key: "file", Path: `C:\tmp\a.txt`, Enabled: true}},
		},
		Auth: model.Auth{Type: model.AuthNone},
	}
	cmd, warnings, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "--form-string 'note=hello'") {
		t.Fatalf("missing form field: %s", cmd)
	}
	if !strings.Contains(cmd, `-F 'file=@C:\tmp\a.txt'`) {
		t.Fatalf("missing file field: %s", cmd)
	}
	if strings.Contains(cmd, "Content-Type") {
		t.Fatalf("multipart command should not set Content-Type: %s", cmd)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestBuildCurlBasicAuthAndShellQuote(t *testing.T) {
	req := model.Request{
		Method: "GET",
		URL:    "https://x",
		Headers: []model.KV{
			{Key: "Authorization", Value: "should be replaced", Enabled: true},
			{Key: "X-Note", Value: "it's fine", Enabled: true},
		},
		Auth: model.Auth{Type: model.AuthBasic, Basic: model.BasicAuth{Username: "u", Password: "p"}},
		Body: model.Body{Type: model.BodyNone},
	}
	cmd, _, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "-u 'u:p'") {
		t.Fatalf("missing basic auth: %s", cmd)
	}
	if strings.Contains(cmd, "should be replaced") {
		t.Fatalf("authorization header should be replaced: %s", cmd)
	}
	if !strings.Contains(cmd, `-H 'X-Note: it'\''s fine'`) {
		t.Fatalf("shell quote missing: %s", cmd)
	}
}

func TestBuildCurlAPIKeyHeaderAndQuery(t *testing.T) {
	headerReq := model.Request{
		Method:  "GET",
		URL:     "https://x",
		Headers: []model.KV{{Key: "X-Key", Value: "old", Enabled: true}},
		Auth: model.Auth{
			Type:   model.AuthAPIKey,
			APIKey: model.APIKeyAuth{Key: "X-Key", Value: "new", AddTo: model.AddToHeader},
		},
		Body: model.Body{Type: model.BodyNone},
	}
	cmd, _, err := buildCurlCommand(headerReq, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "-H 'X-Key: new'") || strings.Contains(cmd, "X-Key: old") {
		t.Fatalf("api key header wrong: %s", cmd)
	}

	queryReq := model.Request{
		Method: "GET",
		URL:    "https://x/users",
		Auth: model.Auth{
			Type:   model.AuthAPIKey,
			APIKey: model.APIKeyAuth{Key: "api_key", Value: "v", AddTo: model.AddToQuery},
		},
		Body: model.Body{Type: model.BodyNone},
	}
	cmd, _, err = buildCurlCommand(queryReq, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "https://x/users?api_key=v") {
		t.Fatalf("api key query missing: %s", cmd)
	}
}

func TestBuildCurlGetWithBodyAddsExplicitMethod(t *testing.T) {
	req := model.Request{
		Method: "GET",
		URL:    "https://x",
		Body:   model.Body{Type: model.BodyJSON, Raw: `{"a":1}`},
		Auth:   model.Auth{Type: model.AuthNone},
	}
	cmd, _, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "-X GET") {
		t.Fatalf("GET with body should be explicit: %s", cmd)
	}
}

func TestBuildCurlHeadUsesI(t *testing.T) {
	req := model.Request{
		Method: "HEAD",
		URL:    "https://x",
		Auth:   model.Auth{Type: model.AuthNone},
		Body:   model.Body{Type: model.BodyNone},
	}
	cmd, _, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, " -I ") || strings.Contains(cmd, "-X HEAD") {
		t.Fatalf("HEAD command = %s", cmd)
	}
}

func TestBuildCurlEmptyURL(t *testing.T) {
	_, _, err := buildCurlCommand(model.Request{Method: "GET"}, model.Settings{})
	if err == nil {
		t.Fatal("expected an error for an empty URL")
	}
}

func TestBuildCurlMultipartWithoutFieldsWarns(t *testing.T) {
	req := model.Request{
		Method: "POST",
		URL:    "https://x/upload",
		Body:   model.Body{Type: model.BodyMultipart},
		Auth:   model.Auth{Type: model.AuthNone},
	}
	_, warnings, err := buildCurlCommand(req, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !hasWarning(warnings, "没有有效字段或文件") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestParseCurlRoundTripMultipartFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upload.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed := mustParseCurl(t, "curl -X POST 'https://x/upload' -F 'file=@"+path+"'")
	if len(parsed.Request.Body.Files) != 1 || parsed.Request.Body.Files[0].Path != path {
		t.Fatalf("parsed files = %+v", parsed.Request.Body.Files)
	}
	cmd, _, err := buildCurlCommand(parsed.Request, model.Settings{TimeoutMS: 1000})
	if err != nil {
		t.Fatalf("buildCurlCommand: %v", err)
	}
	if !strings.Contains(cmd, "-F 'file=@"+path+"'") {
		t.Fatalf("round-trip command = %s", cmd)
	}
}
