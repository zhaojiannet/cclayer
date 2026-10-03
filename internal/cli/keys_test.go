package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// fakeKeys stands in for ssh-keygen, gh and git ls-remote.
type fakeKeys struct {
	home     string
	ghAuthed bool
	calls    []string
}

func (f *fakeKeys) Run(dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "ssh-keygen":
		priv := args[len(args)-1]
		os.WriteFile(priv, []byte("PRIVATE"), 0o600)
		os.WriteFile(priv+".pub", []byte("ssh-ed25519 AAAATEST comment\n"), 0o644)
		return "", nil
	case "gh":
		if args[0] == "auth" {
			if f.ghAuthed {
				return "", nil
			}
			return "", errors.New("not logged in")
		}
		return `{"id": 1}`, nil
	case "git":
		return "abc\trefs/heads/main", nil
	}
	return "", nil
}

func TestKeysSetupDeployKey(t *testing.T) {
	e, home := fixture(t)
	fk := &fakeKeys{home: home, ghAuthed: true}
	old := keyRunner
	keyRunner = fk
	defer func() { keyRunner = old }()
	dev, _ := os.ReadFile(e.DevicePath)
	write(t, e.DevicePath, strings.Replace(string(dev), `acme = "https://git.example/acme.git"`, `acme = "git@github.com:you/cclayer-acme.git"`, 1))
	if code := Run(e, []string{"keys", "setup", "acme", "--method", "deploy-key"}); code != 0 {
		t.Fatalf("keys setup: %s", e.Stderr.(*bytes.Buffer).String())
	}
	d, _ := manifest.LoadDevice(e.DevicePath)
	if d.Repo["acme"] != "git@cclayer-acme:you/cclayer-acme.git" || d.Auth["acme"] != "deploy-key" {
		t.Errorf("manifest not updated: %+v", d)
	}
	cfg, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if !strings.Contains(string(cfg), "Host cclayer-acme") || !strings.Contains(string(cfg), "IdentityFile ~/.ssh/cclayer-acme") {
		t.Errorf("ssh config:\n%s", cfg)
	}
	joined := strings.Join(fk.calls, "\n")
	if !strings.Contains(joined, "gh api -X POST repos/you/cclayer-acme/keys") || !strings.Contains(joined, "read_only=false") {
		t.Errorf("deploy key not attached: %s", joined)
	}
	if !strings.Contains(out(e), "attached") || !strings.Contains(out(e), "reachable over ssh") {
		t.Errorf("summary:\n%s", out(e))
	}
	// not logged in: prints the command instead of failing
	fk.ghAuthed = false
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"keys", "setup", "acme", "--method", "deploy-key"}); code != 0 {
		t.Fatalf("second setup: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if !strings.Contains(out(e), "gh api -X POST") {
		t.Errorf("must print the command for another machine:\n%s", out(e))
	}
	// remove cleans the stanza and the key files
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"keys", "remove", "acme"}); code != 0 {
		t.Fatalf("remove: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "cclayer-acme")); !os.IsNotExist(err) {
		t.Error("private key not removed")
	}
	cfg, _ = os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if strings.Contains(string(cfg), "cclayer-acme") {
		t.Error("ssh stanza not removed")
	}
	if !strings.Contains(out(e), "Revoke the deploy key") {
		t.Errorf("revoke hint missing:\n%s", out(e))
	}
}

func TestKeysSetupTokenRequiresTokenNonInteractive(t *testing.T) {
	e, _ := fixture(t)
	dev, _ := os.ReadFile(e.DevicePath)
	write(t, e.DevicePath, strings.Replace(string(dev), `acme = "https://git.example/acme.git"`, `acme = "https://github.com/you/cclayer-acme.git"`, 1))
	e.Interactive = false
	if code := Run(e, []string{"keys", "setup", "acme", "--method", "token"}); code == 0 {
		t.Fatal("token without --token and without a terminal must fail")
	}
}

func TestProfilesEnvAndApply(t *testing.T) {
	e, home := fixture(t)
	dev, _ := os.ReadFile(e.DevicePath)
	write(t, e.DevicePath, "profiles = true\nprofile_dir = \""+filepath.ToSlash(filepath.Join(home, "profiles"))+"\"\n"+string(dev))
	if code := Run(e, []string{"apply"}); code != 0 {
		t.Fatalf("apply: %s", e.Stderr.(*bytes.Buffer).String())
	}
	prof := filepath.Join(home, "profiles", "acme")
	if _, err := os.Stat(filepath.Join(prof, "rules", "comments.md")); err != nil {
		t.Error("base rules not copied into the profile")
	}
	if _, err := os.Stat(filepath.Join(prof, "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md must not be copied into a profile (#88528)")
	}
	if b, _ := os.ReadFile(filepath.Join(prof, "settings.json")); !strings.Contains(string(b), "dark") {
		t.Errorf("profile settings missing owned keys: %s", b)
	}
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"env", filepath.Join(home, "Projects", "acme-api")}); code != 0 {
		t.Fatalf("env: %s", e.Stderr.(*bytes.Buffer).String())
	}
	// env speaks the shell of the platform: sh syntax elsewhere, PowerShell on Windows
	setLine, unsetLine := "export CLAUDE_CONFIG_DIR=", "unset CLAUDE_CONFIG_DIR"
	if runtime.GOOS == "windows" {
		setLine, unsetLine = "$env:CLAUDE_CONFIG_DIR = ", "Remove-Item Env:CLAUDE_CONFIG_DIR"
	}
	if !strings.Contains(out(e), setLine) || !strings.Contains(out(e), prof) {
		t.Errorf("env output: %s", out(e))
	}
	e.Stdout = &bytes.Buffer{}
	Run(e, []string{"env", home})
	if !strings.Contains(out(e), unsetLine) {
		t.Errorf("unmatched dir must unset: %s", out(e))
	}
	e.Stdout = &bytes.Buffer{}
	if code := Run(e, []string{"run", filepath.Join(home, "Projects", "acme-api"), "--", "sh", "-c", "echo $CLAUDE_CONFIG_DIR"}); code != 0 {
		t.Fatalf("run: %s", e.Stderr.(*bytes.Buffer).String())
	}
	if strings.TrimSpace(out(e)) != prof {
		t.Errorf("run env: %q", out(e))
	}
}
