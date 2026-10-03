package inject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/settings"
)

func TestSettingsLocalPreservesApprovals(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	path := filepath.Join(dir, ".claude", "settings.local.json")
	os.WriteFile(path, []byte(`{"permissions": {"allow": ["Bash(ls)"]}}`), 0o644)

	tmpl, _ := settings.Parse([]byte(`{"permissions": {"deny": ["Read(.env)"]}, "env": {"ORG": "acme"}}`))
	changed, err := SettingsLocal(dir, tmpl, []string{"permissions.deny", "env.ORG"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	cur, _ := settings.Load(path)
	if v, _ := cur.Get("permissions.allow"); len(v.([]any)) != 1 {
		t.Error("approvals lost")
	}
	if v, _ := cur.Get("env.ORG"); v != "acme" {
		t.Error("key not injected")
	}
	if changed, _ := SettingsLocal(dir, tmpl, []string{"permissions.deny", "env.ORG"}); changed {
		t.Error("second run must be a no-op")
	}
	if err := RemoveSettingsLocal(dir, []string{"permissions.deny", "env.ORG"}); err != nil {
		t.Fatal(err)
	}
	cur, _ = settings.Load(path)
	if _, ok := cur.Get("env"); ok {
		t.Error("env should be gone")
	}
	if _, ok := cur.Get("permissions.allow"); !ok {
		t.Error("approvals must survive removal")
	}
}

func TestClaudeLocalBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.local.md")
	os.WriteFile(path, []byte("# my notes\n\nkeep me\n"), 0o644)
	if _, err := ClaudeLocal(dir, "rule one"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.HasPrefix(s, "# my notes\n\nkeep me\n") || !strings.Contains(s, BeginMarker+"\nrule one\n"+EndMarker) {
		t.Fatalf("unexpected:\n%s", s)
	}
	// user adds text after the block; update must keep it
	os.WriteFile(path, []byte(s+"\nafter\n"), 0o644)
	if _, err := ClaudeLocal(dir, "rule two"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	s = string(b)
	if !strings.Contains(s, "rule two") || strings.Contains(s, "rule one") || !strings.Contains(s, "after") {
		t.Fatalf("update wrong:\n%s", s)
	}
	if Block(s) != "rule two" {
		t.Errorf("Block = %q", Block(s))
	}
	if err := RemoveClaudeLocal(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), BeginMarker) || !strings.Contains(string(b), "keep me") || !strings.Contains(string(b), "after") {
		t.Fatalf("remove wrong:\n%s", b)
	}
	// only the block: file disappears
	os.WriteFile(path, []byte(BeginMarker+"\nx\n"+EndMarker+"\n"), 0o644)
	RemoveClaudeLocal(dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file with only the block should be deleted")
	}
	// half a marker is an error, never a guess
	if _, err := SetBlock(BeginMarker+"\nx\n", "y"); err == nil {
		t.Error("expected error for unmatched marker")
	}
}

func TestEnsureExcludes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git", "ignore")
	added, err := EnsureExcludes(path)
	if err != nil || len(added) != 2 {
		t.Fatalf("added=%v err=%v", added, err)
	}
	// Claude Code already wrote the first pattern on another machine
	os.WriteFile(path, []byte("**/.claude/settings.local.json\n"), 0o644)
	added, _ = EnsureExcludes(path)
	if len(added) != 1 || added[0] != "**/CLAUDE.local.md" {
		t.Errorf("added=%v", added)
	}
	if added, _ = EnsureExcludes(path); added != nil {
		t.Errorf("third run must add nothing, got %v", added)
	}
}
