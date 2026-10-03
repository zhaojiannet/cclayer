package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// applied builds the standard device, trusts acme and runs the first apply.
func applied(t *testing.T) (*world, seeded) {
	t.Helper()
	w, sd := deviceState(t, "S2")
	return w, sd
}

func TestApplyHooksKeyGate(t *testing.T) {
	w := newWorld(t)
	sd := seedStandard(w)
	w.setManifest("trust_exec = [\"acme\"]\n" + w.manifest())
	w.run("n\n", "init")
	// accept the skills, decline the hooks: theme still applied, hooks absent
	out, code := w.run("y\nn\ny\ny\ny\n", "apply")
	if code != 0 {
		t.Fatalf("apply: %s", out)
	}
	set := w.read(".claude/settings.json")
	if strings.Contains(set, "cclayer apply --hook") || !strings.Contains(set, `"dark"`) {
		t.Fatalf("declined hooks must not land, other keys must:\n%s", set)
	}
	if !strings.Contains(out, "hooks change declined") {
		t.Errorf("report must say hooks were declined:\n%s", out)
	}
	// --hook mode never applies a hooks change
	w.replaceManifest("auto_pull = false", "auto_pull = true")
	out, _ = w.run("", "apply", "--hook")
	if strings.Contains(w.read(".claude/settings.json"), "cclayer apply --hook") || !strings.Contains(out, "hooks changed") {
		t.Errorf("hook mode applied hooks or kept quiet:\n%s", out)
	}
	// accepted interactively
	out, code = w.run("y\n", "apply")
	if code != 0 || !strings.Contains(w.read(".claude/settings.json"), "cclayer apply --hook") {
		t.Fatalf("accepted hooks not applied: %d %s", code, out)
	}
	_ = sd
}

func TestApplyHookScriptAskedOnce(t *testing.T) {
	w, sd := applied(t)
	// a second overlay and profiles on
	globex := w.bareRepo("globex", map[string]string{"layer.toml": "[layer]\nname = \"globex\"\nkind = \"overlay\"\n[identity]\nname = \"G\"\nemail = \"g@globex.example\"\n[[match]]\nremote = \"github.com/globex-inc/*\"\n"})
	m := w.manifest()
	m = strings.Replace(m, `layers = ["base", "acme"]`, `layers = ["base", "acme", "globex"]`, 1)
	m = strings.Replace(m, "profiles = false", "profiles = true", 1)
	m += "\n[clone]\nglobex = \"" + filepath.Join(w.home, "layers", "globex") + "\"\n[repo]\nglobex = \"" + globex + "\"\n"
	// TOML forbids a table defined twice; rebuild the file with merged tables instead
	m = mergeTables(m)
	w.setManifest(m)
	if out, code := w.run("n\n", "init"); code != 0 {
		t.Fatalf("init: %s", out)
	}
	w.pushToRemote("base", "claude/hooks/notify.sh", "#!/bin/sh\necho hi\n")
	w.pushToRemote("base", "layer.toml", strings.Replace(w.read("seed/base/layer.toml"), `paths = ["CLAUDE.md", "rules/", "skills/"]`, `paths = ["CLAUDE.md", "rules/", "skills/", "hooks/"]`, 1))
	out, code := w.run("y\n", "apply", "--pull")
	if code != 0 {
		t.Fatalf("apply: %s", out)
	}
	if n := strings.Count(out, "Apply these"); n != 1 {
		t.Errorf("hook script must be confirmed once, asked %d times:\n%s", n, out)
	}
	for _, p := range []string{".claude/hooks/notify.sh", ".claude-profiles/acme/hooks/notify.sh", ".claude-profiles/globex/hooks/notify.sh"} {
		if w.read(p) == "" {
			t.Errorf("%s missing after one yes", p)
		}
	}
	_ = sd
}

