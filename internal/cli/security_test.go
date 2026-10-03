package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/settings"
)

func TestInjectRefusesSymlinkInProject(t *testing.T) {
	e, home := fixture(t)
	proj := filepath.Join(home, "Projects", "acme-api")
	outside := filepath.Join(home, "victim.md")
	os.Symlink(outside, filepath.Join(proj, "CLAUDE.local.md"))
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatalf("one project's symlink must not fail the whole apply: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("write went through the symlink")
	}
	if !strings.Contains(out(e), "symbolic link") {
		t.Errorf("the report should name the symlink:\n%s", out(e))
	}
	// everything else was still applied
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Error("mirror did not run")
	}
}

func TestExecCapableFragmentNeedsTrust(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "acme", "git", "fragment.gitconfig"), "[core]\n\thooksPath = ~/.config/acme/hooks\n")
	write(t, filepath.Join(home, "layers", "acme", "layer.toml"), `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Acme Me"
email = "me@acme.example"
[[match]]
remote = "github.com/acme-inc/*"
[git]
fragment = "git/fragment.gitconfig"
`)
	if code := Run(e, []string{"apply"}); code == 0 {
		t.Fatal("hooksPath without trust_exec must be refused")
	}
	if !strings.Contains(e.Stderr.(*bytes.Buffer).String(), "trust_exec") {
		t.Errorf("error should point at trust_exec: %s", e.Stderr.(*bytes.Buffer).String())
	}
	dev, _ := os.ReadFile(e.DevicePath)
	// top-level key, so it must go before the [clone] table
	write(t, e.DevicePath, "trust_exec = [\"acme\"]\n"+string(dev))
	e.Stderr = &bytes.Buffer{}
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatalf("trusted layer must apply: %s", e.Stderr.(*bytes.Buffer).String())
	}
	id, _ := os.ReadFile(filepath.Join(home, ".gitconfig.cclayer.d", "acme.gitconfig"))
	if !strings.Contains(string(id), `hookspath = "~/.config/acme/hooks"`) {
		t.Error("trusted fragment missing from the identity file")
	}
}

func TestIdentityWithNewlineIsRejected(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "acme", "layer.toml"), `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Evil\n[core]\n\thooksPath = /tmp"
email = "me@acme.example"
[[match]]
remote = "github.com/acme-inc/*"
`)
	if code := Run(e, []string{"apply"}); code == 0 {
		t.Fatal("identity with a newline must be rejected")
	}
}

func TestHooksChangeIsConfirmed(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md"]
settings_keys = ["theme", "hooks"]
`)
	write(t, filepath.Join(home, "layers", "base", "claude", "settings.json"), `{"theme": "dark", "hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "evil"}]}]}}`)
	// non-interactive: hooks skipped, theme applied
	e.Interactive = false
	Run(e, []string{"apply"})
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(b), "evil") || !strings.Contains(string(b), "dark") {
		t.Fatalf("hook mode must skip hooks but apply other keys:\n%s", b)
	}
	// interactive, declined
	e.Interactive = true
	e.Stdin = strings.NewReader("n\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(b), "evil") {
		t.Fatal("declined hooks were applied")
	}
	// interactive, accepted
	e.Stdin = strings.NewReader("y\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(b), "evil") {
		t.Fatal("accepted hooks were not applied")
	}
}

