package interp

import "testing"

func TestResolveSubstitutesVariables(t *testing.T) {
	vars := Vars{"baseUrl": "https://api.example.com", "version": "v2"}
	got, unresolved := Resolve("{{baseUrl}}/{{version}}/users", vars)
	if got != "https://api.example.com/v2/users" {
		t.Fatalf("got %q, want %q", got, "https://api.example.com/v2/users")
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want empty", unresolved)
	}
}

func TestResolveKeepsUnknownVariables(t *testing.T) {
	got, unresolved := Resolve("{{baseUrl}}/users", Vars{})
	if got != "{{baseUrl}}/users" {
		t.Fatalf("got %q, want the original text", got)
	}
	if len(unresolved) != 1 || unresolved[0] != "baseUrl" {
		t.Fatalf("unresolved = %v, want [baseUrl]", unresolved)
	}
}

func TestResolveFollowsNestedVariables(t *testing.T) {
	vars := Vars{"a": "{{b}}", "b": "{{c}}", "c": "final"}
	got, _ := Resolve("{{a}}", vars)
	if got != "final" {
		t.Fatalf("got %q, want %q", got, "final")
	}
}

func TestResolveStopsOnCycle(t *testing.T) {
	vars := Vars{"a": "{{b}}", "b": "{{a}}"}
	got, _ := Resolve("{{a}}", vars)
	// 循环引用必须在有限步内收敛，不能无限展开。
	if got == "" {
		t.Fatal("expected a non-empty result")
	}
	if got != "{{a}}" && got != "{{b}}" {
		t.Fatalf("unexpected cycle result %q", got)
	}
}

func TestResolveHandlesEmptyInput(t *testing.T) {
	got, unresolved := Resolve("", Vars{})
	if got != "" || unresolved != nil {
		t.Fatalf("got %q / %v, want empty", got, unresolved)
	}
}

func TestResolveTrimsSpacesInsideBraces(t *testing.T) {
	got, _ := Resolve("{{ name }}", Vars{"name": "x"})
	if got != "x" {
		t.Fatalf("got %q, want %q", got, "x")
	}
}

func TestUnresolvedIsDedupedAndSorted(t *testing.T) {
	got := Unresolved("{{b}} {{a}} {{b}}")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestNamesListsAllReferences(t *testing.T) {
	got := Names("{{a}}/{{b}}?x={{a}}")
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 entries", got)
	}
}
