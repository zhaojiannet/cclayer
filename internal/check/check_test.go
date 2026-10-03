package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rules(fs []Finding) string {
	var r []string
	for _, f := range fs {
		r = append(r, f.Rule)
	}
	return strings.Join(r, "|")
}

func TestBaseRules(t *testing.T) {
	opt := Options{Base: true, Blocklist: []string{"acme", "acme-inc", "globex"}, HomeDir: "/Users/me"}
	content := []byte(strings.Join([]string{
		"name = me@real.example.org",
		`[includeIf "gitdir:~/work/"]`,
		"path = /Users/me/.claude/hooks/x.sh",
		"marketplace acme-marketplace",
		"macmed pacme",
		"token = ghp_" + strings.Repeat("a", 36),
		"docs me@example.com",
	}, "\n"))
	fs := File("claude/settings.json", content, opt)
	got := rules(fs)
	for _, want := range []string{"email address", "includeIf", "absolute home path", "blocklist word acme", "looks like a secret"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for _, f := range fs {
		if f.Line == 5 {
			t.Errorf("html/atmosphere must not hit blocklist word acme: %v", f)
		}
		if f.Line == 7 {
			t.Errorf("example.com must pass: %v", f)
		}
	}
}

func TestOverlayRules(t *testing.T) {
	opt := Options{Base: false}
	if fs := File("project/CLAUDE.local.md", []byte("contact me@acme.example\n"), opt); len(fs) != 0 {
		t.Errorf("emails are fine in overlays: %v", fs)
	}
	fs := File("git/fragment.gitconfig", []byte("[user]\n\temail = x@y.example\n[remote \"o\"]\n\turl = https://x\n"), opt)
	if got := rules(fs); !strings.Contains(got, "unconditional [user]") || !strings.Contains(got, "remote url") {
		t.Errorf("git rules missing: %s", got)
	}
	if fs := File("project/settings.local.json", []byte(`{"env": {"TOKEN": "sk-`+strings.Repeat("x", 24)+`"}}`), opt); len(fs) == 0 {
		t.Error("secrets must be rejected in every layer")
	}
	for _, line := range []string{
		"DB_PASSWORD=supersecretpassword123456",
		"export GITHUB_TOKEN=abcdefghijklmnopqrstuvwxyz",
		`"ANTHROPIC_API_KEY": "abcdefghijklmnopqrstuvwxyz"`,
	} {
		if fs := File("mcp/x.json", []byte(line), opt); len(fs) == 0 {
			t.Errorf("secret assignment not caught: %s", line)
		}
	}
	if fs := File("git/hooks/pre-push", []byte("[user]\n"), opt); len(fs) == 0 {
		t.Error("files under git/ must get the git rules")
	}
	if fs := File("claude/.claude.json", []byte("{}"), opt); len(fs) != 1 || fs[0].Line != 0 {
		t.Errorf("runtime file must be rejected whole: %v", fs)
	}
	if fs := File("claude/projects/x/mem.md", []byte("x"), opt); len(fs) != 1 {
		t.Errorf("projects/ must be rejected: %v", fs)
	}
}

// Runtime names are refused whatever their case: the file system that
// receives the mirror may not distinguish Projects from projects.
func TestRuntimeNamesIgnoreCase(t *testing.T) {
	for _, p := range []string{"PROJECTS/x/memory.md", "claude/History.JSONL", ".Credentials.json", "project\u017f/x.jsonl"} {
		if f := File(p, []byte("x"), Options{}); len(f) == 0 {
			t.Errorf("%s must be refused", p)
		}
	}
}

// The layer directory may itself be a link; links below it are findings.
func TestTreeAcceptsLinkedRoot(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	os.MkdirAll(real, 0o755)
	os.WriteFile(filepath.Join(real, "a.md"), []byte("x\n"), 0o644)
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available")
	}
	if f, err := Tree(link, Options{}); err != nil || len(f) != 0 {
		t.Errorf("a linked layer root must be walked: %v %v", f, err)
	}
	os.Symlink(filepath.Join(tmp, "elsewhere"), filepath.Join(real, "b.md"))
	if f, _ := Tree(link, Options{}); len(f) != 1 {
		t.Errorf("a link inside the layer is a finding: %v", f)
	}
}

// A private base lets email addresses through but still refuses overlay
// names and absolute home paths.
func TestPrivateBase(t *testing.T) {
	opt := Options{Base: true, Private: true, Blocklist: []string{"acme"}, HomeDir: "/home/me"}
	if f := File("claude/CLAUDE.md", []byte("mail me@real-company.example.org\n"), opt); len(f) != 0 {
		t.Errorf("a private base must accept an email: %v", f)
	}
	for _, line := range []string{"ask acme first\n", "see /home/me/notes\n"} {
		if f := File("claude/CLAUDE.md", []byte(line), opt); len(f) == 0 {
			t.Errorf("a private base must still refuse %q", line)
		}
	}
	opt.Private = false
	if f := File("claude/CLAUDE.md", []byte("mail me@real-company.example.org\n"), opt); len(f) == 0 {
		t.Error("a public base refuses an email")
	}
}