// statusLine is a command Claude Code runs, like hooks: same gate.
func TestStatusLineChangeIsConfirmed(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md"]
settings_keys = ["theme", "statusLine"]
`)
	write(t, filepath.Join(home, "layers", "base", "claude", "settings.json"), `{"theme": "dark", "statusLine": {"type": "command", "command": "evil"}}`)
	e.Interactive = false
	Run(e, []string{"apply"})
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(b), "evil") || !strings.Contains(string(b), "dark") {
		t.Fatalf("hook mode must skip statusLine but apply other keys:\n%s", b)
	}
	if !strings.Contains(out(e), "statusLine changed in the base layer") {
		t.Errorf("report must name the skipped key:\n%s", out(e))
	}
	e.Interactive = true
	e.Stdin = strings.NewReader("n\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(string(b), "evil") {
		t.Fatal("declined statusLine was applied")
	}
	e.Stdin = strings.NewReader("y\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(b), "evil") {
		t.Fatal("accepted statusLine was not applied")
	}
}

// An overlay's hooks land in a project's settings.local.json, where Claude
// Code runs them too; the overlay comes from a repository, so the same
// confirmation applies per project.
func TestInjectedHooksAreConfirmed(t *testing.T) {
	e, home := fixture(t)
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
settings_keys = ["permissions.deny", "hooks"]
`)
	write(t, filepath.Join(home, "layers", "acme", "project", "settings.local.json"), `{"permissions": {"deny": ["Bash(rm -rf *)"]}, "hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "evil"}]}]}}`)
	proj := filepath.Join(home, "Projects", "acme-api", ".claude", "settings.local.json")
	e.Interactive = false
	Run(e, []string{"apply"})
	b, _ := os.ReadFile(proj)
	if strings.Contains(string(b), "evil") || !strings.Contains(string(b), "rm -rf") || !strings.Contains(string(b), "Bash(ls)") {
		t.Fatalf("hook mode must skip injected hooks, write the other keys and keep the project's own:\n%s", b)
	}
	if !strings.Contains(out(e), "hooks from overlay acme need confirmation") {
		t.Errorf("report must say what was held back:\n%s", out(e))
	}
	e.Interactive = true
	e.Stdin = strings.NewReader("n\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(proj)
	if strings.Contains(string(b), "evil") {
		t.Fatal("declined injected hooks were written")
	}
	e.Stdin = strings.NewReader("y\n")
	e.stdin = nil
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatalf("apply: %s", e.Stderr.(*bytes.Buffer).String())
	}
	b, _ = os.ReadFile(proj)
	if !strings.Contains(string(b), "evil") {
		t.Fatal("accepted injected hooks were not written")
	}
}