// mergeTables folds duplicate [clone]/[repo] tables appended to a manifest
// into the first occurrence, which is what TOML requires.
func mergeTables(m string) string {
	lines := strings.Split(m, "\n")
	var top []string
	tables := map[string][]string{}
	var order []string
	cur := ""
	for _, l := range lines {
		tl := strings.TrimSpace(l)
		if strings.HasPrefix(tl, "[") && strings.HasSuffix(tl, "]") {
			cur = tl
			if _, ok := tables[cur]; !ok {
				order = append(order, cur)
			}
			continue
		}
		if cur == "" {
			top = append(top, l)
		} else if tl != "" {
			tables[cur] = append(tables[cur], l)
		}
	}
	out := strings.Join(top, "\n")
	for _, tname := range order {
		out += "\n" + tname + "\n" + strings.Join(tables[tname], "\n") + "\n"
	}
	return out
}

func TestApplyConflictHandling(t *testing.T) {
	w, _ := applied(t)
	w.write(".claude/CLAUDE.md", "# edited here\n")
	w.pushToRemote("base", "claude/CLAUDE.md", "# layer v2\n")
	w.replaceManifest("auto_pull = false", "auto_pull = true")
	out, _ := w.run("", "apply", "--hook")
	if w.read(".claude/CLAUDE.md") != "# edited here\n" || !strings.Contains(out, "conflicts") {
		t.Fatalf("hook must leave the conflict alone and name it:\n%s", out)
	}
	w.run("n\n", "apply")
	if w.read(".claude/CLAUDE.md") != "# edited here\n" {
		t.Fatal("declined conflict was overwritten")
	}
	w.run("y\n", "apply")
	if w.read(".claude/CLAUDE.md") != "# layer v2\n" {
		t.Fatal("accepted conflict was not resolved")
	}
}

func TestApplyLocalDeleteStays(t *testing.T) {
	w, _ := applied(t)
	os.Remove(filepath.Join(w.home, ".claude", "rules", "comments.md"))
	out, _ := w.run("", "apply")
	if _, err := os.Stat(filepath.Join(w.home, ".claude", "rules", "comments.md")); err == nil {
		t.Fatal("deleted file came back")
	}
	if !strings.Contains(out, "comments.md") {
		t.Errorf("deletion not reported:\n%s", out)
	}
}

func TestApplyIgnoreScoped(t *testing.T) {
	w, _ := applied(t)
	w.write(".claude/skills/.trash/old.md", "junk\n")
	out, _ := w.run("", "apply")
	if strings.Contains(out, ".trash") {
		t.Errorf("ignored path reported:\n%s", out)
	}
	if w.read(".claude/skills/.trash/old.md") == "" {
		t.Error("ignored file deleted")
	}
	out, _ = w.run("", "capture")
	if strings.Contains(out, ".trash") {
		t.Errorf("ignored path offered by capture:\n%s", out)
	}
}

func TestApplyProjectStopsMatching(t *testing.T) {
	w, sd := applied(t)
	w.git(sd.proj, "remote", "set-url", "origin", "https://github.com/me/not-acme.git")
	out, code := w.run("", "apply")
	if code != 0 || !strings.Contains(out, "no longer matches") {
		t.Fatalf("apply: %d %s", code, out)
	}
	loc := w.read("Projects/acme-api/.claude/settings.local.json")
	if strings.Contains(loc, "rm -rf") || !strings.Contains(loc, "Bash(ls)") {
		t.Errorf("injected keys must go, approvals stay:\n%s", loc)
	}
	var st struct {
		Projects map[string]string `json:"projects"`
	}
	json.Unmarshal([]byte(w.read(".local/state/cclayer/applied.json")), &st)
	if _, ok := st.Projects[sd.proj]; ok {
		t.Error("project still in the map")
	}
}

