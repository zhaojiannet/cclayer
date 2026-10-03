package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDevice(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "device.toml", `
layers = ["base", "personal", "acme"]
default_identity = "personal"
roots = ["~/Cores/Projects"]
auto_pull = false
blocklist = ["acme-inc"]
[clone]
base = "~/.local/share/cclayer/base"
personal = "~/.local/share/cclayer/personal"
acme = "~/.local/share/cclayer/acme"
`)
	d, err := LoadDevice(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Layers[2] != "acme" || !strings.HasSuffix(filepath.ToSlash(d.ClonePath("acme")), "/.local/share/cclayer/acme") {
		t.Errorf("unexpected %+v", d)
	}
}

func TestDeviceValidateErrors(t *testing.T) {
	cases := map[string]string{
		"base first": `layers = ["acme", "base"]
roots = ["~"]
[clone]
base = "x"
acme = "y"`,
		"missing clone": `layers = ["base", "acme"]
roots = ["~"]
[clone]
base = "x"`,
		"default identity unknown": `layers = ["base"]
default_identity = "acme"
roots = ["~"]
[clone]
base = "x"`,
		"no roots": `layers = ["base"]
[clone]
base = "x"`,
	}
	for name, body := range cases {
		p := write(t, t.TempDir(), "device.toml", body)
		if _, err := LoadDevice(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLayerValidate(t *testing.T) {
	good := t.TempDir()
	write(t, good, "layer.toml", `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Someone"
email = "someone@example.com"
[[match]]
remote = "github.com/acme-inc/*"
[inject]
settings_keys = ["permissions.deny", "env"]
`)
	// env is a device key, so inject must reject it
	if _, err := LoadLayer(good); err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("expected env rejection, got %v", err)
	}

	write(t, good, "layer.toml", `
[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Someone"
email = "someone@example.com"
[[match]]
remote = "github.com/acme-inc/*"
[inject]
settings_keys = ["permissions.deny"]
claude_local = "project/CLAUDE.local.md"
`)
	l, err := LoadLayer(good)
	if err != nil {
		t.Fatal(err)
	}
	if l.Inject.ClaudeLocal != "project/CLAUDE.local.md" {
		t.Errorf("unexpected %+v", l.Inject)
	}

	bad := t.TempDir()
	write(t, bad, "layer.toml", `
[layer]
name = "acme"
kind = "overlay"
[claude]
paths = ["CLAUDE.md"]
[identity]
name = "x"
email = "x@example.com"
[[match]]
remote = "github.com/x/*"
`)
	if _, err := LoadLayer(bad); err == nil || !strings.Contains(err.Error(), "[claude]") {
		t.Errorf("overlay with [claude] must fail, got %v", err)
	}

	base := t.TempDir()
	write(t, base, "layer.toml", `
[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "rules/"]
settings_keys = ["theme", "permissions.allow"]
`)
	if _, err := LoadLayer(base); err == nil || !strings.Contains(err.Error(), "permissions.allow") {
		t.Errorf("base owning permissions.allow must fail, got %v", err)
	}
}

func TestRemoveLayerDropsEveryReference(t *testing.T) {
	d := &Device{
		Layers: []string{"base", "acme"}, DefaultIdentity: "acme", TrustExec: []string{"acme"}, Roots: []string{"x"},
		Clone: map[string]string{"base": "b", "acme": "a"}, Repo: map[string]string{"acme": "u"}, Auth: map[string]string{"acme": "token"},
	}
	if !d.RemoveLayer("acme") {
		t.Error("default identity should be reported as cleared")
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("manifest invalid after removal: %v", err)
	}
	if len(d.TrustExec) != 0 || d.Auth["acme"] != "" || d.Repo["acme"] != "" {
		t.Errorf("references left: %+v", d)
	}
}

// A [[match]] remote lands in an includeIf header of the generated git
// configuration, and runtime names are compared without case because the
// file system may not distinguish them.
func TestLayerValidateRefusesInjectionAndCaseVariants(t *testing.T) {
	ov := Layer{Layer: LayerMeta{Name: "acme", Kind: "overlay"}, Identity: &Identity{Name: "A", Email: "a@acme.example"}}
	ov.Match = []Match{{Remote: "github.com/acme-inc/*"}}
	if err := ov.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"github.com/acme/*\"]\n[core]\n\thooksPath = /tmp", "github.com/acme/* ", "github.com/acme;x/*"} {
		ov.Match = []Match{{Remote: bad}}
		if err := ov.Validate(); err == nil {
			t.Errorf("remote %q must be refused", bad)
		}
	}
	for _, bad := range []string{"PROJECTS/", "Settings.json", "History.jsonl", "Plugins/x", "project\u017f/", "\u017fettings.json", "tas\u212as/", "projects./", "projects /x", "PROJEC~1/", "projects:x", "x/projects./y"} {
		base := Layer{Layer: LayerMeta{Name: "base", Kind: "base"}, Claude: &Claude{Paths: []string{bad}}}
		if err := base.Validate(); err == nil {
			t.Errorf("path %q must be refused whatever its case", bad)
		}
	}
}

// An overlay names one owner literally; a wildcard host or owner would let it
// claim other teams' repositories.
func TestMatchNamesHostAndOwner(t *testing.T) {
	ov := Layer{Layer: LayerMeta{Name: "acme", Kind: "overlay"}, Identity: &Identity{Name: "A", Email: "a@acme.example"}}
	for _, ok := range []string{"github.com/acme-inc/*", "git@github.com:acme-inc/api.git", "https://github.com/acme-inc/**", "gitlab.example/acme/sub/*"} {
		ov.Match = []Match{{Remote: ok}}
		if err := ov.Validate(); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"github.com/*", "**", "github.com/**", "*/acme-inc/*", "github.com", "github.com/acme-*/x", "git@github.com:*/x"} {
		ov.Match = []Match{{Remote: bad}}
		if err := ov.Validate(); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// A settings key path is checked for shape, so it cannot name the whole file.
func TestSettingsKeyShape(t *testing.T) {
	for _, bad := range []string{"", ".", "theme.", ".hooks", "a b", "hooks..x"} {
		l := Layer{Layer: LayerMeta{Name: "base", Kind: "base"}, Claude: &Claude{SettingsKeys: []string{bad}}}
		if err := l.Validate(); err == nil {
			t.Errorf("settings key %q must be refused", bad)
		}
	}
	l := Layer{Layer: LayerMeta{Name: "base", Kind: "base"}, Claude: &Claude{SettingsKeys: []string{"theme", "permissions.deny", "enabledPlugins.tools@acme-market"}}}
	if err := l.Validate(); err != nil {
		t.Error(err)
	}
}

// Single characters and listed exceptions never block the base layer.
func TestBlockedWords(t *testing.T) {
	d := Device{Blocklist: []string{"x", "acme", "AcmeNet", "tm"}, BlocklistExcept: []string{"acmenet"}}
	if got := strings.Join(d.BlockedWords(), ","); got != "acme,tm" {
		t.Errorf("BlockedWords = %s", got)
	}
}

func TestPrivateOnlyForBase(t *testing.T) {
	ov := Layer{Layer: LayerMeta{Name: "acme", Kind: "overlay", Private: true}, Identity: &Identity{Name: "A", Email: "a@acme.example"}, Match: []Match{{Remote: "github.com/acme-inc/*"}}}
	if ov.Validate() == nil {
		t.Error("an overlay cannot be marked private")
	}
	b := Layer{Layer: LayerMeta{Name: "base", Kind: "base", Private: true}, Claude: &Claude{}}
	if err := b.Validate(); err != nil {
		t.Error(err)
	}
}
