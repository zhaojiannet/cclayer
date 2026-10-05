// Package e2e drives the real cclayer binary against a throwaway home
// directory: local bare repositories stand in for GitHub, shell shims stand
// in for claude and gh. Run with `go test ./e2e/`.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cclayer-e2e-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "cclayer")
	build := exec.Command("go", "build", "-o", bin, "..")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// world is one isolated device: HOME, state, shims and the "GitHub" remotes.
type world struct {
	t     *testing.T
	home  string
	bins  string // shim directory, first on PATH
	calls string // file the shims append their invocations to
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the shims are sh scripts")
	}
	home := t.TempDir()
	w := &world{t: t, home: home, bins: filepath.Join(home, "bin"), calls: filepath.Join(home, "shim-calls.log")}
	os.MkdirAll(w.bins, 0o755)
	// claude shim: records purge calls, answers mcp get with "not found"
	w.shim("claude", `#!/bin/sh
echo "claude $*" >> "$SHIM_LOG"
state="$SHIM_LOG.mcp"
case "$1 $2" in
  "mcp get") grep -qx "$3" "$state" 2>/dev/null; exit $? ;;
  "mcp add-json") shift; shift; while [ "$1" = "--scope" ]; do shift; shift; done; echo "$1" >> "$state" ;;
esac
exit 0
`)
	// gh shim: logged in, accepts any api call
	w.shim("gh", `#!/bin/sh
echo "gh $*" >> "$SHIM_LOG"
exit 0
`)
	// ssh shim: no network in tests; git ls-remote over the alias fails at once
	w.shim("ssh", `#!/bin/sh
echo "ssh $*" >> "$SHIM_LOG"
exit 255
`)
	return w
}