// A script outside hooks/ that Claude Code runs, such as the statusLine
// script, and a hooks directory spelled with another case both go through
// the script gate.
func TestScriptsOutsideHooksAreConfirmed(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "statusline.sh", "Hooks/"]
`)
	write(t, filepath.Join(home, "layers", "base", "claude", "statusline.sh"), "#!/bin/sh\necho evil\n")
	write(t, filepath.Join(home, "layers", "base", "claude", "Hooks", "run.sh"), "echo evil\n")
	e.Interactive = false
	Run(e, []string{"apply"})
	for _, f := range []string{"statusline.sh", "Hooks/run.sh"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", f)); err == nil {
			t.Errorf("hook mode must hold %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Error("plain files must still be mirrored")
	}
	e.Interactive = true
	e.Stdin = strings.NewReader("y\ny\n")
	e.stdin = nil
	Run(e, []string{"apply"})
	for _, f := range []string{"statusline.sh", "Hooks/run.sh"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", f)); err != nil {
			t.Errorf("accepted script %s was not applied", f)
		}
	}
}

// Exec keys and keys cclayer does not count as harmless, enabledPlugins
// among them, are confirmed; theme and attribution pass, and a path below an
// exec key is reported under the exec key.
func TestGatedKeysChanged(t *testing.T) {
	cur, _ := settings.Parse([]byte(`{}`))
	want, _ := settings.Parse([]byte(`{"apiKeyHelper": "evil", "extraKnownMarketplaces": {"m": {}}, "enabledPlugins": {"x@m": true}, "theme": "dark", "attribution": {"commit": "x"}, "sandbox": {"enabled": false}, "hooks": {"SessionStart": []}}`))
	got := gatedKeysChanged(cur, want, []string{"apiKeyHelper", "extraKnownMarketplaces", "enabledPlugins", "theme", "attribution.commit", "sandbox.enabled", "hooks.SessionStart"})
	if strings.Join(got, ",") != "apiKeyHelper,extraKnownMarketplaces,enabledPlugins,sandbox.enabled,hooks" {
		t.Errorf("gatedKeysChanged = %v", got)
	}
	unchanged, _ := settings.Parse([]byte(`{"apiKeyHelper": "evil"}`))
	if got := gatedKeysChanged(unchanged, unchanged, []string{"apiKeyHelper"}); len(got) != 0 {
		t.Errorf("an unchanged key must not be asked about: %v", got)
	}
}

// Anything Claude Code may run is held: a script in any language anywhere,
// skill files, and the layer's version of a conflicting script, which is
// shown before it overwrites. Plain markdown outside those directories is
// mirrored as it is.
func TestCodeIsHeldWherever(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "rules/", "scripts/", "skills/"]
`)
	write(t, filepath.Join(home, "layers", "base", "claude", "scripts", "status.py"), "print('evil')\n")
	write(t, filepath.Join(home, "layers", "base", "claude", "skills", "x", "SKILL.md"), "!`evil`\n")
	e.Interactive = false
	Run(e, []string{"apply"})
	for _, f := range []string{"scripts/status.py", "skills/x/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(home, ".claude", f)); err == nil {
			t.Errorf("hook mode must hold %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "rules", "comments.md")); err != nil {
		t.Error("plain markdown must still be mirrored")
	}
	e.Interactive = true
	e.Stdin = strings.NewReader("y\ny\n")
	e.stdin = nil
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"apply"})
	if !strings.Contains(out(e), "print('evil')") {
		t.Errorf("the content must be shown before it is applied:\n%s", out(e))
	}
	// edited here and changed in the layer: the layer's version is shown before it overwrites
	write(t, filepath.Join(home, ".claude", "scripts", "status.py"), "print('mine')\n")
	write(t, filepath.Join(home, "layers", "base", "claude", "scripts", "status.py"), "print('evil v2')\n")
	e.Stdin = strings.NewReader("y\ny\n")
	e.stdin = nil
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"apply"})
	if !strings.Contains(out(e), "print('evil v2')") {
		t.Errorf("a conflicting script must be shown before it overwrites:\n%s", out(e))
	}
}

// Content cclayer wrote elsewhere does not count as an approval: plain
// markdown copied into hooks/ is still held.
func TestApprovalIsPerConfirmedFile(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "hooks/"]
`)
	e.Interactive = false
	Run(e, []string{"apply"})
	shared, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if len(shared) == 0 {
		t.Fatal("CLAUDE.md not mirrored")
	}
	write(t, filepath.Join(home, "layers", "base", "claude", "hooks", "run.sh"), string(shared))
	Run(e, []string{"apply"})
	if _, err := os.Stat(filepath.Join(home, ".claude", "hooks", "run.sh")); err == nil {
		t.Error("content written elsewhere must not approve a hook script")
	}
}

// A symbolic link in a layer is never followed, wherever it sits; and a
// directory spelled with Unicode case folding is still the skills directory.
func TestLayerLinksAndFoldedNames(t *testing.T) {
	e, home := fixture(t)
	secret := filepath.Join(home, "secret.txt")
	write(t, secret, "PRIVATE KEY\n")
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "rules/", "notes.md"]
`)
	if err := os.Symlink(secret, filepath.Join(home, "layers", "base", "claude", "notes.md")); err != nil {
		t.Skip("symlinks not available")
	}
	e.Interactive = false
	if code := Run(e, []string{"apply"}); code == 0 {
		t.Error("a linked file in the layer must stop apply")
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude", "notes.md")); strings.Contains(string(b), "PRIVATE") {
		t.Fatal("a link in the layer was followed")
	}
	if !mayRun("ſkills/x/SKILL.md", "", []byte("x")) || !mayRun("HOOKS/run.md", "", []byte("x")) || mayRun("rules/a.md", "", []byte("x")) {
		t.Error("mayRun must fold names the way the file system does")
	}
}

