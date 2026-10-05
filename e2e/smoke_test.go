package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deviceState prepares a world in one of the device states S0..S3 the smoke
// cases run against.
func deviceState(t *testing.T, state string) (*world, seeded) {
	t.Helper()
	w := newWorld(t)
	var sd seeded
	switch state {
	case "S0":
		return w, sd
	case "S1":
		sd = seedStandard(w)
		return w, sd
	case "S2", "S3":
		sd = seedStandard(w)
		w.setManifest("trust_exec = [\"acme\"]\n" + w.manifest())
		if out, code := w.run("n\n", "init"); code != 0 {
			t.Fatalf("init: %s", out)
		}
		if state == "S3" {
			w.replaceManifest("profiles = false", "profiles = true")
		}
		if out, code := w.run("y\ny\ny\ny\ny\n", "apply"); code != 0 {
			t.Fatalf("apply: %s", out)
		}
		return w, sd
	}
	t.Fatalf("unknown state %s", state)
	return nil, sd
}

// noTempLeftovers asserts no half-written files survive a command.
func noTempLeftovers(t *testing.T, w *world) {
	t.Helper()
	filepath.Walk(w.home, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".tmp") && !strings.Contains(path, "/layers/") {
			t.Errorf("leftover temp file %s", path)
		}
		return nil
	})
}

type smokeCase struct {
	name     string
	args     []string
	stdin    string
	wantCode int
	wantOut  string // substring of combined output, "" to skip
	before   func() // fixture tweak the case needs
}

// githubURL swaps the overlay's local bare remote for a GitHub-shaped URL;
// deploy keys only exist for such repositories. Nothing is cloned or pulled
// afterwards in the smoke cases, so the swap is harmless.
func githubURL(w *world, sd seeded) func() {
	return func() { w.replaceManifest(sd.acme, "git@github.com:you/cclayer-acme.git") }
}

