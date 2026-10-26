package model

import (
	"encoding/json"
	"testing"
)

func sampleTree() []*CollectionItem {
	return []*CollectionItem{
		{
			ID: "c1", Type: ItemFolder, Name: "我的接口",
			Items: []*CollectionItem{
				{ID: "f1", Type: ItemFolder, Name: "用户", Items: []*CollectionItem{
					{ID: "r1", Type: ItemRequest, Name: "列表", Request: &Request{ID: "r1", Method: "GET", URL: "https://x/users"}},
				}},
			},
		},
		{ID: "c2", Type: ItemFolder, Name: "其他"},
	}
}

func TestJSONRoundTrip(t *testing.T) {
	req := NewRequest()
	req.Name = "创建用户"
	req.Method = "POST"
	req.URL = "https://api.example.com/users"
	req.Params = []KV{{Key: "dry_run", Value: "true", Enabled: true}}
	req.Headers = []KV{{Key: "Content-Type", Value: "application/json", Enabled: true}}
	req.Body = Body{Type: BodyJSON, Raw: `{"name":"张三"}`}
	req.Auth = Auth{Type: AuthBearer, Bearer: "tok"}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var back Request
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != req.Name || back.Method != req.Method || back.URL != req.URL {
		t.Fatalf("round trip mismatch: %+v", back)
	}
	if back.Body.Raw != req.Body.Raw || back.Body.Type != req.Body.Type {
		t.Fatalf("body mismatch: %+v", back.Body)
	}
	if back.Auth.Bearer != "tok" || back.Auth.Type != AuthBearer {
		t.Fatalf("auth mismatch: %+v", back.Auth)
	}
	if len(back.Params) != 1 || !back.Params[0].Enabled {
		t.Fatalf("params mismatch: %+v", back.Params)
	}
}

func TestMultipartBodyJSONRoundTrip(t *testing.T) {
	req := NewRequest()
	req.Body = Body{
		Type:  BodyMultipart,
		Form:  []KV{{Key: "name", Value: "张三", Enabled: true}},
		Files: []FileField{{Key: "file", Path: `C:\tmp\a.txt`, Enabled: true}},
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var back Request
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Body.Type != BodyMultipart {
		t.Fatalf("body type = %q, want %q", back.Body.Type, BodyMultipart)
	}
	if len(back.Body.Files) != 1 {
		t.Fatalf("files = %+v", back.Body.Files)
	}
	got := back.Body.Files[0]
	if got.Key != "file" || got.Path != `C:\tmp\a.txt` || !got.Enabled {
		t.Fatalf("file field = %+v", got)
	}
	if len(back.Body.Form) != 1 || back.Body.Form[0].Value != "张三" {
		t.Fatalf("form = %+v", back.Body.Form)
	}
}

func TestFindItem(t *testing.T) {
	tree := sampleTree()
	parent, found := FindItem(tree, "r1")
	if found == nil || found.ID != "r1" {
		t.Fatalf("found = %+v", found)
	}
	if parent == nil || parent.ID != "f1" {
		t.Fatalf("parent = %+v", parent)
	}

	_, root := FindItem(tree, "c1")
	if root == nil {
		t.Fatal("root item not found")
	}
	if p, _ := FindItem(tree, "c1"); p != nil {
		t.Fatal("root item should have no parent")
	}
	if _, missing := FindItem(tree, "nope"); missing != nil {
		t.Fatal("expected nil for unknown id")
	}
}

func TestRemoveItem(t *testing.T) {
	tree := sampleTree()
	if !RemoveItem(&tree, "r1") {
		t.Fatal("expected removal to succeed")
	}
	if _, found := FindItem(tree, "r1"); found != nil {
		t.Fatal("item still present after removal")
	}
	if RemoveItem(&tree, "r1") {
		t.Fatal("second removal should fail")
	}
}

func TestRemoveRootItemShortensSlice(t *testing.T) {
	tree := sampleTree()
	if !RemoveItem(&tree, "c2") {
		t.Fatal("expected removal to succeed")
	}
	if len(tree) != 1 {
		t.Fatalf("length = %d, want 1", len(tree))
	}
	if tree[0].ID != "c1" {
		t.Fatalf("wrong item kept: %+v", tree[0])
	}
}

func TestCountRequests(t *testing.T) {
	if got := CountRequests(sampleTree()); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if seen[id] {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}

func TestDefaultSettings(t *testing.T) {
	s := DefaultSettings()
	if s.TimeoutMS != 30000 || !s.FollowRedirects || s.InsecureTLS || s.Theme != "dark" {
		t.Fatalf("unexpected defaults: %+v", s)
	}
}