// A second overlay cannot claim the owner another overlay already has: git
// would apply its identity in that team's repositories.
func TestOverlaysCannotShareAnOwner(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "evil", "layer.toml"), `
[layer]
name = "evil"
kind = "overlay"
[identity]
name = "Evil"
email = "evil@evil.example"
[[match]]
remote = "github.com/Acme-Inc/*"
`)
	dev, _ := os.ReadFile(e.DevicePath)
	s := strings.Replace(string(dev), `layers = ["base", "acme"]`, `layers = ["base", "acme", "evil"]`, 1)
	s = strings.Replace(s, "[clone]\n", "[clone]\nevil = \""+filepath.ToSlash(filepath.Join(home, "layers", "evil"))+"\"\n", 1)
	write(t, e.DevicePath, s)
	if code := Run(e, []string{"apply"}); code == 0 {
		t.Fatal("two overlays claiming one owner must stop apply")
	}
	if !strings.Contains(e.Stderr.(*bytes.Buffer).String(), "both claim github.com/acme-inc") {
		t.Errorf("error must name the owner: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".gitconfig.cclayer")); strings.Contains(string(b), "evil") {
		t.Error("the git configuration must not include the second overlay")
	}
}

// A restrictive key the layer leaves out or loosens is a change to confirm:
// the hook mode keeps the user's limits.
func TestRestrictionsAreNotDroppedSilently(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "layer.toml"), `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md"]
settings_keys = ["theme", "permissions.deny", "disableAllHooks"]
`)
	write(t, filepath.Join(home, "layers", "base", "claude", "settings.json"), `{"theme": "dark", "permissions": {"deny": ["Read(.env)", "Read(~/.ssh/**)"]}}`)
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"theme": "light", "permissions": {"deny": ["Read(~/.ssh/**)"]}, "disableAllHooks": true}`)
	e.Interactive = false
	Run(e, []string{"apply"})
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	doc, _ := settings.Parse(b)
	if v, _ := doc.Get("disableAllHooks"); v != true {
		t.Errorf("a limit the layer leaves out must stay: %s", b)
	}
	if v, _ := doc.Get("permissions.deny"); len(v.([]any)) != 2 {
		t.Errorf("adding to a deny list needs no question: %s", b)
	}
	if v, _ := doc.Get("theme"); v != "dark" {
		t.Errorf("harmless keys still merge: %s", b)
	}
	write(t, filepath.Join(home, "layers", "base", "claude", "settings.json"), `{"theme": "dark", "permissions": {"deny": ["Read(.env)"]}}`)
	Run(e, []string{"apply"})
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(b), "Read(~/.ssh/**)") {
		t.Errorf("removing a deny rule must be confirmed: %s", b)
	}
}

// capture never writes through a link in the layer.
func TestCaptureRefusesLayerLink(t *testing.T) {
	e, home := fixture(t)
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatal(e.Stderr.(*bytes.Buffer).String())
	}
	base := filepath.Join(home, "layers", "base")
	outside := filepath.Join(home, "outside")
	os.MkdirAll(filepath.Join(outside, "rules"), 0o755)
	write(t, filepath.Join(outside, "rules", "comments.md"), "outside\n")
	os.RemoveAll(filepath.Join(base, "claude"))
	if err := os.Symlink(outside, filepath.Join(base, "claude")); err != nil {
		t.Skip("symlinks not available")
	}
	write(t, filepath.Join(home, ".claude", "rules", "comments.md"), "device content\n")
	Run(e, []string{"capture"})
	if b, _ := os.ReadFile(filepath.Join(outside, "rules", "comments.md")); string(b) != "outside\n" {
		t.Fatalf("capture wrote outside the layer: %q", b)
	}
}

// A base that has not taken in any settings yet leaves the device's
// settings.json alone, and capture then fills the layer from it.
func TestEmptyLayerKeepsDeviceSettings(t *testing.T) {
	e, home := fixture(t)
	os.Remove(filepath.Join(home, "layers", "base", "claude", "settings.json"))
	e.Interactive = false
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatal(e.Stderr.(*bytes.Buffer).String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(b), `"light"`) {
		t.Fatalf("device settings were changed by a layer without settings.json:\n%s", b)
	}
	// profiles get the same treatment
	dev, _ := os.ReadFile(e.DevicePath)
	write(t, e.DevicePath, "profiles = true\nprofile_dir = \""+filepath.ToSlash(filepath.Join(home, "profiles"))+"\"\n"+string(dev))
	write(t, filepath.Join(home, "profiles", "acme", "settings.json"), `{"theme": "light"}`)
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatal(e.Stderr.(*bytes.Buffer).String())
	}
	if pb, _ := os.ReadFile(filepath.Join(home, "profiles", "acme", "settings.json")); !strings.Contains(string(pb), `"light"`) {
		t.Fatalf("a profile's settings were changed by a layer without settings.json:\n%s", pb)
	}
	e.Interactive = true
	if code := Run(e, []string{"capture"}); code != 0 {
		t.Fatal(e.Stderr.(*bytes.Buffer).String())
	}
	l, _ := os.ReadFile(filepath.Join(home, "layers", "base", "claude", "settings.json"))
	if !strings.Contains(string(l), `"light"`) {
		t.Errorf("capture must take the owned keys into the layer:\n%s", l)
	}
}

// --add takes a directory, with or without the slash, and refuses a path the
// base does not mirror.
func TestCaptureAddDirectory(t *testing.T) {
	e, home := fixture(t)
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatal(e.Stderr.(*bytes.Buffer).String())
	}
	write(t, filepath.Join(home, ".claude", "rules", "new.md"), "new rule\n")
	write(t, filepath.Join(home, ".claude", "rules", "sub", "deep.md"), "deep rule\n")
	for _, arg := range []string{"rules/", "rules"} {
		os.RemoveAll(filepath.Join(home, "layers", "base", "claude", "rules", "sub"))
		os.Remove(filepath.Join(home, "layers", "base", "claude", "rules", "new.md"))
		if code := Run(e, []string{"capture", "--add", arg}); code != 0 {
			t.Fatalf("--add %s: %s", arg, e.Stderr.(*bytes.Buffer).String())
		}
		for _, f := range []string{"new.md", "sub/deep.md"} {
			if _, err := os.Stat(filepath.Join(home, "layers", "base", "claude", "rules", f)); err != nil {
				t.Errorf("--add %s did not take in rules/%s", arg, f)
			}
		}
	}
	e.Stderr = &bytes.Buffer{}
	if code := Run(e, []string{"capture", "--add", "agents/"}); code == 0 || !strings.Contains(e.Stderr.(*bytes.Buffer).String(), "outside the paths") {
		t.Errorf("an --add outside the base paths must be refused: %s", e.Stderr.(*bytes.Buffer).String())
	}
}

// A switch the layer keeps off is not asked about; one whose false loosens
// a guard, or that turns something off the device had on, still is.
func TestKeepsOff(t *testing.T) {
	cur, _ := settings.Parse([]byte(`{"autoMemoryEnabled": true}`))
	want, _ := settings.Parse([]byte(`{"remoteControlAtStartup": false, "respectGitignore": false, "disableAllHooks": false, "sandbox": {"enabled": false}, "autoMemoryEnabled": false, "skipWorkflowUsageWarning": true}`))
	got := gatedKeysChanged(cur, want, []string{"remoteControlAtStartup", "respectGitignore", "disableAllHooks", "sandbox.enabled", "autoMemoryEnabled", "skipWorkflowUsageWarning"})
	if strings.Join(got, ",") != "respectGitignore,sandbox.enabled,autoMemoryEnabled,skipWorkflowUsageWarning" {
		t.Errorf("gatedKeysChanged = %v", got)
	}
}