func TestApplyForkConflict(t *testing.T) {
	w, sd := applied(t)
	// a second overlay claims the personal account, so a fork with
	// origin=me and upstream=acme matches both
	globex := w.bareRepo("globex", map[string]string{"layer.toml": "[layer]\nname = \"globex\"\nkind = \"overlay\"\n[identity]\nname = \"G\"\nemail = \"g@globex.example\"\n[[match]]\nremote = \"github.com/me/*\"\n"})
	m := strings.Replace(w.manifest(), `layers = ["base", "acme"]`, `layers = ["base", "acme", "globex"]`, 1)
	m += "\n[clone]\nglobex = \"" + filepath.Join(w.home, "layers", "globex") + "\"\n[repo]\nglobex = \"" + globex + "\"\n"
	w.setManifest(mergeTables(m))
	if out, code := w.run("n\n", "init"); code != 0 {
		t.Fatalf("init: %s", out)
	}
	w.git(sd.mine, "remote", "add", "upstream", "https://github.com/acme-inc/mine.git")
	out, _ := w.run("", "apply")
	if !strings.Contains(out, "matches") || !strings.Contains(out, "acme") {
		t.Errorf("conflict not reported:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(sd.mine, "CLAUDE.local.md")); err == nil {
		t.Error("conflicting project must not be injected")
	}
}

func TestApplySymlinkedProjectFileSkipped(t *testing.T) {
	w := newWorld(t)
	sd := seedStandard(w)
	w.setManifest("trust_exec = [\"acme\"]\n" + w.manifest())
	w.run("n\n", "init")
	victim := filepath.Join(w.home, "victim.md")
	os.Symlink(victim, filepath.Join(sd.proj, "CLAUDE.local.md"))
	out, code := w.run("y\ny\ny\n", "apply")
	if code != 0 || !strings.Contains(out, "symbolic link") {
		t.Fatalf("apply: %d %s", code, out)
	}
	if _, err := os.Stat(victim); err == nil {
		t.Fatal("wrote through the symlink")
	}
	if w.read(".claude/CLAUDE.md") == "" {
		t.Error("the rest of apply did not run")
	}
}

func TestApplyExcludesReRegistered(t *testing.T) {
	w, _ := applied(t)
	os.Remove(filepath.Join(w.home, ".config", "git", "ignore"))
	w.run("", "apply")
	if !strings.Contains(w.read(".config/git/ignore"), "**/CLAUDE.local.md") {
		t.Error("excludes not re-registered")
	}
}

func TestApplyReplayPlugins(t *testing.T) {
	w, _ := applied(t)
	before := strings.Count(w.read("shim-calls.log"), "plugin install")
	w.run("y\ny\n", "apply")
	if after := strings.Count(w.read("shim-calls.log"), "plugin install"); after != before {
		t.Errorf("plugin install repeated without --replay-plugins: %d -> %d", before, after)
	}
	w.run("y\ny\n", "apply", "--replay-plugins")
	if after := strings.Count(w.read("shim-calls.log"), "plugin install"); after <= before {
		t.Error("--replay-plugins did not rerun the install")
	}
}

func TestApplyMCPForce(t *testing.T) {
	w, _ := applied(t)
	w.run("y\ny\n", "apply", "--mcp-force")
	log := w.read("shim-calls.log")
	if !strings.Contains(log, "mcp remove --scope user exa") || strings.LastIndex(log, "mcp add-json") < strings.LastIndex(log, "mcp remove") {
		t.Errorf("mcp-force must remove then add:\n%s", log)
	}
}

func TestApplyPullSkipsDirtyClone(t *testing.T) {
	w, _ := applied(t)
	w.write("layers/base/claude/rules/wip.md", "wip\n")
	w.pushToRemote("base", "claude/rules/new.md", "new\n")
	out, code := w.run("", "apply", "--pull")
	if code != 0 || !strings.Contains(out, "uncommitted changes") {
		t.Fatalf("pull must skip the dirty clone and go on: %d %s", code, out)
	}
	if w.read(".claude/rules/new.md") != "" {
		t.Error("dirty clone must not have been fast-forwarded")
	}
}

func TestApplyCheckFailureBlocksAll(t *testing.T) {
	w := newWorld(t)
	sd := seedStandard(w)
	w.setManifest("trust_exec = [\"acme\"]\n" + w.manifest())
	w.run("n\n", "init")
	if !strings.Contains(w.manifest(), "acme-inc") {
		t.Fatalf("init must persist the blocklist:\n%s", w.manifest())
	}
	w.write("layers/base/claude/rules/oops.md", "contact acme-inc support\n")
	out, code := w.run("y\ny\ny\n", "apply")
	if code == 0 || !strings.Contains(out, "blocklist") {
		t.Fatalf("blocklist word must stop apply: %d %s", code, out)
	}
	if w.read(".claude/CLAUDE.md") != "" || strings.Contains(w.read(".gitconfig"), "cclayer") {
		t.Error("something was applied although check failed")
	}
	_ = sd
}