func (w *world) shim(name, body string) {
	p := filepath.Join(w.bins, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) write(rel, body string) {
	p := filepath.Join(w.home, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(w.home, rel))
	return string(b)
}

func (w *world) env() []string {
	return []string{
		"HOME=" + w.home,
		"PATH=" + w.bins + string(os.PathListSeparator) + os.Getenv("PATH"),
		"XDG_CONFIG_HOME=" + filepath.Join(w.home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(w.home, ".local", "state"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(w.home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.com",
		"SHIM_LOG=" + w.calls,
		"TERM=dumb",
		// no network: HTTPS goes to a closed local port, ssh is a shim
		"https_proxy=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9",
		"http_proxy=http://127.0.0.1:9", "HTTP_PROXY=http://127.0.0.1:9",
	}
}

// run executes cclayer with stdin and returns stdout+stderr and the exit code.
func (w *world) run(stdin string, args ...string) (string, int) {
	return w.runEnv(nil, stdin, args...)
}

// runEnv is run with extra environment variables.
func (w *world) runEnv(extra []string, stdin string, args ...string) (string, int) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = w.home
	cmd.Env = append(w.env(), extra...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		w.t.Fatalf("%v: %v", args, err)
	}
	return string(out), code
}

func (w *world) git(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = w.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareRepo creates a bare "remote" seeded from the files in a work tree.
func (w *world) bareRepo(name string, files map[string]string) string {
	bare := filepath.Join(w.home, "remotes", name+".git")
	work := filepath.Join(w.home, "seed", name)
	os.MkdirAll(bare, 0o755)
	os.MkdirAll(work, 0o755)
	w.git(bare, "init", "-q", "--bare", "-b", "main")
	w.git(work, "init", "-q", "-b", "main")
	for rel, body := range files {
		p := filepath.Join(work, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	w.git(work, "add", "-A")
	w.git(work, "commit", "-q", "-m", "seed")
	w.git(work, "remote", "add", "origin", bare)
	w.git(work, "push", "-q", "origin", "main")
	return bare
}

// pushToRemote commits a change in the seed work tree and pushes it, as
// another device would.
func (w *world) pushToRemote(name, rel, body string) {
	work := filepath.Join(w.home, "seed", name)
	p := filepath.Join(work, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
	w.git(work, "add", "-A")
	w.git(work, "commit", "-q", "-m", "update "+rel)
	w.git(work, "push", "-q", "origin", "main")
}

func (w *world) project(name, remote string) string {
	dir := filepath.Join(w.home, "Projects", name)
	os.MkdirAll(dir, 0o755)
	w.git(dir, "init", "-q", "-b", "main")
	w.git(dir, "remote", "add", "origin", remote)
	return dir
}

// seeded is the standard device every scenario starts from: a base and an
// "acme" overlay as local bare remotes, a matching project with its own
// approvals, an unmatched personal project, a device manifest and a
// pre-existing settings.json and .gitconfig.
type seeded struct {
	base, acme, proj, mine string
}

// seedStandard builds the standard device without running cclayer.
func seedStandard(w *world) seeded {
	base := w.bareRepo("base", map[string]string{
		"layer.toml": `[layer]
name = "base"
kind = "base"
[claude]
paths = ["CLAUDE.md", "rules/", "skills/"]
settings_keys = ["theme", "permissions.deny", "hooks"]
ignore = ["skills/.trash/"]
[git]
fragment = "git/fragment.gitconfig"
`,
		"claude/CLAUDE.md":         "# shared\n",
		"claude/rules/comments.md": "comments rule\n",
		"claude/skills/x/SKILL.md": "---\nname: x\n---\nskill x\n",
		"claude/settings.json":     `{"theme": "dark", "permissions": {"deny": ["Read(.env)"]}, "hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "cclayer apply --hook || true"}]}]}}`,
		"git/fragment.gitconfig":   "[pull]\n\trebase = true\n",
		"mcp/exa.json":             `{"type": "http", "url": "https://mcp.example/${EXA_KEY}"}`,
	})
	acme := w.bareRepo("acme", map[string]string{
		"layer.toml": `[layer]
name = "acme"
kind = "overlay"
[identity]
name = "Acme Me"
email = "me@acme.example"
[[match]]
remote = "github.com/acme-inc/*"
[inject]
settings_keys = ["permissions.deny", "enabledPlugins"]
claude_local = "project/CLAUDE.local.md"
[git]
fragment = "git/fragment.gitconfig"
`,
		"project/settings.local.json": `{"permissions": {"deny": ["Bash(rm -rf *)"]}, "enabledPlugins": {"tools@acme-market": true}}`,
		"project/CLAUDE.local.md":     "acme rule\n",
		"git/fragment.gitconfig":      "[core]\n\thooksPath = ~/.config/acme/hooks\n",
	})
	proj := w.project("acme-api", "git@github.com:acme-inc/api.git")
	w.write("Projects/acme-api/.claude/settings.local.json", `{"permissions": {"allow": ["Bash(ls)"]}}`)
	mine := w.project("mine", "https://github.com/me/mine.git")
	w.write(".claude/settings.json", `{"theme": "light", "permissions": {"defaultMode": "auto", "allow": ["Bash(pwd)"]}}`)
	w.write(".gitconfig", "[credential]\n\thelper = store\n")
	w.write(".config/cclayer/device.toml", `
layers = ["base", "acme"]
roots = ["`+filepath.Join(w.home, "Projects")+`"]
[clone]
base = "`+filepath.Join(w.home, "layers", "base")+`"
acme = "`+filepath.Join(w.home, "layers", "acme")+`"
[repo]
base = "`+base+`"
acme = "`+acme+`"
`)

	return seeded{base: base, acme: acme, proj: proj, mine: mine}
}

// device.toml helpers used by many scenarios.
func (w *world) manifest() string { return w.read(".config/cclayer/device.toml") }
func (w *world) setManifest(s string) {
	w.write(".config/cclayer/device.toml", s)
}
func (w *world) replaceManifest(old, new string) {
	w.setManifest(strings.Replace(w.manifest(), old, new, 1))
}

func TestLifecycle(t *testing.T) {
	w := newWorld(t)
	sd := seedStandard(w)
	base, acme, proj, mine := sd.base, sd.acme, sd.proj, sd.mine
	_, _, _ = base, acme, mine

	// init clones the layers
	out, code := w.run("n\n", "init")
	if code != 0 {
		t.Fatalf("init: %s", out)
	}
	if !strings.Contains(out, "blocklist += acme") {
		t.Errorf("init output: %s", out)
	}

	// apply refuses the exec-capable fragment until trusted
	out, code = w.run("", "apply")
	if code == 0 || !strings.Contains(out, "trust_exec") {
		t.Fatalf("apply must refuse hooksPath without trust: %d %s", code, out)
	}
	w.write(".config/cclayer/device.toml", "trust_exec = [\"acme\"]\n"+w.read(".config/cclayer/device.toml"))

	// interactive apply: accept hooks, run claude commands
	out, code = w.run("y\ny\ny\ny\ny\n", "apply")
	if code != 0 {
		t.Fatalf("apply: %s", out)
	}
	for _, want := range []string{"# shared\n"} {
		if w.read(".claude/CLAUDE.md") != want {
			t.Errorf("CLAUDE.md = %q", w.read(".claude/CLAUDE.md"))
		}
	}
	set := w.read(".claude/settings.json")
	if !strings.Contains(set, `"dark"`) || !strings.Contains(set, `"auto"`) || !strings.Contains(set, "Bash(pwd)") || !strings.Contains(set, "cclayer apply --hook") {
		t.Errorf("settings.json:\n%s", set)
	}
	gc := w.read(".gitconfig")
	if !strings.HasPrefix(gc, "[credential]\n\thelper = store\n") || !strings.HasSuffix(strings.TrimSpace(gc), "path = ~/.gitconfig.cclayer") {
		t.Errorf("gitconfig:\n%s", gc)
	}
	main := w.read(".gitconfig.cclayer")
	if !strings.Contains(main, `rebase = "true"`) || !strings.Contains(main, "useConfigOnly = true") || !strings.Contains(main, "[aA][cC][mM][eE]-[iI][nN][cC]/**") || strings.Contains(main, "hookspath") {
		t.Errorf("gitconfig.cclayer:\n%s", main)
	}
	if id := w.read(".gitconfig.cclayer.d/acme.gitconfig"); !strings.Contains(id, "me@acme.example") || !strings.Contains(id, `hookspath = "~/.config/acme/hooks"`) {
		t.Errorf("identity file:\n%s", id)
	}
	// git itself resolves the identity through the include chain
	if got := w.git(proj, "config", "--get", "user.email"); got != "me@acme.example" {
		t.Errorf("git sees user.email = %q in the acme project", got)
	}
	if out := w.git(mine, "config", "--get", "user.useConfigOnly"); out != "true" {
		t.Errorf("useConfigOnly not active for unmatched repo: %q", out)
	}
	loc := w.read("Projects/acme-api/.claude/settings.local.json")
	if !strings.Contains(loc, "Bash(ls)") || !strings.Contains(loc, "rm -rf") {
		t.Errorf("project settings.local.json:\n%s", loc)
	}
	if cl := w.read("Projects/acme-api/CLAUDE.local.md"); !strings.Contains(cl, "acme rule") {
		t.Errorf("CLAUDE.local.md: %q", cl)
	}
	if _, err := os.Stat(filepath.Join(mine, "CLAUDE.local.md")); err == nil {
		t.Error("unmatched project must not receive overlay files")
	}
	if ex := w.read(".config/git/ignore"); !strings.Contains(ex, "**/CLAUDE.local.md") {
		t.Errorf("excludes: %q", ex)
	}
	// the overlay's plugin went to the project at local scope, the MCP server to user scope
	calls := w.read("shim-calls.log")
	if !strings.Contains(calls, "plugin install --scope local tools@acme-market") || !strings.Contains(calls, "mcp add-json --scope user exa") {
		t.Errorf("claude calls:\n%s", calls)
	}
	if strings.Contains(w.read(".claude/settings.json"), "acme-market") {
		t.Error("overlay plugin leaked into user settings")
	}

	// second apply is a no-op
	out, _ = w.run("", "apply")
	if !strings.Contains(out, "Nothing to do") {
		t.Errorf("second apply:\n%s", out)
	}

	// another device pushes a base change; --hook fast-forwards and applies it
	w.pushToRemote("base", "claude/rules/new.md", "new rule\n")
	w.write(".config/cclayer/device.toml", strings.Replace(w.read(".config/cclayer/device.toml"), "auto_pull = false", "auto_pull = true", 1))
	out, code = w.run("", "apply", "--hook")
	if code != 0 || w.read(".claude/rules/new.md") != "new rule\n" {
		t.Fatalf("hook apply: %d %s", code, out)
	}

	// local edit, capture writes it back, check passes, status lists the project
	w.write(".claude/rules/comments.md", "edited here\n")
	out, code = w.run("", "capture")
	if code != 0 || !strings.Contains(out, "base: claude/rules/comments.md") {
		t.Fatalf("capture: %d %s", code, out)
	}
	if w.read("layers/base/claude/rules/comments.md") != "edited here\n" {
		t.Error("capture did not write back")
	}
	if out, code = w.run("", "check"); code != 0 {
		t.Fatalf("check: %s", out)
	}
	if out, _ = w.run("", "status"); !strings.Contains(out, "projects of acme") || !strings.Contains(out, "acme-api") {
		t.Errorf("status:\n%s", out)
	}
	// a secret in the clone is refused by check and by apply
	w.write("layers/base/claude/rules/leak.md", "token = ghp_"+strings.Repeat("a", 36)+"\n")
	if out, code = w.run("", "check"); code == 0 {
		t.Errorf("check must refuse the secret:\n%s", out)
	}
	os.Remove(filepath.Join(w.home, "layers/base/claude/rules/leak.md"))

	// doctor is clean
	out, code = w.run("", "doctor")
	if code != 0 || strings.Contains(out, "warn") {
		t.Errorf("doctor:\n%s", out)
	}

	// profiles: env selects the overlay's config dir
	w.write(".config/cclayer/device.toml", strings.Replace(w.read(".config/cclayer/device.toml"), "profiles = false", "profiles = true", 1))
	if out, code = w.run("", "apply"); code != 0 {
		t.Fatalf("apply with profiles: %s", out)
	}
	out, code = w.run("", "env", proj)
	if code != 0 || !strings.Contains(out, "export CLAUDE_CONFIG_DIR=") || !strings.Contains(out, "acme") {
		t.Errorf("env: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".claude-profiles", "acme", "rules", "comments.md")); err != nil {
		t.Error("profile not populated")
	}

	// leave refuses while the overlay clone has uncommitted work
	w.write("layers/acme/project/CLAUDE.local.md", "acme rule v2\n")
	out, code = w.run("y\n", "leave", "acme")
	if code == 0 || !strings.Contains(out, "uncommitted changes") {
		t.Fatalf("leave must refuse a dirty clone: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(w.home, "layers", "acme")); err != nil {
		t.Fatal("refused leave must not touch the clone")
	}
	// --force goes ahead
	out, code = w.run("y\n", "leave", "--force", "acme")
	if code != 0 {
		t.Fatalf("leave --force: %s", out)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".claude-profiles", "acme")); !os.IsNotExist(err) {
		t.Error("profile directory of the left layer must be removed")
	}
	calls = w.read("shim-calls.log")
	if !strings.Contains(calls, "claude purge --yes "+proj) {
		t.Errorf("purge not called:\n%s", calls)
	}
	if _, err := os.Stat(filepath.Join(w.home, "layers", "acme")); !os.IsNotExist(err) {
		t.Error("acme clone not removed")
	}
	if strings.Contains(w.read(".gitconfig.cclayer"), "acme") {
		t.Error("acme includeIf survived leave")
	}
	probe := exec.Command("git", "config", "--get-all", "user.email")
	probe.Dir = proj
	probe.Env = w.env()
	if got, _ := probe.Output(); strings.TrimSpace(string(got)) != "" {
		t.Errorf("acme identity still active after leave: %q", got)
	}
	loc = w.read("Projects/acme-api/.claude/settings.local.json")
	if strings.Contains(loc, "rm -rf") || !strings.Contains(loc, "Bash(ls)") {
		t.Errorf("leave must remove injected keys and keep approvals:\n%s", loc)
	}
	if !strings.Contains(w.read(".config/cclayer/device.toml"), `layers = ["base"]`) {
		t.Errorf("device manifest after leave:\n%s", w.read(".config/cclayer/device.toml"))
	}

	// status reports a dirty clone
	w.write("layers/base/claude/rules/dirty.md", "x\n")
	out, code = w.run("", "status")
	if code != 0 || !strings.Contains(out, "uncommitted changes") {
		t.Errorf("status must show the dirty clone:\n%s", out)
	}
}

func TestKeysDeployKeyFlow(t *testing.T) {
	w := newWorld(t)
	acme := w.bareRepo("acme", map[string]string{"layer.toml": "[layer]\nname = \"acme\"\nkind = \"overlay\"\n[identity]\nname = \"A\"\nemail = \"a@acme.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n"})
	_ = acme
	w.write(".config/cclayer/device.toml", `
layers = ["base", "acme"]
roots = ["`+filepath.Join(w.home, "Projects")+`"]
[clone]
base = "`+filepath.Join(w.home, "layers", "base")+`"
acme = "`+filepath.Join(w.home, "layers", "acme")+`"
[repo]
base = "https://github.com/you/cclayer-base.git"
acme = "git@github.com:you/cclayer-acme.git"
`)
	// git ls-remote against the alias will fail (no network); the command still succeeds and reports it
	out, code := w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	if code != 0 {
		t.Fatalf("keys setup: %s", out)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".ssh", "cclayer-acme")); err != nil {
		t.Error("private key not generated by the real ssh-keygen")
	}
	cfg := w.read(".ssh/config")
	if !strings.Contains(cfg, "Host cclayer-acme") || !strings.Contains(cfg, "IdentitiesOnly yes") {
		t.Errorf("ssh config:\n%s", cfg)
	}
	if !strings.Contains(w.read("shim-calls.log"), "gh api -X POST repos/you/cclayer-acme/keys") {
		t.Errorf("deploy key not attached via gh:\n%s", w.read("shim-calls.log"))
	}
	if !strings.Contains(w.read(".config/cclayer/device.toml"), "git@cclayer-acme:you/cclayer-acme.git") {
		t.Errorf("manifest repo URL not rewritten:\n%s", w.read(".config/cclayer/device.toml"))
	}
	if !strings.Contains(out, "not reachable yet") {
		t.Errorf("unreachable alias must be reported, not hidden:\n%s", out)
	}
	out, _ = w.run("", "keys", "list")
	if !strings.Contains(out, "deploy-key") {
		t.Errorf("keys list:\n%s", out)
	}
}

func TestSetupAccessible(t *testing.T) {
	w := newWorld(t)
	base := w.bareRepo("base", map[string]string{
		"layer.toml":       "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\"]\n",
		"claude/CLAUDE.md": "# shared\n",
	})
	// accessible huh prompts read one line per field. The overview lists
	// base layer, add an overlay, project dirs, local clones, auto pull,
	// profiles, trusted layers, language, save, quit; a picked row returns
	// to it.
	answers := strings.Join([]string{
		"1",  // base layer
		base, // local bare repository: no credential question
		"3",  // project dirs
		filepath.Join(w.home, "Projects"),
		"9", // save
		"y", // apply now; doctor follows
	}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 {
		t.Fatalf("setup: %s", out)
	}
	if strings.Contains(out, "Invalid") {
		t.Errorf("an answer was rejected, the prompt sequence is off:\n%s", out)
	}
	if !strings.Contains(out, "include block is in place") {
		t.Errorf("doctor must follow apply:\n%s", out)
	}
	if w.read(".claude/CLAUDE.md") != "# shared\n" {
		t.Errorf("setup did not end in an applied device:\n%s", out)
	}
	if !strings.Contains(w.read(".config/cclayer/device.toml"), `layers = ["base"]`) {
		t.Errorf("manifest:\n%s", w.read(".config/cclayer/device.toml"))
	}
}
