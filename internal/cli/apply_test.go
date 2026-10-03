package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/settings"
	"github.com/zhaojiannet/cclayer/internal/state"
)

type noRunner struct{}

func (noRunner) Run(dir, name string, args ...string) (string, error) { return "", nil }

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitc(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// fixture builds a fake home with a base layer, one overlay and one matching
// project, and returns the Env pointing at it.
func fixture(t *testing.T) (*Env, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	layers := filepath.Join(home, "layers")
	base := filepath.Join(layers, "base")
	write(t, filepath.Join(base, "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "rules/"]
settings_keys = ["theme", "permissions.deny"]
ignore = ["rules/.trash/"]
[git]
fragment = "git/fragment.gitconfig"
`)
	write(t, filepath.Join(base, "claude", "CLAUDE.md"), "# shared rules\n")
	write(t, filepath.Join(base, "claude", "rules", "comments.md"), "comments\n")
	write(t, filepath.Join(base, "claude", "settings.json"), `{"theme": "dark", "permissions": {"deny": ["Read(.env)"]}}`)
	write(t, filepath.Join(base, "git", "fragment.gitconfig"), "[pull]\n\trebase = true\n")

	acme := filepath.Join(layers, "acme")
	write(t, filepath.Join(acme, "layer.toml"), `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Acme Me"
email = "me@acme.example"
[[match]]
remote = "github.com/acme-inc/*"
[inject]
settings_keys = ["permissions.deny"]
claude_local = "project/CLAUDE.local.md"
`)
	write(t, filepath.Join(acme, "project", "settings.local.json"), `{"permissions": {"deny": ["Bash(rm -rf *)"]}}`)
	write(t, filepath.Join(acme, "project", "CLAUDE.local.md"), "acme rule\n")

	proj := filepath.Join(home, "Projects", "acme-api")
	os.MkdirAll(proj, 0o755)
	gitc(t, proj, "init", "-q")
	gitc(t, proj, "remote", "add", "origin", "git@github.com:acme-inc/api.git")
	write(t, filepath.Join(proj, ".claude", "settings.local.json"), `{"permissions": {"allow": ["Bash(ls)"]}}`)

	// device state before cclayer: an existing settings.json and gitconfig
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"theme": "light", "permissions": {"defaultMode": "auto", "allow": ["Bash(pwd)"]}, "env": {"X": "1"}}`)
	write(t, filepath.Join(home, ".gitconfig"), "[credential]\n\thelper = osxkeychain\n")

	devicePath := filepath.Join(home, ".config", "cclayer", "device.toml")
	write(t, devicePath, `
layers = ["base", "acme"]
roots = ["`+filepath.ToSlash(filepath.Join(home, "Projects"))+`"]
[clone]
base = "`+filepath.ToSlash(base)+`"
acme = "`+filepath.ToSlash(acme)+`"
[repo]
base = "https://git.example/base.git"
acme = "https://git.example/acme.git"
`)
	e := &Env{
		DevicePath: devicePath, Home: home,
		ClaudeDir: filepath.Join(home, ".claude"), GitConfig: filepath.Join(home, ".gitconfig"),
		State:  state.Dir{Path: filepath.Join(home, ".local", "state", "cclayer")},
		Runner: noRunner{}, Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Interactive: true,
	}
	return e, home
}

func TestApplyEndToEnd(t *testing.T) {
	e, home := fixture(t)
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatalf("apply exit %d: %s", code, e.Stderr.(*bytes.Buffer).String())
	}
	// mirrored file
	if b, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); string(b) != "# shared rules\n" {
		t.Errorf("CLAUDE.md not mirrored: %q", b)
	}
	// settings merged by owned path only
	doc, _ := settings.Load(filepath.Join(home, ".claude", "settings.json"))
	if v, _ := doc.Get("theme"); v != "dark" {
		t.Errorf("theme = %v", v)
	}
	if v, _ := doc.Get("permissions.defaultMode"); v != "auto" {
		t.Errorf("defaultMode lost: %v", v)
	}
	if v, _ := doc.Get("permissions.allow"); v == nil {
		t.Error("allow lost")
	}
	if v, _ := doc.Get("env.X"); v != "1" {
		t.Error("env lost")
	}
	// git files
	gc, _ := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if !strings.HasPrefix(string(gc), "[credential]\n\thelper = osxkeychain\n") || !strings.Contains(string(gc), "path = ~/.gitconfig.cclayer") {
		t.Errorf("gitconfig wrong:\n%s", gc)
	}
	main, _ := os.ReadFile(filepath.Join(home, ".gitconfig.cclayer"))
	if !strings.Contains(string(main), "useConfigOnly = true") || !strings.Contains(string(main), "[aA][cC][mM][eE]-[iI][nN][cC]/**") || !strings.Contains(string(main), `rebase = "true"`) {
		t.Errorf("gitconfig.cclayer wrong:\n%s", main)
	}
	id, _ := os.ReadFile(filepath.Join(home, ".gitconfig.cclayer.d", "acme.gitconfig"))
	if !strings.Contains(string(id), "email = me@acme.example") {
		t.Errorf("identity file wrong:\n%s", id)
	}
	// project injection preserves approvals
	loc, _ := settings.Load(filepath.Join(home, "Projects", "acme-api", ".claude", "settings.local.json"))
	if v, _ := loc.Get("permissions.allow"); v == nil {
		t.Error("project approvals lost")
	}
	if v, _ := loc.Get("permissions.deny"); v == nil {
		t.Error("project deny not injected")
	}
	cl, _ := os.ReadFile(filepath.Join(home, "Projects", "acme-api", "CLAUDE.local.md"))
	if !strings.Contains(string(cl), "acme rule") {
		t.Errorf("CLAUDE.local.md: %q", cl)
	}
	// excludes registered
	ex, _ := os.ReadFile(filepath.Join(home, ".config", "git", "ignore"))
	if !strings.Contains(string(ex), "**/CLAUDE.local.md") {
		t.Errorf("excludes: %q", ex)
	}
	// state recorded
	applied, _ := e.State.Load()
	if applied.Projects[filepath.Join(home, "Projects", "acme-api")] != "acme" {
		t.Errorf("project map: %v", applied.Projects)
	}

	// second run: nothing to do, no new backup dir
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"apply"})
	if out := e.Stdout.(*bytes.Buffer).String(); !strings.Contains(out, "Nothing to do") {
		t.Errorf("second apply should be a no-op:\n%s", out)
	}

	// local edit + layer change = conflict; non-interactive leaves it alone
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# edited here\n")
	write(t, filepath.Join(home, "layers", "base", "claude", "CLAUDE.md"), "# layer v2\n")
	e.Interactive = false
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"apply"})
	if b, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); string(b) != "# edited here\n" {
		t.Error("conflict overwritten in non-interactive mode")
	}
	if out := e.Stdout.(*bytes.Buffer).String(); !strings.Contains(out, "conflicts") {
		t.Errorf("hook line must name the conflict:\n%s", out)
	}
	// interactive "y" resolves it
	e.Interactive = true
	e.Stdin = strings.NewReader("y\n")
	Run(e, []string{"apply"})
	if b, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); string(b) != "# layer v2\n" {
		t.Error("conflict not resolved after yes")
	}
}

func TestApplyHookRespectsAutoPull(t *testing.T) {
	e, home := fixture(t)
	e.Interactive = true
	if code := Run(e, []string{"apply", "--hook"}); code != 0 {
		t.Fatal("hook must exit 0")
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig.cclayer")); !os.IsNotExist(err) {
		t.Error("auto_pull=false hook must do nothing")
	}
}
