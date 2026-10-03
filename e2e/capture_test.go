package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureConflictNotReverted(t *testing.T) {
	w, _ := applied(t)
	// another device pushed; this device pulled into the clone but did not apply
	w.pushToRemote("base", "claude/CLAUDE.md", "# layer v2\n")
	w.git(filepath.Join(w.home, "layers", "base"), "pull", "-q", "--ff-only")
	out, code := w.run("", "capture")
	if code != 0 || w.read("layers/base/claude/CLAUDE.md") != "# layer v2\n" {
		t.Fatalf("unapplied layer version reverted: %d %s", code, out)
	}
	w.write(".claude/CLAUDE.md", "# device edit\n")
	out, _ = w.run("", "capture")
	if w.read("layers/base/claude/CLAUDE.md") != "# layer v2\n" || !strings.Contains(out, "Changed both here and in the layer") {
		t.Fatalf("conflict must be reported, not written:\n%s", out)
	}
}

func TestCaptureCandidatesNeedAdd(t *testing.T) {
	w, _ := applied(t)
	w.write(".claude/rules/local.md", "device only\n")
	out, _ := w.run("", "capture")
	if !strings.Contains(out, "rules/local.md") || w.read("layers/base/claude/rules/local.md") != "" {
		t.Fatalf("candidate must be listed, not captured:\n%s", out)
	}
	if out, code := w.run("", "capture", "--add", "../x"); code != 2 {
		t.Errorf("--add ../x must be a usage error: %d %s", code, out)
	}
	w.run("", "capture", "--add", "rules/local.md")
	if w.read("layers/base/claude/rules/local.md") != "device only\n" {
		t.Error("--add did not admit the file")
	}
}

func TestCaptureRefusesSecret(t *testing.T) {
	w, _ := applied(t)
	w.write(".claude/rules/comments.md", "token = ghp_"+strings.Repeat("b", 36)+"\n")
	out, code := w.run("", "capture")
	if code == 0 || !strings.Contains(out, "refused") {
		t.Fatalf("secret must be refused: %d %s", code, out)
	}
	if strings.Contains(w.read("layers/base/claude/rules/comments.md"), "ghp_") {
		t.Error("secret reached the layer")
	}
}

func TestCaptureOwnedKeysOnly(t *testing.T) {
	w, _ := applied(t)
	set := w.read(".claude/settings.json")
	set = strings.Replace(set, `"dark"`, `"solarized"`, 1)
	w.write(".claude/settings.json", set)
	w.run("", "capture")
	layer := w.read("layers/base/claude/settings.json")
	if !strings.Contains(layer, "solarized") {
		t.Error("owned key not captured")
	}
	if strings.Contains(layer, "Bash(pwd)") || strings.Contains(layer, "defaultMode") {
		t.Errorf("device-only keys leaked into the base:\n%s", layer)
	}
}

func TestCaptureOverlayConsistency(t *testing.T) {
	w, sd := applied(t)
	second := w.project("acme-web", "git@github.com:acme-inc/web.git")
	w.run("y\n", "apply")
	// change the injected key in one project only
	loc := filepath.Join(sd.proj, ".claude", "settings.local.json")
	w.write(strings.TrimPrefix(loc, w.home+"/"), `{"permissions": {"allow": ["Bash(ls)"], "deny": ["Bash(rm -rf *)", "Bash(sudo *)"]}, "enabledPlugins": {"tools@acme-market": true}}`)
	out, code := w.run("", "capture")
	if code == 0 || !strings.Contains(out, "differs between") {
		t.Fatalf("inconsistent projects must be refused: %d %s", code, out)
	}
	if out, code := w.run("", "capture", "--from", filepath.Join(w.home, "nowhere")); code == 0 || !strings.Contains(out, "not a project") {
		t.Errorf("--from with an unknown project must fail: %d %s", code, out)
	}
	out, code = w.run("", "capture", "--from", sd.proj)
	if code != 0 || !strings.Contains(w.read("layers/acme/project/settings.local.json"), "sudo") {
		t.Fatalf("--from must take that project's values: %d %s", code, out)
	}
	_ = second
}

func TestCaptureRefusesOverlayPluginInBase(t *testing.T) {
	w, _ := applied(t)
	set := w.read(".claude/settings.json")
	set = strings.Replace(set, `"theme": "dark"`, `"theme": "dark", "enabledPlugins": {"tools@acme-market": true}`, 1)
	w.write(".claude/settings.json", set)
	// base owns enabledPlugins in this variant
	w.pushToRemote("base", "layer.toml", strings.Replace(w.read("seed/base/layer.toml"), `settings_keys = ["theme", "permissions.deny", "hooks"]`, `settings_keys = ["theme", "permissions.deny", "hooks", "enabledPlugins"]`, 1))
	w.git(filepath.Join(w.home, "layers", "base"), "pull", "-q", "--ff-only")
	out, code := w.run("", "capture")
	if code == 0 || !strings.Contains(out, "belongs to overlay acme") {
		t.Fatalf("overlay plugin must be refused: %d %s", code, out)
	}
	if strings.Contains(w.read("layers/base/claude/settings.json"), "acme-market") {
		t.Error("overlay plugin reached the base")
	}
	_ = os.Remove
}
