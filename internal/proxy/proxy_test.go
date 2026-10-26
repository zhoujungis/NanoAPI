package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nanoapi/internal/model"
)

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetReturnsJSONBody(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "yes")
		fmt.Fprint(w, `{"code":0,"name":"张三"}`)
	})

	res := Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/users"})

	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Status != 200 {
		t.Fatalf("status = %d, want 200", res.Status)
	}
	if res.Body != `{"code":0,"name":"张三"}` {
		t.Fatalf("body = %q", res.Body)
	}
	if res.IsBinary {
		t.Fatal("JSON body should not be treated as binary")
	}
	if res.Headers["X-Custom"][0] != "yes" {
		t.Fatalf("custom header missing: %v", res.Headers)
	}
	if res.DurationMS < 0 {
		t.Fatalf("duration = %d", res.DurationMS)
	}
}

func TestPostJSONBody(t *testing.T) {
	var gotBody, gotType, gotMethod string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = string(buf)
		gotType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		w.WriteHeader(201)
	})

	res := Send(context.Background(), Options{
		Method:      "POST",
		URL:         srv.URL + "/users",
		Body:        []byte(`{"name":"李四"}`),
		ContentType: "application/json",
	})

	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if res.Status != 201 {
		t.Fatalf("status = %d, want 201", res.Status)
	}
	if gotMethod != "POST" || gotBody != `{"name":"李四"}` || gotType != "application/json" {
		t.Fatalf("method=%q body=%q type=%q", gotMethod, gotBody, gotType)
	}
}

func TestCustomHeaderIsSent(t *testing.T) {
	var got string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Token")
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method:  "GET",
		URL:     srv.URL,
		Headers: []model.KV{{Key: "X-Token", Value: "abc123", Enabled: true}},
	})
	if got != "abc123" {
		t.Fatalf("X-Token = %q, want abc123", got)
	}
}

func TestDisabledHeaderIsNotSent(t *testing.T) {
	var present string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		present = r.Header.Get("X-Skip")
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method:  "GET",
		URL:     srv.URL,
		Headers: []model.KV{{Key: "X-Skip", Value: "v", Enabled: false}},
	})
	if present != "" {
		t.Fatalf("disabled header was sent: %q", present)
	}
}

func TestQueryParamsMergeAndOverride(t *testing.T) {
	var rawQuery string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL + "/users?page=1&size=20",
		Params: []model.KV{
			{Key: "size", Value: "50", Enabled: true},
			{Key: "name", Value: "Bob Smith", Enabled: true},
		},
	})

	if !strings.Contains(rawQuery, "page=1") {
		t.Fatalf("original param lost: %q", rawQuery)
	}
	if !strings.Contains(rawQuery, "size=50") {
		t.Fatalf("param not overridden: %q", rawQuery)
	}
	if !strings.Contains(rawQuery, "name=Bob+Smith") {
		t.Fatalf("new param missing or not encoded: %q", rawQuery)
	}
}

func TestAuthBasicIsSent(t *testing.T) {
	var user, pass string
	var ok bool
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok = r.BasicAuth()
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL,
		Auth:   model.Auth{Type: model.AuthBasic, Basic: model.BasicAuth{Username: "u", Password: "p"}},
	})
	if !ok || user != "u" || pass != "p" {
		t.Fatalf("basic auth = %q/%q ok=%v", user, pass, ok)
	}
}

func TestAuthBearerIsSent(t *testing.T) {
	var auth string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL,
		Auth:   model.Auth{Type: model.AuthBearer, Bearer: "tok123"},
	})
	if auth != "Bearer tok123" {
		t.Fatalf("Authorization = %q", auth)
	}
}

func TestAuthAPIKeyInHeaderAndQuery(t *testing.T) {
	var header, query string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Api-Key")
		query = r.URL.Query().Get("api_key")
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL,
		Auth: model.Auth{Type: model.AuthAPIKey, APIKey: model.APIKeyAuth{
			Key: "X-Api-Key", Value: "k1", AddTo: model.AddToHeader,
		}},
	})
	if header != "k1" {
		t.Fatalf("header api key = %q", header)
	}

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL,
		Auth: model.Auth{Type: model.AuthAPIKey, APIKey: model.APIKeyAuth{
			Key: "api_key", Value: "k2", AddTo: model.AddToQuery,
		}},
	})
	if query != "k2" {
		t.Fatalf("query api key = %q", query)
	}
}

