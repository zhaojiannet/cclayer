package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func warns(t *testing.T, w *world, want string) {
	t.Helper()
	out, code := w.run("", "doctor")
	if code != 0 || !strings.Contains(out, "warn") || !strings.Contains(out, want) {
		t.Errorf("doctor should warn about %q:\n%s", want, out)
	}
}

func TestDoctorWarnsClaudeDirIsRepo(t *testing.T) {
	w, _ := applied(t)
	w.git(filepath.Join(w.home, ".claude"), "init", "-q")
	warns(t, w, "#80304")
}

func TestDoctorWarnsSymlinkedSettings(t *testing.T) {
	w, _ := applied(t)
	real := filepath.Join(w.home, "settings-elsewhere.json")
	os.Rename(filepath.Join(w.home, ".claude", "settings.json"), real)
	os.Symlink(real, filepath.Join(w.home, ".claude", "settings.json"))
	warns(t, w, "symlink")
}

func TestDoctorWarnsIncludeBlockNotLast(t *testing.T) {
	w, _ := applied(t)
	w.write(".gitconfig", w.read(".gitconfig")+"[core]\n\tautocrlf = input\n")
	warns(t, w, "not at the end")
	w.run("", "apply")
	gc := w.read(".gitconfig")
	if !strings.HasSuffix(strings.TrimSpace(gc), "path = ~/.gitconfig.cclayer") || !strings.Contains(gc, "autocrlf") {
		t.Errorf("apply must move the block back and keep the user's section:\n%s", gc)
	}
}

func TestDoctorWarnsMissingExcludes(t *testing.T) {
	w, _ := applied(t)
	os.Remove(filepath.Join(w.home, ".config", "git", "ignore"))
	warns(t, w, "lacks")
}

func TestDoctorWarnsIdentityMismatch(t *testing.T) {
	w, sd := applied(t)
	w.git(sd.proj, "config", "user.email", "wrong@example.com")
	warns(t, w, "layer expects me@acme.example")
}

func TestDoctorWarnsConfigDirDefault(t *testing.T) {
	w, _ := applied(t)
	// the shim env has no CLAUDE_CONFIG_DIR; run doctor with it set to the default path
	out, _ := w.runEnv([]string{"CLAUDE_CONFIG_DIR=" + filepath.Join(w.home, ".claude")}, "", "doctor")
	if !strings.Contains(out, "#92252") {
		t.Errorf("doctor should warn about the default CLAUDE_CONFIG_DIR:\n%s", out)
	}
}

func TestStatusViews(t *testing.T) {
	w, sd := applied(t)
	// gone, unmatched and conflicting projects at once
	gone := w.project("acme-old", "git@github.com:acme-inc/old.git")
	w.run("", "apply")
	os.RemoveAll(gone)
	globex := w.bareRepo("globex", map[string]string{"layer.toml": "[layer]\nname = \"globex\"\nkind = \"overlay\"\n[identity]\nname = \"G\"\nemail = \"g@globex.example\"\n[[match]]\nremote = \"github.com/me/*\"\n"})
	m := strings.Replace(w.manifest(), `layers = ["base", "acme"]`, `layers = ["base", "acme", "globex"]`, 1)
	m += "\n[clone]\nglobex = \"" + filepath.Join(w.home, "layers", "globex") + "\"\n[repo]\nglobex = \"" + globex + "\"\n"
	w.setManifest(mergeTables(m))
	w.run("n\n", "init")
	w.git(sd.mine, "remote", "add", "upstream", "https://github.com/acme-inc/mine.git")
	w.project("lonely", "https://github.com/nobody/x.git")
	out, code := w.run("", "status")
	if code != 0 {
		t.Fatalf("status: %s", out)
	}
	for _, want := range []string{"projects of acme", "acme-api", "projects no overlay matches", "lonely", "matching more than one overlay", "mine", "directory is gone", "acme-old"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
}

// CLAUDE_CONFIG_DIR naming an overlay's profile is fine; anywhere else
// cclayer does not write, and doctor says so.
func TestDoctorConfigDirProfile(t *testing.T) {
	w, _ := deviceState(t, "S3")
	out, _ := w.runEnv([]string{"CLAUDE_CONFIG_DIR=" + filepath.Join(w.home, ".claude-profiles", "acme")}, "", "doctor")
	if !strings.Contains(out, "selects the profile of layer acme") {
		t.Errorf("profile directory not recognized:\n%s", out)
	}
	out, _ = w.runEnv([]string{"CLAUDE_CONFIG_DIR=" + filepath.Join(w.home, "elsewhere")}, "", "doctor")
	if !strings.Contains(out, "which cclayer does not write") {
		t.Errorf("an unknown CLAUDE_CONFIG_DIR must be flagged:\n%s", out)
	}
}
