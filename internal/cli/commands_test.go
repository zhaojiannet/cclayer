package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/manifest"
	"github.com/zhaojiannet/cclayer/internal/settings"
)

type recRunner struct{ calls []string }

func (r *recRunner) Run(dir, name string, args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", nil
}

func out(e *Env) string { return e.Stdout.(*bytes.Buffer).String() }

func TestCheckRefusesIdentityInBase(t *testing.T) {
	e, home := fixture(t)
	if code := Run(e, []string{"check"}); code != 0 {
		t.Fatalf("clean layers must pass: %s", e.Stderr.(*bytes.Buffer).String())
	}
	write(t, filepath.Join(home, "layers", "base", "claude", "rules", "me.md"), "mail me at me@real.example.org\n")
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"check"}); code == 0 {
		t.Fatal("email in base must fail")
	}
	if !strings.Contains(out(e), "email address in the base layer") {
		t.Errorf("finding missing:\n%s", out(e))
	}
}

func TestCaptureWritesBackAndRefusesOverlayPlugin(t *testing.T) {
	e, home := fixture(t)
	Run(e, []string{"apply"})
	// local edits: mirrored file, owned setting, injected key, managed block
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# edited\n")
	write(t, filepath.Join(home, ".claude", "rules", "local.md"), "device only\n")
	devSettings := filepath.Join(home, ".claude", "settings.json")
	doc, _ := settings.Load(devSettings)
	doc.Set("theme", "solarized")
	doc.Set("permissions.allow", []any{"Bash(secret-thing)"})
	b, _ := doc.Marshal()
	os.WriteFile(devSettings, b, 0o644)
	proj := filepath.Join(home, "Projects", "acme-api")
	loc, _ := settings.Load(filepath.Join(proj, ".claude", "settings.local.json"))
	loc.Set("permissions.deny", []any{"Bash(rm -rf *)", "Bash(sudo *)"})
	loc.Set("permissions.allow", []any{"Bash(ls)", "Bash(cat /Users/me/other-client/x)"})
	b, _ = loc.Marshal()
	os.WriteFile(filepath.Join(proj, ".claude", "settings.local.json"), b, 0o644)
	cl, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.local.md"))
	os.WriteFile(filepath.Join(proj, "CLAUDE.local.md"), []byte(strings.Replace(string(cl), "acme rule", "acme rule v2", 1)+"my own note\n"), 0o644)

	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"capture"}); code != 0 {
		t.Fatalf("capture failed: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if b, _ := os.ReadFile(filepath.Join(home, "layers", "base", "claude", "CLAUDE.md")); string(b) != "# edited\n" {
		t.Error("mirrored edit not captured")
	}
	baseDoc, _ := settings.Load(filepath.Join(home, "layers", "base", "claude", "settings.json"))
	if v, _ := baseDoc.Get("theme"); v != "solarized" {
		t.Error("owned key not captured")
	}
	if _, ok := baseDoc.Get("permissions.allow"); ok {
		t.Error("permissions.allow must never reach the base")
	}
	tmpl, _ := settings.Load(filepath.Join(home, "layers", "acme", "project", "settings.local.json"))
	if v, _ := tmpl.Get("permissions.deny"); len(v.([]any)) != 2 {
		t.Error("injected key change not captured")
	}
	if _, ok := tmpl.Get("permissions.allow"); ok {
		t.Error("project allow rules must never reach the overlay")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "layers", "acme", "project", "CLAUDE.local.md")); string(b) != "acme rule v2\n" {
		t.Errorf("block not captured: %q", b)
	}
	if !strings.Contains(out(e), "rules/local.md") {
		t.Errorf("device-only file must be listed as candidate:\n%s", out(e))
	}
	if _, err := os.Stat(filepath.Join(home, "layers", "base", "claude", "rules", "local.md")); err == nil {
		t.Error("candidate must not be captured without --add")
	}
	Run(e, []string{"capture", "--add", "rules/local.md"})
	if _, err := os.Stat(filepath.Join(home, "layers", "base", "claude", "rules", "local.md")); err != nil {
		t.Error("--add must admit the file")
	}

	// an overlay-declared plugin installed at user scope must not enter the base
	write(t, filepath.Join(home, "layers", "acme", "project", "settings.local.json"), `{"permissions": {"deny": ["x"]}, "enabledPlugins": {"tools@acme-market": true}}`)
	write(t, filepath.Join(home, "layers", "acme", "layer.toml"), `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Acme Me"
email = "me@acme.example"
[[match]]
remote = "github.com/acme-inc/*"
[inject]
settings_keys = ["permissions.deny", "enabledPlugins"]
`)
	doc, _ = settings.Load(devSettings)
	doc.Set("enabledPlugins", map[string]any{"tools@acme-market": true})
	b, _ = doc.Marshal()
	os.WriteFile(devSettings, b, 0o644)
	// base owns enabledPlugins in this variant
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md"]
settings_keys = ["theme", "enabledPlugins"]
`)
	e.Stderr = &bytes.Buffer{}
	if code := Run(e, []string{"capture"}); code == 0 || !strings.Contains(e.Stderr.(*bytes.Buffer).String(), "belongs to overlay acme") {
		t.Errorf("overlay plugin must be refused: code=%d %s", code, e.Stderr.(*bytes.Buffer).String())
	}
}

func TestLeaveRemovesLayer(t *testing.T) {
	e, home := fixture(t)
	Run(e, []string{"apply"})
	rr := &recRunner{}
	e.Runner = rr
	old := claudeAvailable
	claudeAvailable = func() bool { return true }
	defer func() { claudeAvailable = old }()
	e.Stdin = strings.NewReader("y\n")
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"leave", "acme"}); code != 0 {
		t.Fatalf("leave failed: %s", e.Stderr.(*bytes.Buffer).String())
	}
	proj := filepath.Join(home, "Projects", "acme-api")
	if len(rr.calls) != 1 || rr.calls[0] != "purge --yes "+proj {
		t.Errorf("purge not run as planned: %v", rr.calls)
	}
	if _, err := os.Stat(filepath.Join(home, "layers", "acme")); !os.IsNotExist(err) {
		t.Error("clone not removed")
	}
	main, _ := os.ReadFile(filepath.Join(home, ".gitconfig.cclayer"))
	if strings.Contains(string(main), "acme-inc") {
		t.Error("includeIf for the left layer survived")
	}
	loc, _ := settings.Load(filepath.Join(proj, ".claude", "settings.local.json"))
	if _, ok := loc.Get("permissions.deny"); ok {
		t.Error("injected key not removed")
	}
	if _, ok := loc.Get("permissions.allow"); !ok {
		t.Error("project approvals must survive leave")
	}
	if b, _ := os.ReadFile(filepath.Join(proj, "CLAUDE.local.md")); strings.Contains(string(b), "acme rule") {
		t.Error("managed block not removed")
	}
	if b, _ := os.ReadFile(e.DevicePath); strings.Contains(string(b), "\"acme\"") {
		t.Errorf("device manifest still lists acme:\n%s", b)
	}
}

func TestStatusAndDoctorRun(t *testing.T) {
	e, _ := fixture(t)
	Run(e, []string{"apply"})
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"status"}); code != 0 {
		t.Fatalf("status: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if !strings.Contains(out(e), "projects of acme") || !strings.Contains(out(e), "acme-api") {
		t.Errorf("status output:\n%s", out(e))
	}
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"doctor"}); code != 0 {
		t.Fatalf("doctor: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if !strings.Contains(out(e), "include block is in place") {
		t.Errorf("doctor output:\n%s", out(e))
	}
}

// init does not add single characters or excepted words to the blocklist.
func TestFillBlocklistSkips(t *testing.T) {
	l := &Loaded{Device: &manifest.Device{BlocklistExcept: []string{"acme-inc"}}}
	l.Layers = []Layer{
		{Name: "base", Manifest: &manifest.Layer{}},
		{Name: "acme", Manifest: &manifest.Layer{Identity: &manifest.Identity{Name: "A", Email: "m@acme.example"}, Match: []manifest.Match{{Remote: "github.com/acme-inc/*"}}}},
	}
	fillBlocklist(l)
	if got := strings.Join(l.Device.Blocklist, ","); got != "acme,acme.example" {
		t.Errorf("blocklist = %s", got)
	}
}
