package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

func TestManifestRejectsEscapingPaths(t *testing.T) {
	for _, p := range []string{"../evil/", "/etc/x", "projects/", ".credentials.json", "settings.json", "rules/../../x"} {
		l := &manifest.Layer{Layer: manifest.LayerMeta{Name: "base", Kind: "base"}, Claude: &manifest.Claude{Paths: []string{p}}}
		if err := l.Validate(); err == nil {
			t.Errorf("path %q must be rejected", p)
		}
	}
	l := &manifest.Layer{Layer: manifest.LayerMeta{Name: "base", Kind: "base"}, Claude: &manifest.Claude{Paths: []string{"CLAUDE.md", "rules/", "skills/"}}}
	if err := l.Validate(); err != nil {
		t.Errorf("normal paths rejected: %v", err)
	}
}

func TestIgnoreIsScopedToMirroredPath(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, ".claude", "rules", ".trash", "old.md"), "junk\n")
	Run(e, []string{"apply"})
	if out := out(e); strings.Contains(out, ".trash") {
		t.Errorf("ignored file must not be reported:\n%s", out)
	}
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"capture"})
	if out := out(e); strings.Contains(out, ".trash") {
		t.Errorf("ignored file must not be a capture candidate:\n%s", out)
	}
}

func TestProjectMapSurvivesDirectoryDeletion(t *testing.T) {
	e, home := fixture(t)
	Run(e, []string{"apply"})
	proj := filepath.Join(home, "Projects", "acme-api")
	os.RemoveAll(proj)
	Run(e, []string{"apply"})
	applied, _ := e.State.Load()
	if applied.Projects[proj] != "acme" {
		t.Fatalf("deleted project must stay in the map for leave, got %v", applied.Projects)
	}
	rr := &recRunner{}
	e.Runner = rr
	old := claudeAvailable
	claudeAvailable = func() bool { return true }
	defer func() { claudeAvailable = old }()
	e.Stdin = strings.NewReader("y\n")
	e.stdin = nil
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"leave", "acme"}); code != 0 {
		t.Fatalf("leave: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if len(rr.calls) != 1 || !strings.HasSuffix(rr.calls[0], proj) {
		t.Errorf("gone project must still be purged: %v", rr.calls)
	}
}

func TestLocalDeleteIsNotRecreated(t *testing.T) {
	e, home := fixture(t)
	Run(e, []string{"apply"})
	target := filepath.Join(home, ".claude", "rules", "comments.md")
	os.Remove(target)
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"apply"})
	if _, err := os.Stat(target); err == nil {
		t.Error("a file deleted on the device must not come back while the layer is unchanged")
	}
	if !strings.Contains(out(e), "comments.md") {
		t.Errorf("the deletion must be reported:\n%s", out(e))
	}
}

func TestCaptureDoesNotRevertUnappliedLayerChange(t *testing.T) {
	e, home := fixture(t)
	Run(e, []string{"apply"})
	// simulate a pull: the layer moves on, the device file is untouched
	write(t, filepath.Join(home, "layers", "base", "claude", "CLAUDE.md"), "# layer v2\n")
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"capture"})
	if b, _ := os.ReadFile(filepath.Join(home, "layers", "base", "claude", "CLAUDE.md")); string(b) != "# layer v2\n" {
		t.Error("capture reverted the layer's newer version")
	}
	// device edited too: conflict, still not written
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# device edit\n")
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"capture"})
	if b, _ := os.ReadFile(filepath.Join(home, "layers", "base", "claude", "CLAUDE.md")); string(b) != "# layer v2\n" {
		t.Error("conflicting capture must not overwrite the layer")
	}
	if !strings.Contains(out(e), "Changed both here and in the layer") {
		t.Errorf("conflict must be reported:\n%s", out(e))
	}
}

func TestApplyRefusesLayerThatFailsCheck(t *testing.T) {
	e, home := fixture(t)
	write(t, filepath.Join(home, "layers", "base", "claude", "rules", "leak.md"), "token = ghp_"+strings.Repeat("a", 36)+"\n")
	if code := Run(e, []string{"apply"}); code == 0 {
		t.Fatal("apply must refuse a layer with a secret")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
		t.Error("nothing may be applied when check fails")
	}
}

// --from may spell a project through a symlink (macOS keeps temporary and
// some user directories behind one) while discovery saw the real path.
func TestFilterDirsFollowsSymlinks(t *testing.T) {
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available")
	}
	if got := filterDirs([]string{real}, link); len(got) != 1 || got[0] != real {
		t.Errorf("link must select the real entry, got %v", got)
	}
	if got := filterDirs([]string{link}, real); len(got) != 1 || got[0] != link {
		t.Errorf("real path must select the linked entry as discovered, got %v", got)
	}
	if got := filterDirs([]string{real}, filepath.Join(tmp, "other")); got != nil {
		t.Errorf("unrelated path must not match, got %v", got)
	}
}
