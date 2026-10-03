package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layer add puts one more overlay on a device that is already set up:
// manifest, clone and blocklist, ready for apply.
func TestLayerAdd(t *testing.T) {
	w, _ := applied(t)
	globex := w.bareRepo("globex", map[string]string{"layer.toml": "[layer]\nname = \"globex\"\nkind = \"overlay\"\n[identity]\nname = \"G\"\nemail = \"g@globex.example\"\n[[match]]\nremote = \"github.com/globex-inc/*\"\n"})
	out, code := w.run("", "layer", "add", "globex", globex, "--method", "none")
	if code != 0 || !strings.Contains(out, "layer globex added") {
		t.Fatalf("layer add: %d %s", code, out)
	}
	m := w.manifest()
	if !strings.Contains(m, `"globex"]`) || !strings.Contains(m, "globex.example") {
		t.Errorf("manifest lacks the layer or its blocklist words:\n%s", m)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".local", "share", "cclayer", "globex", "layer.toml")); err != nil {
		t.Errorf("globex not cloned: %v", err)
	}
	if out, code := w.run("", "status"); code != 0 || !strings.Contains(out, "globex:") {
		t.Errorf("status: %d %s", code, out)
	}
	// the same name again is refused
	if out, code := w.run("", "layer", "add", "globex", globex, "--method", "none"); code == 0 {
		t.Errorf("a second globex must be refused: %s", out)
	}
}

// A layer the device cannot take, here one claiming another overlay's owner,
// leaves the manifest as it was and no clone behind.
func TestLayerAddRollsBack(t *testing.T) {
	w, _ := applied(t)
	before := w.manifest()
	evil := w.bareRepo("evil", map[string]string{"layer.toml": "[layer]\nname = \"evil\"\nkind = \"overlay\"\n[identity]\nname = \"E\"\nemail = \"e@evil.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n"})
	out, code := w.run("", "layer", "add", "evil", evil, "--method", "none")
	if code == 0 || !strings.Contains(out, "both claim") {
		t.Fatalf("an overlay claiming acme-inc must be refused: %d %s", code, out)
	}
	if w.manifest() != before {
		t.Errorf("manifest changed:\n%s", w.manifest())
	}
	if _, err := os.Stat(filepath.Join(w.home, ".local", "share", "cclayer", "evil")); err == nil {
		t.Error("the clone of a refused layer was left behind")
	}
}
