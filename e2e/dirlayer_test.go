package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A directory layer is a plain directory on this machine (a cloud-synced
// folder works), with no git. setup accepts the directory, apply and capture
// read and write it in place, status and --pull call it a local directory,
// keys has nothing to do with it and leave does not delete it.
func TestDirectoryLayerLifecycle(t *testing.T) {
	w := newWorld(t)
	baseDir := filepath.Join(w.home, "Drive", "cclayer", "base")
	acmeDir := filepath.Join(w.home, "Drive", "cclayer", "acme")
	w.write("Drive/cclayer/base/layer.toml", "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\", \"rules/\"]\n")
	w.write("Drive/cclayer/base/claude/CLAUDE.md", "# from the drive\n")
	w.write("Drive/cclayer/acme/layer.toml", "[layer]\nname = \"acme\"\nkind = \"overlay\"\n[identity]\nname = \"Acme Me\"\nemail = \"me@acme.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n[inject]\nclaude_local = \"project/CLAUDE.local.md\"\n")
	w.write("Drive/cclayer/acme/project/CLAUDE.local.md", "acme rule\n")
	proj := w.project("acme-api", "git@github.com:acme-inc/api.git")

	// both layers are directories: no repository, so no credential question
	// overview rows once acme is added: base, overlay acme, add, project
	// dirs (4), default identity, auto pull, profiles, trusted layers,
	// language, save (10)
	answers := strings.Join([]string{
		"1", baseDir,
		"2", "acme", acmeDir,
		"4", filepath.Join(w.home, "Projects"),
		"10", // save
		"y",  // apply now; doctor follows
	}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 {
		t.Fatalf("setup: %s", out)
	}
	if strings.Contains(out, "Invalid") || strings.Contains(out, "reach the") {
		t.Errorf("prompt sequence off (a credential question for a directory layer?):\n%s", out)
	}
	m := w.manifest()
	if !strings.Contains(m, `base = "`+baseDir+`"`) || !strings.Contains(m, `acme = "`+acmeDir+`"`) {
		t.Errorf("clone paths must be the directories themselves:\n%s", m)
	}
	if strings.Contains(m, "[repo]") {
		t.Errorf("a directory layer has no [repo] entry:\n%s", m)
	}
	if w.read(".claude/CLAUDE.md") != "# from the drive\n" {
		t.Errorf("apply did not read the directory layer: %q", w.read(".claude/CLAUDE.md"))
	}
	if cl := w.read("Projects/acme-api/CLAUDE.local.md"); !strings.Contains(cl, "acme rule") {
		t.Errorf("overlay directory not injected: %q", cl)
	}

	// status and --pull say local directory instead of failing on git
	out, code = w.run("", "status")
	if code != 0 || !strings.Contains(out, "base: ~/Drive/cclayer/base, local directory") {
		t.Errorf("status: %d %s", code, out)
	}
	out, code = w.run("", "apply", "--pull")
	if code != 0 || !strings.Contains(out, "pull base: local directory, nothing to pull") {
		t.Errorf("apply --pull: %d %s", code, out)
	}

	// a change synced in from another machine is visible to apply at once
	w.write("Drive/cclayer/base/claude/CLAUDE.md", "# from the drive v2\n")
	out, code = w.run("", "apply")
	if code != 0 || w.read(".claude/CLAUDE.md") != "# from the drive v2\n" {
		t.Errorf("apply after a synced change: %d %s", code, out)
	}

	// a local edit is captured straight into the directory
	w.write(".claude/CLAUDE.md", "# edited here\n")
	out, code = w.run("", "capture")
	if code != 0 || w.read("Drive/cclayer/base/claude/CLAUDE.md") != "# edited here\n" {
		t.Errorf("capture: %d %s", code, out)
	}
	if !strings.Contains(out, "Written to the layers") {
		t.Errorf("capture report: %s", out)
	}

	// keys has nothing to set up for a directory layer
	out, code = w.run("", "keys", "setup", "acme", "--method", "token")
	if code == 0 || !strings.Contains(out, "needs no credentials") {
		t.Errorf("keys setup on a directory layer: %d %s", code, out)
	}

	// leave removes the injection but keeps the user's directory. The layer is
	// not a git repository; even inside a parent repository with uncommitted
	// changes (a dotfiles $HOME, say) that state must not block leave
	w.git(filepath.Join(w.home, "Drive"), "init", "-q")
	out, code = w.run("y\n", "leave", "acme")
	if code != 0 {
		t.Fatalf("leave: %s", out)
	}
	if !strings.Contains(out, "left in place") || strings.Contains(out, "revoke the git credential") {
		t.Errorf("leave output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(acmeDir, "layer.toml")); err != nil {
		t.Errorf("leave deleted the user's directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, "CLAUDE.local.md")); err == nil {
		t.Error("injected file still in the project after leave")
	}
	if m := w.manifest(); !strings.Contains(m, `layers = ["base"]`) || strings.Contains(m, `acme = "`) {
		t.Errorf("manifest still lists acme:\n%s", m)
	}
}

// A directory that does not exist yet gets a starter layer.toml after one
// confirmation; an overlay also asks for the identity and remote pattern.
// apply then writes nothing from the empty layer and capture --add fills it.
func TestDirectoryLayerStarter(t *testing.T) {
	w := newWorld(t)
	baseDir := filepath.Join(w.home, "Drive", "base")
	acmeDir := filepath.Join(w.home, "Drive", "acme")
	os.MkdirAll(filepath.Join(w.home, "Drive"), 0o755)
	w.write(".claude/CLAUDE.md", "# mine\n")
	w.write(".claude/rules/a.md", "rule a\n")

	answers := strings.Join([]string{
		"1", baseDir, // does not exist yet: a starter is written on save
		"2", "acme", acmeDir, // does not exist yet either
		"Acme Me", "me@acme.example", "github.com/acme-inc/*",
		"4", filepath.Join(w.home, "Projects"),
		"10", // save
		"y",  // apply now; doctor follows
	}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 {
		t.Fatalf("setup: %s", out)
	}
	if strings.Contains(out, "Invalid") {
		t.Errorf("an answer was rejected:\n%s", out)
	}
	for _, f := range []string{"Drive/base/layer.toml", "Drive/acme/layer.toml", "Drive/acme/project/CLAUDE.local.md"} {
		if w.read(f) == "" {
			t.Errorf("%s was not written; output:\n%s", f, out)
		}
	}
	if lt := w.read("Drive/acme/layer.toml"); !strings.Contains(lt, `email = "me@acme.example"`) || !strings.Contains(lt, `remote = "github.com/acme-inc/*"`) {
		t.Errorf("overlay starter:\n%s", lt)
	}
	// an empty base layer leaves the device files alone
	if w.read(".claude/CLAUDE.md") != "# mine\n" {
		t.Errorf("apply on an empty starter touched CLAUDE.md: %q", w.read(".claude/CLAUDE.md"))
	}
	// capture --add fills the new layer from the device
	out, code = w.run("", "capture", "--add", "CLAUDE.md", "--add", "rules/a.md")
	if code != 0 {
		t.Fatalf("capture --add: %s", out)
	}
	if w.read("Drive/base/claude/CLAUDE.md") != "# mine\n" || w.read("Drive/base/claude/rules/a.md") != "rule a\n" {
		t.Errorf("capture --add did not seed the layer:\n%s", out)
	}
	if strings.Contains(w.manifest(), "[repo]") {
		t.Errorf("starter layers are directory layers:\n%s", w.manifest())
	}
}

// setup refuses a directory that has other content but no layer.toml, and
// init reports clearly when the manifest points at such a directory.
func TestDirectoryLayerRefusals(t *testing.T) {
	w := newWorld(t)
	w.write("Notes/readme.txt", "not a layer\n")
	notes := filepath.Join(w.home, "Notes")
	// the rejected answer is followed by a valid one so setup can go on
	base := w.bareRepo("base", map[string]string{
		"layer.toml":       "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\"]\n",
		"claude/CLAUDE.md": "# shared\n",
	})
	answers := strings.Join([]string{
		"1", notes, base, "3", filepath.Join(w.home, "Projects"), "8", "n",
	}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 {
		t.Fatalf("setup: %s", out)
	}
	if !strings.Contains(out, "holds neither a layer.toml nor a git repository") {
		t.Errorf("the notes directory must be rejected:\n%s", out)
	}
	if !strings.Contains(w.manifest(), base) {
		t.Errorf("manifest should use the second answer:\n%s", w.manifest())
	}

	w.setManifest("layers = [\"base\"]\nroots = [\"" + filepath.Join(w.home, "Projects") + "\"]\n[clone]\nbase = \"" + notes + "\"\n")
	out, code = w.run("", "init")
	if code == 0 || !strings.Contains(out, "holds no layer.toml and [repo] has no URL") {
		t.Errorf("init with a clone path that is not a layer: %d %s", code, out)
	}
}

// A directory layer may hold a .git that came with the sync; cclayer never
// runs git in it, so settings there such as core.fsmonitor cannot run.
func TestDirectoryLayerGitIsNotRun(t *testing.T) {
	w := newWorld(t)
	baseDir := filepath.Join(w.home, "Drive", "base")
	w.write("Drive/base/layer.toml", "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\"]\n")
	w.write("Drive/base/claude/CLAUDE.md", "# shared\n")
	w.git(baseDir, "init", "-q")
	marker := filepath.Join(w.home, "fsmonitor-ran")
	w.git(baseDir, "config", "core.fsmonitor", "touch "+marker+" #")
	w.write(".config/cclayer/device.toml", "layers = [\"base\"]\nroots = [\""+filepath.Join(w.home, "Projects")+"\"]\n[clone]\nbase = \""+baseDir+"\"\n")
	for _, args := range [][]string{{"apply", "--pull"}, {"status"}, {"capture"}} {
		w.run("", args...)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("git ran inside a directory layer and executed its core.fsmonitor")
	}
}
