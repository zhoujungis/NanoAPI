package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleEchoMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("note", "hello"); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("file", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("file-body")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/echo?x=1", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handleEcho(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Method string              `json:"method"`
		Fields map[string][]string `json:"fields"`
		Files  []struct {
			Field       string `json:"field"`
			Filename    string `json:"filename"`
			Size        int64  `json:"size"`
			ContentType string `json:"contentType"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Method != http.MethodPost {
		t.Fatalf("method = %q", resp.Method)
	}
	if got := resp.Fields["note"]; len(got) != 1 || got[0] != "hello" {
		t.Fatalf("fields = %+v", resp.Fields)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("files = %+v", resp.Files)
	}
	f := resp.Files[0]
	if f.Field != "file" || f.Filename != "a.txt" || f.Size != int64(len("file-body")) {
		t.Fatalf("file = %+v", f)
	}
}

func TestHandleEchoRawBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleEcho(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Body != `{"a":1}` {
		t.Fatalf("body = %q", resp.Body)
	}
}
