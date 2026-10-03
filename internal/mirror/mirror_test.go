package mirror

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/state"
)

func w(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanApplyCapture(t *testing.T) {
	layer := t.TempDir()
	target := t.TempDir()
	st := state.Dir{Path: t.TempDir()}
	applied, _ := st.Load()

	w(t, filepath.Join(layer, "CLAUDE.md"), "v1")
	w(t, filepath.Join(layer, "rules/a.md"), "a1")
	w(t, filepath.Join(target, "skills/.trash/old.md"), "junk")
	w(t, filepath.Join(target, "skills/mine/SKILL.md"), "local skill")
	ignore := []string{"skills/.trash/"}

	// first apply: everything is written, ignored and device-only files untouched
	actions, err := Plan(layer, target, ignore, applied.Hashes)
	if err != nil {
		t.Fatal(err)
	}
	written, skipped, err := Apply(actions, st.NewBackup(), applied)
	if err != nil || len(written) != 2 {
		t.Fatalf("written=%d skipped=%d err=%v", len(written), len(skipped), err)
	}
	if len(skipped) != 1 || skipped[0].Rel != "skills/mine/SKILL.md" || skipped[0].Decision != state.LocalOnly {
		t.Fatalf("device-only file must be reported LocalOnly, got %+v", skipped)
	}
	if _, err := os.Stat(filepath.Join(target, "skills/.trash/old.md")); err != nil {
		t.Error("ignored file must survive")
	}

	// local edit + new layer version = conflict; local edit alone = LocalOnly
	w(t, filepath.Join(target, "CLAUDE.md"), "local edit")
	w(t, filepath.Join(target, "rules/a.md"), "local a")
	w(t, filepath.Join(layer, "CLAUDE.md"), "v2")
	actions, _ = Plan(layer, target, ignore, applied.Hashes)
	got := map[string]state.Decision{}
	for _, a := range actions {
		got[a.Rel] = a.Decision
	}
	if got["CLAUDE.md"] != state.Conflict || got["rules/a.md"] != state.LocalOnly {
		t.Fatalf("decisions wrong: %v", got)
	}
	_, skipped, _ = Apply(actions, st.NewBackup(), applied)
	if b, _ := os.ReadFile(filepath.Join(target, "CLAUDE.md")); string(b) != "local edit" {
		t.Error("conflict must not be overwritten")
	}
	if len(skipped) != 3 {
		t.Errorf("expected 3 skipped (conflict, local, device-only), got %d", len(skipped))
	}

	// capture: rules/a.md changed only on the device -> written back;
	// CLAUDE.md changed on both sides -> conflict; device-only file -> candidate
	res, err := Capture(layer, target, CaptureOptions{Ignore: ignore, LastApplied: applied.Hashes})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 1 || res.Updated[0] != "rules/a.md" {
		t.Errorf("updated=%v", res.Updated)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0] != "CLAUDE.md" {
		t.Errorf("conflicts=%v", res.Conflicts)
	}
	if len(res.Candidates) != 1 || res.Candidates[0] != "skills/mine/SKILL.md" {
		t.Errorf("candidates=%v", res.Candidates)
	}
	if b, _ := os.ReadFile(filepath.Join(layer, "CLAUDE.md")); string(b) != "v2" {
		t.Error("conflicting file must not be written back")
	}
	if b, _ := os.ReadFile(filepath.Join(layer, "rules/a.md")); string(b) != "local a" {
		t.Error("capture did not copy the local edit")
	}
	// a layer update that was pulled but not applied must not be reverted
	w(t, filepath.Join(layer, "rules/b.md"), "b1")
	actions, _ = Plan(layer, target, ignore, applied.Hashes)
	Apply(actions, st.NewBackup(), applied)
	w(t, filepath.Join(layer, "rules/b.md"), "b2")
	res, _ = Capture(layer, target, CaptureOptions{Ignore: ignore, LastApplied: applied.Hashes})
	if b, _ := os.ReadFile(filepath.Join(layer, "rules/b.md")); string(b) != "b2" {
		t.Error("unapplied layer version was reverted by capture")
	}
	// --add admits a device-only file, Allow can refuse it
	res, _ = Capture(layer, target, CaptureOptions{Ignore: ignore, LastApplied: applied.Hashes, Admit: []string{"skills/mine/SKILL.md"},
		Allow: func(rel string, content []byte) error { return errors.New("nope") }})
	if len(res.Refused) != 1 || len(res.Updated) != 0 {
		t.Errorf("Allow must be able to refuse: %+v", res)
	}
	res, _ = Capture(layer, target, CaptureOptions{Ignore: ignore, LastApplied: applied.Hashes, Admit: []string{"skills/mine/SKILL.md"}})
	if len(res.Updated) != 1 {
		t.Errorf("--add should admit the file, got %v", res.Updated)
	}

	// a file the layer drops is deleted only when it still matches what we wrote
	os.Remove(filepath.Join(layer, "rules/a.md"))
	actions, _ = Plan(layer, target, ignore, applied.Hashes)
	for _, a := range actions {
		if a.Rel == "rules/a.md" && (!a.Delete || a.Decision != state.LocalOnly) {
			t.Errorf("locally edited file dropped by the layer must not be deleted: %+v", a)
		}
	}
}

// Files the file manager writes are neither mirrored nor reported.
func TestPlanSkipsOSJunk(t *testing.T) {
	layer, target := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(layer, "a.md"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(layer, ".DS_Store"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(target, "Thumbs.db"), []byte("x"), 0o644)
	actions, err := Plan(layer, target, nil, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Rel != "a.md" {
		t.Errorf("actions = %+v", actions)
	}
}