func TestRedirectFollowedWhenEnabled(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		fmt.Fprint(w, "arrived")
	})

	res := Send(context.Background(), Options{
		Method: "GET", URL: srv.URL + "/old", FollowRedirects: true,
	})
	if res.Status != 200 {
		t.Fatalf("status = %d, want 200", res.Status)
	}
	if res.Body != "arrived" {
		t.Fatalf("body = %q", res.Body)
	}
	if len(res.Redirects) != 1 || res.Redirects[0].Status != 302 {
		t.Fatalf("redirects = %+v", res.Redirects)
	}
	if res.Redirects[0].URL != srv.URL+"/new" {
		t.Fatalf("redirect target = %q, want %q", res.Redirects[0].URL, srv.URL+"/new")
	}
	if res.URL != srv.URL+"/new" {
		t.Fatalf("final url = %q, want %q", res.URL, srv.URL+"/new")
	}
}

func TestTooManyRedirectsKeepsSpecificError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	})

	res := Send(context.Background(), Options{
		Method: "GET", URL: srv.URL, FollowRedirects: true,
	})
	if res.ErrorKind != ErrTooManyRedirects {
		t.Fatalf("errorKind = %q, want %q (error: %s)", res.ErrorKind, ErrTooManyRedirects, res.Error)
	}
	if !strings.Contains(res.Error, "重定向超过") {
		t.Fatalf("error message = %q, want a redirect hint", res.Error)
	}
}

func TestClientKeepsCookiesAcrossRequests(t *testing.T) {
	var got string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/set":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
			fmt.Fprint(w, "set")
		case "/check":
			if c, err := r.Cookie("sid"); err == nil {
				got = c.Value
			}
			fmt.Fprint(w, "check")
		default:
			http.NotFound(w, r)
		}
	})

	client := NewClient()
	if res := client.Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/set"}); res.Error != "" {
		t.Fatalf("set cookie: %s", res.Error)
	}
	if res := client.Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/check"}); res.Error != "" {
		t.Fatalf("check cookie: %s", res.Error)
	}
	if got != "abc123" {
		t.Fatalf("cookie = %q, want abc123", got)
	}
}

func TestClientClearCookies(t *testing.T) {
	var got string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
			fmt.Fprint(w, "set")
			return
		}
		if c, err := r.Cookie("sid"); err == nil {
			got = c.Value
		}
		fmt.Fprint(w, "check")
	})

	client := NewClient()
	client.Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/set"})
	client.ClearCookies()
	client.Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/check"})
	if got != "" {
		t.Fatalf("cookie survived ClearCookies: %q", got)
	}
}

func TestQueryFlagWithoutValueIsPreserved(t *testing.T) {
	var rawQuery string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		fmt.Fprint(w, "ok")
	})

	Send(context.Background(), Options{
		Method: "GET",
		URL:    srv.URL + "/users?debug&page=1",
		Params: []model.KV{{Key: "size", Value: "20", Enabled: true}},
	})

	if !strings.Contains(rawQuery, "debug") || strings.Contains(rawQuery, "debug=") {
		t.Fatalf("value-less query flag was rewritten: %q", rawQuery)
	}
	if !strings.Contains(rawQuery, "size=20") {
		t.Fatalf("param missing: %q", rawQuery)
	}
}

func TestRedirectNotFollowedWhenDisabled(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusFound)
	})

	res := Send(context.Background(), Options{
		Method: "GET", URL: srv.URL + "/old", FollowRedirects: false,
	})
	if res.Status != 302 {
		t.Fatalf("status = %d, want 302", res.Status)
	}
	if res.Headers["Location"] == nil {
		t.Fatal("Location header missing")
	}
}

func TestTimeoutIsReported(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		fmt.Fprint(w, "too late")
	})

	res := Send(context.Background(), Options{
		Method: "GET", URL: srv.URL + "/slow", TimeoutMS: 200,
	})
	if res.Error == "" {
		t.Fatal("expected a timeout error")
	}
	if res.ErrorKind != ErrTimeout {
		t.Fatalf("errorKind = %q, want %q", res.ErrorKind, ErrTimeout)
	}
}