func TestSmoke(t *testing.T) {
	for _, state := range []string{"S0", "S1", "S2", "S3"} {
		state := state
		t.Run(state, func(t *testing.T) {
			w, sd := deviceState(t, state)
			proj := sd.proj
			if proj == "" {
				proj = w.home
			}
			var cases []smokeCase
			switch state {
			case "S0":
				cases = []smokeCase{
					{"init", []string{"init"}, "", 1, "base layer URL is required", nil},
					{"apply", []string{"apply"}, "", 1, "device manifest", nil},
					{"apply --hook", []string{"apply", "--hook"}, "", 0, "cclayer hook", nil},
					{"capture", []string{"capture"}, "", 1, "", nil},
					{"check", []string{"check"}, "", 1, "", nil},
					{"status", []string{"status"}, "", 1, "", nil},
					{"leave", []string{"leave", "acme"}, "", 1, "", nil},
					{"doctor", []string{"doctor"}, "", 0, "cannot load layers", nil},
					{"keys list", []string{"keys", "list"}, "", 1, "", nil},
					{"keys setup", []string{"keys", "setup", "acme", "--method", "deploy-key"}, "", 1, "", nil},
					{"keys remove", []string{"keys", "remove", "acme"}, "", 1, "", nil},
					{"env", []string{"env"}, "", 1, "", nil},
					{"run", []string{"run", "--", "true"}, "", 1, "", nil},
					{"help", []string{"help"}, "", 0, "Usage:", nil},
					{"version", []string{"version"}, "", 0, "cclayer", nil},
				}
			case "S1":
				cases = []smokeCase{
					{"setup", []string{"setup", "--accessible"}, "12\n", 0, "nothing saved", nil},
					{"apply", []string{"apply"}, "", 1, "cclayer init", nil},
					{"apply --hook", []string{"apply", "--hook"}, "", 0, "cclayer hook", nil},
					{"capture", []string{"capture"}, "", 1, "", nil},
					{"check", []string{"check"}, "", 1, "", nil},
					{"status", []string{"status"}, "", 1, "", nil},
					{"leave", []string{"leave", "acme"}, "", 1, "", nil},
					{"doctor", []string{"doctor"}, "", 0, "warn", nil},
					{"keys list", []string{"keys", "list"}, "", 0, "git default", nil},
					{"env", []string{"env"}, "", 1, "cclayer init", nil},
					{"run", []string{"run", "--", "true"}, "", 1, "cclayer init", nil},
					{"help", []string{"help"}, "", 0, "Usage:", nil},
					{"version", []string{"version"}, "", 0, "cclayer", nil},
					{"init", []string{"init"}, "n\n", 0, "init done", nil},
					// after the clone exists the overlay URL can be swapped for a GitHub-shaped one
					{"keys setup", []string{"keys", "setup", "acme", "--method", "deploy-key"}, "", 0, "cclayer-acme", githubURL(w, sd)},
					{"keys remove", []string{"keys", "remove", "acme"}, "", 0, "Revoke", nil},
				}
			case "S2":
				cases = []smokeCase{
					{"setup", []string{"setup", "--accessible"}, "12\n", 0, "nothing saved", nil},
					{"init", []string{"init"}, "", 0, "init done", nil},
					{"apply", []string{"apply"}, "", 0, "Nothing to do", nil},
					{"apply --hook", []string{"apply", "--hook"}, "", 0, "", nil},
					{"capture", []string{"capture"}, "", 0, "no local changes", nil},
					{"check", []string{"check"}, "", 0, "nothing to refuse", nil},
					{"status", []string{"status"}, "", 0, "projects of acme", nil},
					{"doctor", []string{"doctor"}, "", 0, "include block is in place", nil},
					{"keys list", []string{"keys", "list"}, "", 0, "git default", nil},
					{"keys setup", []string{"keys", "setup", "acme", "--method", "deploy-key"}, "", 0, "cclayer-acme", githubURL(w, sd)},
					{"keys remove", []string{"keys", "remove", "acme"}, "", 0, "Revoke", nil},
					{"env", []string{"env", proj}, "", 1, "profiles are off", nil},
					{"run", []string{"run", proj, "--", "true"}, "", 1, "profiles are off", nil},
					{"help", []string{"help"}, "", 0, "Usage:", nil},
					{"version", []string{"version"}, "", 0, "cclayer", nil},
					{"leave declined", []string{"leave", "acme"}, "n\n", 1, "cancelled", nil},
					{"leave", []string{"leave", "acme"}, "y\n", 0, "removed from this device", nil},
				}
			case "S3":
				cases = []smokeCase{
					{"setup", []string{"setup", "--accessible"}, "12\n", 0, "nothing saved", nil},
					{"init", []string{"init"}, "", 0, "init done", nil},
					{"apply", []string{"apply"}, "", 0, "Nothing to do", nil},
					{"apply --hook", []string{"apply", "--hook"}, "", 0, "", nil},
					{"capture", []string{"capture"}, "", 0, "no local changes", nil},
					{"check", []string{"check"}, "", 0, "nothing to refuse", nil},
					{"status", []string{"status"}, "", 0, "projects of acme", nil},
					{"doctor", []string{"doctor"}, "", 0, "include block is in place", nil},
					{"keys list", []string{"keys", "list"}, "", 0, "git default", nil},
					{"keys setup", []string{"keys", "setup", "acme", "--method", "deploy-key"}, "", 0, "cclayer-acme", githubURL(w, sd)},
					{"keys remove", []string{"keys", "remove", "acme"}, "", 0, "Revoke", nil},
					{"env matched", []string{"env", proj}, "", 0, "export CLAUDE_CONFIG_DIR=", nil},
					{"env unmatched", []string{"env", sd.mine}, "", 0, "unset CLAUDE_CONFIG_DIR", nil},
					{"run", []string{"run", proj, "--", "sh", "-c", "exit 3"}, "", 3, "", nil},
					{"help", []string{"help"}, "", 0, "Usage:", nil},
					{"version", []string{"version"}, "", 0, "cclayer", nil},
					{"leave", []string{"leave", "acme"}, "y\n", 0, "removed", nil},
				}
			}
			for _, c := range cases {
				if c.before != nil {
					c.before()
				}
				out, code := w.run(c.stdin, c.args...)
				if code != c.wantCode {
					t.Errorf("%s/%s: exit %d, want %d\n%s", state, c.name, code, c.wantCode, out)
				}
				if c.wantOut != "" && !strings.Contains(out, c.wantOut) {
					t.Errorf("%s/%s: output lacks %q\n%s", state, c.name, c.wantOut, out)
				}
				noTempLeftovers(t, w)
			}
			if state == "S3" {
				if _, err := os.Stat(filepath.Join(w.home, ".claude-profiles", "acme")); !os.IsNotExist(err) {
					t.Error("profile directory must go with the layer")
				}
				if _, err := os.Stat(filepath.Join(w.home, ".claude-profiles", "acme", "settings.json")); err == nil {
					t.Error("profile settings must be gone")
				}
			}
		})
	}
}

// S0 setup is the only command that does real work without a manifest.
func TestSmokeSetupFromNothing(t *testing.T) {
	w := newWorld(t)
	base := w.bareRepo("base", map[string]string{
		"layer.toml":       "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\"]\n",
		"claude/CLAUDE.md": "# shared\n",
	})
	// overview: base layer, project dirs, save; then apply
	answers := strings.Join([]string{"1", base, "3", filepath.Join(w.home, "Projects"), "9", "y"}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 || w.read(".claude/CLAUDE.md") != "# shared\n" {
		t.Fatalf("setup from nothing: %d %s", code, out)
	}
	noTempLeftovers(t, w)
}
