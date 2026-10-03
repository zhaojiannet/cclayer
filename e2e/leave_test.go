package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLeaveGoneDirectoryPurged(t *testing.T) {
	w, sd := applied(t)
	os.RemoveAll(sd.proj)
	w.run("", "apply")
	out, code := w.run("y\n", "leave", "acme")
	if code != 0 || !strings.Contains(w.read("shim-calls.log"), "purge --yes "+sd.proj) {
		t.Fatalf("gone project must still be purged: %d %s\n%s", code, out, w.read("shim-calls.log"))
	}
}

func TestLeaveUnknownStatePathOffered(t *testing.T) {
	w, sd := applied(t)
	stray := filepath.Join(w.home, "Projects", "stray")
	w.write(".claude.json", `{"projects": {"`+stray+`": {}, "/elsewhere/x": {}}}`)
	out, code := w.run("y\ny\n", "leave", "acme")
	if code != 0 || !strings.Contains(out, "not known to belong to any layer") {
		t.Fatalf("unknown path must be offered: %d %s", code, out)
	}
	log := w.read("shim-calls.log")
	if !strings.Contains(log, "purge --yes "+stray) || strings.Contains(log, "/elsewhere/x") {
		t.Errorf("only paths under the roots are offered and purged on yes:\n%s", log)
	}
	_ = sd
}

func TestLeaveClearsDefaultIdentity(t *testing.T) {
	w, _ := applied(t)
	w.setManifest("default_identity = \"acme\"\n" + strings.Replace(w.manifest(), "default_identity = \"\"\n", "", 1))
	w.run("", "apply")
	if !strings.Contains(w.read(".gitconfig.cclayer"), "me@acme.example") {
		t.Fatal("default identity not applied")
	}
	out, code := w.run("y\n", "leave", "acme")
	if code != 0 || !strings.Contains(out, "default_identity named this layer") {
		t.Fatalf("leave: %d %s", code, out)
	}
	if !strings.Contains(w.read(".gitconfig.cclayer"), "useConfigOnly = true") || strings.Contains(w.manifest(), `default_identity = "acme"`) {
		t.Error("default identity not cleared")
	}
}

func TestLeaveRemovesCredentialsAndHints(t *testing.T) {
	w, sd := applied(t)
	w.replaceManifest(sd.acme, "git@github.com:you/cclayer-acme.git")
	w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	w.write("layers/acme/project/settings.local.json", `{"permissions": {"deny": ["Bash(rm -rf *)"]}, "enabledPlugins": {"tools@acme-market": true}, "extraKnownMarketplaces": {"acme-market": {"source": {"source": "github", "repo": "acme-inc/market"}}}}`)
	out, code := w.run("y\n", "leave", "--force", "acme")
	if code != 0 {
		t.Fatalf("leave: %s", out)
	}
	if w.read(".ssh/cclayer-acme") != "" || strings.Contains(w.read(".ssh/config"), "cclayer-acme") {
		t.Error("credentials survived leave")
	}
	for _, want := range []string{"Revoke the deploy key", "claude plugin marketplace remove", "revoke the git credential"} {
		if !strings.Contains(out, want) {
			t.Errorf("hint missing %q:\n%s", want, out)
		}
	}
}

func TestLeaveCancelledChangesNothing(t *testing.T) {
	w, sd := applied(t)
	before := w.manifest()
	out, code := w.run("n\n", "leave", "acme")
	if code == 0 || !strings.Contains(out, "cancelled") {
		t.Fatalf("cancel: %d %s", code, out)
	}
	if w.manifest() != before || w.read("Projects/acme-api/CLAUDE.local.md") == "" {
		t.Error("cancelled leave changed something")
	}
	if _, err := os.Stat(filepath.Join(w.home, "layers", "acme")); err != nil {
		t.Error("clone removed on cancel")
	}
	_ = sd
}

func TestLeaveBaseRefused(t *testing.T) {
	w, _ := applied(t)
	if out, code := w.run("y\n", "leave", "base"); code != 2 || !strings.Contains(out, "cannot be left") {
		t.Errorf("leave base: %d %s", code, out)
	}
}
