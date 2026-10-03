package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// URLs and scp addresses are cloned, a directory holding layer.toml is used
// in place, a bare repository is cloned, a missing or empty directory may get
// a starter, any other directory is refused.
func TestClassifyLocation(t *testing.T) {
	tmp := t.TempDir()
	layerDir := filepath.Join(tmp, "layer")
	os.MkdirAll(layerDir, 0o755)
	os.WriteFile(filepath.Join(layerDir, "layer.toml"), []byte("x"), 0o644)
	bare := filepath.Join(tmp, "bare.git")
	os.MkdirAll(bare, 0o755)
	os.WriteFile(filepath.Join(bare, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	empty := filepath.Join(tmp, "empty")
	os.MkdirAll(empty, 0o755)
	other := filepath.Join(tmp, "other")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "notes.txt"), []byte("x"), 0o644)
	file := filepath.Join(tmp, "file")
	os.WriteFile(file, []byte("x"), 0o644)

	cases := []struct {
		in   string
		want locationKind
		err  bool
	}{
		{"https://github.com/you/base.git", locURL, false},
		{"git@github.com:you/base.git", locURL, false},
		{"ssh://git@host/you/base.git", locURL, false},
		{layerDir, locLayerDir, false},
		{"file://" + layerDir, locLayerDir, false},
		{bare, locGitDir, false},
		{empty, locNew, false},
		{filepath.Join(tmp, "fresh"), locNew, false},
		{filepath.Join(tmp, "no", "parent"), locNew, false},
		{filepath.Join(file, "below"), locBad, true},
		{other, locBad, true},
		{file, locBad, true},
		{"", locBad, true},
	}
	for _, c := range cases {
		got, err := classifyLocation(c.in)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("classifyLocation(%q) = %v, %v; want %v, err=%v", c.in, got, err, c.want, c.err)
		}
	}
}

// A starter must load as written and never replace an existing layer.toml.
func TestStarterLayers(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "base")
	if err := starterBase(base); err != nil {
		t.Fatal(err)
	}
	l, err := manifest.LoadLayer(base)
	if err != nil || l.Layer.Kind != "base" || len(l.Claude.Paths) == 0 {
		t.Fatalf("base starter: %v %+v", err, l)
	}
	if err := starterBase(base); err == nil {
		t.Error("a second starterBase must not replace the existing layer.toml")
	}

	ov := filepath.Join(tmp, "acme")
	if err := starterOverlay(ov, "acme", "Full Name", "me@acme.example", "github.com/acme-inc/*"); err != nil {
		t.Fatal(err)
	}
	l, err = manifest.LoadLayer(ov)
	if err != nil || l.Identity.Email != "me@acme.example" || l.Match[0].Remote != "github.com/acme-inc/*" || l.Inject.ClaudeLocal != "project/CLAUDE.local.md" {
		t.Fatalf("overlay starter: %v %+v", err, l)
	}
	for _, f := range []string{"project/settings.local.json", "project/CLAUDE.local.md"} {
		if _, err := os.Stat(filepath.Join(ov, f)); err != nil {
			t.Errorf("%s missing: %v", f, err)
		}
	}
}

// Local paths are normalized before they enter the manifest: relative becomes
// absolute, ~ is kept; only a remote URL can need credentials.
func TestLocalPathAndRemoteURL(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	if got := localPath("cclayer/base"); got != filepath.Join(tmp, "cclayer", "base") {
		t.Errorf("relative: %q", got)
	}
	if got := localPath("file://" + tmp); got != tmp {
		t.Errorf("file://: %q", got)
	}
	if got := localPath("~/Drive/base"); got != "~/Drive/base" {
		t.Errorf("tilde must be kept: %q", got)
	}
	for in, want := range map[string]bool{
		"https://github.com/you/base.git": true,
		"git@github.com:you/base.git":     true,
		"file:///srv/base.git":            false,
		"/srv/base.git":                   false,
		"~/layers/base.git":               false,
		"":                                false,
	} {
		if isRemoteURL(in) != want {
			t.Errorf("isRemoteURL(%q) = %v", in, !want)
		}
	}
}

// A starter overlay's identity follows the LoadLayer rules; a refused one
// must not leave a half-written layer.toml behind.
func TestStarterOverlayRefusesBadIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	if err := starterOverlay(dir, "acme", "Full Name", "me at acme", "github.com/acme-inc/*"); err == nil {
		t.Fatal("an email without @ must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "layer.toml")); err == nil {
		t.Error("a refused starter left layer.toml behind")
	}
	if manifest.CheckIdentityEmail("me@acme.example") != nil || manifest.CheckIdentityEmail("a@b@c") == nil || manifest.CheckIdentityName("Full Name") != nil || manifest.CheckIdentityName(`a"b`) == nil {
		t.Error("identity rules differ from LoadLayer")
	}
}

// Changing a layer's source drops its old credential, stores a directory as an
// absolute path and a bare repository as the path git must see.
func TestSetLocationDropsCredentialAndNormalizes(t *testing.T) {
	e, home := fixture(t)
	t.Chdir(home)
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		t.Fatal(err)
	}
	d.EnsureMaps()
	d.Auth["acme"] = "token"
	d.Repo["acme"] = "https://github.com/you/cclayer-acme.git"
	w := &wizard{e: e}

	layerDir := filepath.Join(home, "Drive", "acme")
	os.MkdirAll(layerDir, 0o755)
	os.WriteFile(filepath.Join(layerDir, "layer.toml"), []byte("[layer]\nname = \"acme\"\nkind = \"overlay\"\n[identity]\nname = \"A\"\nemail = \"a@acme.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n"), 0o644)
	if err := w.setLocation(d, &layerDraft{name: "acme", loc: "Drive/acme"}); err != nil {
		t.Fatal(err)
	}
	if d.Clone["acme"] != layerDir || d.Repo["acme"] != "" || d.Auth["acme"] != "" {
		t.Errorf("directory layer: clone=%q repo=%q auth=%q", d.Clone["acme"], d.Repo["acme"], d.Auth["acme"])
	}
	if !strings.Contains(out(e), "Revoke the token") {
		t.Errorf("dropping a token must say what to revoke:\n%s", out(e))
	}

	bare := filepath.Join(home, "bare.git")
	os.MkdirAll(bare, 0o755)
	os.WriteFile(filepath.Join(bare, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	if err := w.setLocation(d, &layerDraft{name: "acme", loc: "bare.git"}); err != nil {
		t.Fatal(err)
	}
	if d.Repo["acme"] != bare || d.Clone["acme"] != "~/.local/share/cclayer/acme" {
		t.Errorf("bare repository: repo=%q clone=%q", d.Repo["acme"], d.Clone["acme"])
	}
}
