package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name                         string
		current, lastApplied, wanted string
		want                         Decision
	}{
		{"fresh file", "", "", "w", Write},
		{"already there", "w", "", "w", Unchanged},
		{"unchanged target, new layer", "a", "a", "b", Write},
		{"local edit, layer same", "x", "a", "a", LocalOnly},
		{"local edit, layer new", "x", "a", "b", Conflict},
		{"never applied, differs", "x", "", "b", Conflict},
		{"local delete, layer same", "", "a", "a", LocalOnly},
		{"local delete, layer new", "", "a", "b", Conflict},
	}
	for _, c := range cases {
		if got := Decide(c.current, c.lastApplied, c.wanted); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestAppliedRoundTrip(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	a, _ := d.Load()
	a.Hashes["/x"] = "h"
	a.Projects["/p"] = "acme"
	if err := d.Save(a); err != nil {
		t.Fatal(err)
	}
	b, err := d.Load()
	if err != nil || b.Hashes["/x"] != "h" || b.Projects["/p"] != "acme" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestLock(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	release, err := d.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Lock(); err != ErrLocked {
		t.Errorf("second lock should fail, got %v", err)
	}
	release()
	if _, err := d.Lock(); err != nil {
		t.Errorf("lock after release failed: %v", err)
	}
	// stale lock from a dead pid is taken over
	os.WriteFile(filepath.Join(d.Path, "lock"), []byte("999999999\n"), 0o644)
	rel, err := d.Lock()
	if err != nil {
		t.Errorf("stale lock not taken over: %v", err)
	}
	rel()
	// an empty lock file is another process mid-lock, not stale
	os.WriteFile(filepath.Join(d.Path, "lock"), nil, 0o644)
	if _, err := d.Lock(); err != ErrLocked {
		t.Errorf("empty lock must be treated as held, got %v", err)
	}
	os.Remove(filepath.Join(d.Path, "lock"))
}

func TestBackupAndPrune(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	target := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(target, []byte("v1"), 0o644)
	b := d.NewBackup()
	if err := b.Save(target); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(filepath.Join(t.TempDir(), "missing")); err != nil || b.Count() != 1 {
		t.Errorf("missing file must be skipped silently: count=%d err=%v", b.Count(), err)
	}
	root := filepath.Join(d.Path, "backups")
	for _, r := range []string{"20260101-000000-1", "20260102-000000-1", "20260103-000000-1", "20260104-000000-1"} {
		os.MkdirAll(filepath.Join(root, r), 0o755)
	}
	if err := d.Prune(2); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["20260101-000000-1"] {
		t.Error("oldest backup must be kept")
	}
	if names["20260102-000000-1"] || names["20260103-000000-1"] {
		t.Error("middle runs should be pruned")
	}
	if len(entries) != 3 { // oldest + 2 newest (20260104 and this run)
		t.Errorf("expected 3 dirs, got %d", len(entries))
	}
}
