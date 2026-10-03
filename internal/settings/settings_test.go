package settings

import (
	"reflect"
	"testing"
)

func mustParse(t *testing.T, s string) Doc {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMergeReplacesOnlyOwnedPaths(t *testing.T) {
	device := mustParse(t, `{
	  "permissions": {"defaultMode": "auto", "allow": ["Bash(ls)"], "deny": ["Read(.env)"]},
	  "env": {"X": "1"},
	  "theme": "light"
	}`)
	layer := mustParse(t, `{
	  "permissions": {"deny": ["Read(.env)", "Read(id_rsa)"]},
	  "theme": "dark",
	  "attribution": {"commit": ""}
	}`)
	changed := Merge(device, layer, []string{"permissions.deny", "theme", "attribution"})
	if want := []string{"attribution", "permissions.deny", "theme"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	if v, _ := device.Get("permissions.defaultMode"); v != "auto" {
		t.Errorf("defaultMode must survive, got %v", v)
	}
	if v, _ := device.Get("permissions.allow"); len(v.([]any)) != 1 {
		t.Errorf("allow must survive, got %v", v)
	}
	if v, _ := device.Get("env.X"); v != "1" {
		t.Errorf("env must survive, got %v", v)
	}
	if v, _ := device.Get("permissions.deny"); len(v.([]any)) != 2 {
		t.Errorf("deny not replaced: %v", v)
	}
}

func TestMergeRemovesOwnedPathMissingInSource(t *testing.T) {
	device := mustParse(t, `{"theme": "dark", "statusLine": {"type": "command"}}`)
	layer := mustParse(t, `{"theme": "dark"}`)
	changed := Merge(device, layer, []string{"theme", "statusLine"})
	if !reflect.DeepEqual(changed, []string{"statusLine"}) {
		t.Fatalf("changed = %v", changed)
	}
	if _, ok := device.Get("statusLine"); ok {
		t.Error("statusLine should be removed")
	}
}

func TestMergeNoChangeReportsNothing(t *testing.T) {
	a := mustParse(t, `{"theme": "dark"}`)
	b := mustParse(t, `{"theme": "dark"}`)
	if changed := Merge(a, b, []string{"theme", "missing"}); len(changed) != 0 {
		t.Errorf("unexpected changes %v", changed)
	}
}

func TestDeletePrunesEmptyObjects(t *testing.T) {
	d := mustParse(t, `{"permissions": {"deny": []}, "theme": "x"}`)
	d.Delete("permissions.deny")
	if _, ok := d["permissions"]; ok {
		t.Error("empty permissions object should be pruned")
	}
}

func TestExtractAndDiff(t *testing.T) {
	d := mustParse(t, `{"a": {"b": 1, "c": 2}, "d": 3}`)
	e := Extract(d, []string{"a.b", "d"})
	if _, ok := e.Get("a.c"); ok {
		t.Error("a.c must not be extracted")
	}
	if diff := Diff(d, e, []string{"a.b", "a.c", "d"}); !reflect.DeepEqual(diff, []string{"a.c"}) {
		t.Errorf("diff = %v", diff)
	}
}

func TestSetRejectsNonObjectIntermediate(t *testing.T) {
	d := mustParse(t, `{"theme": "dark"}`)
	if err := d.Set("theme.sub", 1); err == nil {
		t.Error("expected error")
	}
}