func TestBinaryResponseIsBase64(t *testing.T) {
	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x01, 0x02}
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(payload)
	})

	res := Send(context.Background(), Options{Method: "GET", URL: srv.URL + "/img"})
	if !res.IsBinary {
		t.Fatal("expected binary detection")
	}
	if !res.BodyBase64 {
		t.Fatal("expected bodyBase64 flag")
	}
	if res.Body != "iVBORwABAg==" {
		t.Fatalf("body = %q", res.Body)
	}
}

func TestFormURLEncodedIsNotBinary(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		fmt.Fprint(w, "a=1&b=2")
	})

	res := Send(context.Background(), Options{Method: "GET", URL: srv.URL})
	if res.IsBinary {
		t.Fatal("form response must not be treated as binary")
	}
}

func TestLargeBodyIsTruncated(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(strings.Repeat("x", 5000)))
	})

	res := Send(context.Background(), Options{
		Method: "GET", URL: srv.URL, MaxBodyBytes: 1000,
	})
	if !res.Truncated {
		t.Fatal("expected truncation flag")
	}
	if len(res.Body) != 1000 {
		t.Fatalf("body length = %d, want 1000", len(res.Body))
	}
}

func TestUnresolvableHostReportsDNSError(t *testing.T) {
	res := Send(context.Background(), Options{
		Method: "GET",
		URL:    "http://nanoapi-nonexistent-host.invalid/",
	})
	if res.Error == "" {
		t.Fatal("expected an error")
	}
	if res.ErrorKind != ErrDNS {
		t.Fatalf("errorKind = %q, want %q (error: %s)", res.ErrorKind, ErrDNS, res.Error)
	}
}

func TestConnectionRefusedIsReported(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // 立刻释放端口，后续连接会被拒绝

	res := Send(context.Background(), Options{Method: "GET", URL: "http://" + addr})
	if res.Error == "" {
		t.Fatal("expected an error")
	}
	if res.ErrorKind != ErrRefused {
		t.Fatalf("errorKind = %q, want %q (error: %s)", res.ErrorKind, ErrRefused, res.Error)
	}
}

func TestMissingSchemeGetsHTTPDefault(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})
	res := Send(context.Background(), Options{Method: "GET", URL: strings.TrimPrefix(srv.URL, "http://")})
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
}

func TestBuildURLMergesParamsAndAPIKey(t *testing.T) {
	got, err := BuildURL(
		"example.com/users?page=1&flag",
		[]model.KV{
			{Key: "page", Value: "2", Enabled: true},
			{Key: "tag", Value: "a b", Enabled: true},
		},
		model.Auth{
			Type:   model.AuthAPIKey,
			APIKey: model.APIKeyAuth{Key: "api_key", Value: "k", AddTo: model.AddToQuery},
		},
	)
	if err != nil {
		t.Fatalf("BuildURL: %v", err)
	}
	want := "http://example.com/users?page=2&flag&tag=a+b&api_key=k"
	if got != want {
		t.Fatalf("BuildURL = %q, want %q", got, want)
	}
}

func TestBuildURLRejectsBadInput(t *testing.T) {
	if _, err := BuildURL("  ", nil, model.Auth{}); err == nil {
		t.Fatal("expected an error for an empty URL")
	}
	if _, err := BuildURL("http:///path", nil, model.Auth{}); err == nil {
		t.Fatal("expected an error for a URL without a host")
	}
}

func TestEmptyURLIsRejected(t *testing.T) {
	res := Send(context.Background(), Options{Method: "GET", URL: "  "})
	if res.ErrorKind != ErrBadURL {
		t.Fatalf("errorKind = %q, want %q", res.ErrorKind, ErrBadURL)
	}
}

func TestServerErrorStatusIsNotAnError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"boom"}`)
	})

	res := Send(context.Background(), Options{Method: "GET", URL: srv.URL})
	if res.Error != "" {
		t.Fatalf("500 must not be reported as a transport error: %s", res.Error)
	}
	if res.Status != 500 {
		t.Fatalf("status = %d, want 500", res.Status)
	}
}

func TestLooksBinaryDetectsContentTypes(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"application/pdf", true},
		{"application/zip", true},
		{"application/json", false},
		{"text/html; charset=utf-8", false},
		{"application/x-www-form-urlencoded", false},
		{"text/csv", false},
	}
	for _, c := range cases {
		if got := looksBinary(c.ct, []byte("plain")); got != c.want {
			t.Errorf("looksBinary(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}
