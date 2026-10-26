package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nanoapi/internal/model"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nanoapi.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestOpenCreatesDefaultData(t *testing.T) {
	s, path := tempStore(t)
	d := s.Snapshot()
	if d.Settings.TimeoutMS != model.DefaultSettings().TimeoutMS {
		t.Fatalf("timeout = %d, want default", d.Settings.TimeoutMS)
	}
	if d.Collections == nil || d.Environments == nil || d.History == nil || d.Tabs == nil {
		t.Fatal("expected non-nil slices")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected no file before the first flush")
	}
}

func TestRoundTrip(t *testing.T) {
	s, path := tempStore(t)

	s.SetCollections([]*model.CollectionItem{
		{ID: "c1", Type: model.ItemFolder, Name: "我的接口", Items: []*model.CollectionItem{
			{ID: "r1", Type: model.ItemRequest, Name: "列表", Request: &model.Request{
				ID: "r1", Name: "列表", Method: "GET", URL: "https://api.example.com/users",
				Params: []model.KV{{Key: "page", Value: "1", Enabled: true}},
			}},
		}},
	})
	s.SetEnvironments([]*model.Environment{
		{ID: "e1", Name: "开发", Values: []model.KV{{Key: "baseUrl", Value: "https://dev.example.com", Enabled: true}}},
	})
	s.SetSettings(model.Settings{
		TimeoutMS: 12345, FollowRedirects: false, InsecureTLS: true,
		Theme: "light", ActiveEnvID: "e1",
	})
	s.SetTabs([]*model.OpenTab{
		{
			Key: "tab-1",
			Request: model.Request{
				ID: "r1", Name: "列表", Method: "GET", URL: "https://api.example.com/users",
			},
			Dirty: true,
		},
	}, "tab-1")

	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	d := reopened.Snapshot()

	if len(d.Collections) != 1 || d.Collections[0].Name != "我的接口" {
		t.Fatalf("collections = %+v", d.Collections)
	}
	if len(d.Collections[0].Items) != 1 {
		t.Fatalf("children = %+v", d.Collections[0].Items)
	}
	req := d.Collections[0].Items[0].Request
	if req == nil || req.URL != "https://api.example.com/users" {
		t.Fatalf("request = %+v", req)
	}
	if len(req.Params) != 1 || req.Params[0].Key != "page" || !req.Params[0].Enabled {
		t.Fatalf("params = %+v", req.Params)
	}
	if len(d.Environments) != 1 || d.Environments[0].Values[0].Value != "https://dev.example.com" {
		t.Fatalf("environments = %+v", d.Environments)
	}
	if d.Settings.TimeoutMS != 12345 || d.Settings.Theme != "light" || d.Settings.InsecureTLS != true {
		t.Fatalf("settings = %+v", d.Settings)
	}
	if d.Settings.ActiveEnvID != "e1" {
		t.Fatalf("activeEnvId = %q, want e1", d.Settings.ActiveEnvID)
	}
	if len(d.Tabs) != 1 || d.Tabs[0].Key != "tab-1" || !d.Tabs[0].Dirty {
		t.Fatalf("tabs = %+v", d.Tabs)
	}
	if d.Tabs[0].Request.URL != "https://api.example.com/users" {
		t.Fatalf("tab request = %+v", d.Tabs[0].Request)
	}
	if d.ActiveTabKey != "tab-1" {
		t.Fatalf("activeTabKey = %q, want tab-1", d.ActiveTabKey)
	}
}

func TestLoadKeepsThemeWhenTimeoutIsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanoapi.json")
	raw := `{"settings":{"timeoutMs":0,"theme":"light"}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d := s.Snapshot()
	if d.Settings.TimeoutMS != model.DefaultSettings().TimeoutMS {
		t.Fatalf("timeout = %d, want default", d.Settings.TimeoutMS)
	}
	if d.Settings.Theme != "light" {
		t.Fatalf("theme = %q, want light", d.Settings.Theme)
	}
}

func TestCorruptedFileIsBackedUpAndRecovered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nanoapi.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open should recover from a corrupted file: %v", err)
	}
	if len(s.Snapshot().Collections) != 0 {
		t.Fatal("expected empty collections after recovery")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, e := range entries {
		if len(e.Name()) > 7 && e.Name()[:7] == "nanoapi" && filepath.Ext(e.Name()) == ".json" && e.Name() != "nanoapi.json" {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("expected 1 backup file, got %d", backups)
	}
}

func TestHistoryIsCappedAtMax(t *testing.T) {
	s, _ := tempStore(t)
	for i := 0; i < maxHistory+50; i++ {
		s.AddHistory(&model.HistoryEntry{ID: model.NewID(), Status: 200})
	}
	if got := len(s.Snapshot().History); got != maxHistory {
		t.Fatalf("history length = %d, want %d", got, maxHistory)
	}
}

func TestHistoryKeepsNewestFirst(t *testing.T) {
	s, _ := tempStore(t)
	s.AddHistory(&model.HistoryEntry{ID: "first"})
	s.AddHistory(&model.HistoryEntry{ID: "second"})
	h := s.Snapshot().History
	if len(h) != 2 || h[0].ID != "second" {
		t.Fatalf("history order wrong: %+v", h)
	}
}

func TestClearHistory(t *testing.T) {
	s, _ := tempStore(t)
	s.AddHistory(&model.HistoryEntry{ID: "x"})
	s.ClearHistory()
	if got := len(s.Snapshot().History); got != 0 {
		t.Fatalf("history length = %d, want 0", got)
	}
}

func TestFlushAlwaysLeavesValidJSON(t *testing.T) {
	s, path := tempStore(t)
	for i := 0; i < 50; i++ {
		s.SetCollections([]*model.CollectionItem{{ID: model.NewID(), Type: model.ItemFolder, Name: "集合"}})
		if err := s.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var probe map[string]any
		if err := json.Unmarshal(b, &probe); err != nil {
			t.Fatalf("file is not valid JSON after flush %d: %v", i, err)
		}
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	s, path := tempStore(t)
	s.SetSettings(model.DefaultSettings())
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
