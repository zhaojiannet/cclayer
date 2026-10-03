package keys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRepo(t *testing.T) {
	for _, in := range []string{
		"https://github.com/Owner/Repo.git", "git@github.com:Owner/Repo.git",
		"ssh://git@github.com/Owner/Repo", "https://user@github.com/Owner/Repo/", "git@cclayer-acme:Owner/Repo.git",
	} {
		r, err := ParseRepo(in)
		if err != nil || r.Owner != "Owner" || r.Name != "Repo" {
			t.Errorf("%q -> %+v %v", in, r, err)
		}
	}
	if _, err := ParseRepo("not a url"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestSSHConfigBlock(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host myserver\n  HostName 10.0.0.1\n"), 0o600)
	r := Repo{Host: "github.com", Owner: "o", Name: "r"}
	changed, err := EnsureSSHConfig(home, []string{HostBlock("acme", r, false)})
	if err != nil || !changed {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	s := string(b)
	if !strings.HasPrefix(s, "# cclayer:begin\nHost cclayer-acme\n  HostName github.com\n") || !strings.HasSuffix(s, "Host myserver\n  HostName 10.0.0.1\n") {
		t.Fatalf("block must come first, user stanza kept:\n%s", s)
	}
	// replace with a 443 variant and a second layer
	EnsureSSHConfig(home, []string{HostBlock("acme", r, true), HostBlock("base", r, false)})
	b, _ = os.ReadFile(filepath.Join(home, ".ssh", "config"))
	s = string(b)
	if strings.Count(s, "# cclayer:begin") != 1 || !strings.Contains(s, "ssh.github.com\n  Port 443") || !strings.Contains(s, "Host cclayer-base") {
		t.Fatalf("replace wrong:\n%s", s)
	}
	// empty removes the block, keeps the user's stanza
	EnsureSSHConfig(home, nil)
	b, _ = os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if strings.Contains(string(b), "cclayer") || !strings.Contains(string(b), "myserver") {
		t.Fatalf("remove wrong:\n%s", b)
	}
	if SSHURL("acme", r) != "git@cclayer-acme:o/r.git" {
		t.Error(SSHURL("acme", r))
	}
	cmd := DeployKeyCommand(r, "dev", "ssh-ed25519 AAA")
	if strings.Join(cmd, " ") != "api -X POST repos/o/r/keys -f title=dev -f key=ssh-ed25519 AAA -F read_only=false" {
		t.Error(strings.Join(cmd, " "))
	}
}

func TestAuthError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"git ls-remote https://github.com/acme/infra-403.git: exit status 128\nfatal: unable to access 'https://github.com/acme/infra-403.git/': Could not resolve host: github.com", false},
		{"git ls-remote https://github.com/acme/x.git: exit status 128\nremote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/acme/x.git/'", true},
		{"exit status 128\nfatal: unable to access 'https://github.com/acme/x.git/': The requested URL returned error: 403", true},
		{"exit status 128\nfatal: could not read Username for 'https://github.com': terminal prompts disabled", true},
		{"exit status 128\nremote: Repository not found.", false},
	}
	for _, c := range cases {
		if got := AuthError(errors.New(c.msg)); got != c.want {
			t.Errorf("AuthError(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
	if AuthError(nil) {
		t.Error("nil is not an auth error")
	}
	if u := httpsURLWithUser(Repo{Host: "github.com", Owner: "o", Name: "r"}, "cclayer-acme"); u != "https://cclayer-acme@github.com/o/r.git" {
		t.Error(u)
	}
}
