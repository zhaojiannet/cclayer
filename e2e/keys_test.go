package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keysWorld is an applied device whose overlay URL is GitHub-shaped.
func keysWorld(t *testing.T) (*world, seeded) {
	t.Helper()
	w, sd := applied(t)
	w.replaceManifest(sd.acme, "git@github.com:you/cclayer-acme.git")
	return w, sd
}

func TestKeysDeployKeyNotLoggedIn(t *testing.T) {
	w, _ := keysWorld(t)
	w.shim("gh", "#!/bin/sh\necho \"gh $*\" >> \"$SHIM_LOG\"\nexit 1\n")
	out, code := w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	if code != 0 || !strings.Contains(out, "gh is not logged in") || !strings.Contains(out, "gh api -X POST repos/you/cclayer-acme/keys") {
		t.Fatalf("must print the command for another machine: %d %s", code, out)
	}
	if !strings.Contains(out, "ssh-ed25519") {
		t.Error("public key not shown")
	}
}

func TestKeysPort443Persisted(t *testing.T) {
	w, sd := keysWorld(t)
	w.run("", "keys", "setup", "acme", "--method", "deploy-key", "--port-443")
	// a second overlay set up without the flag must not reset acme's stanza
	globex := w.bareRepo("globex", map[string]string{"layer.toml": "[layer]\nname = \"globex\"\nkind = \"overlay\"\n[identity]\nname = \"G\"\nemail = \"g@globex.example\"\n[[match]]\nremote = \"github.com/globex-inc/*\"\n"})
	m := strings.Replace(w.manifest(), `layers = ["base", "acme"]`, `layers = ["base", "acme", "globex"]`, 1)
	m += "\n[clone]\nglobex = \"" + filepath.Join(w.home, "layers", "globex") + "\"\n[repo]\nglobex = \"" + globex + "\"\n"
	w.setManifest(mergeTables(m))
	w.run("n\n", "init")
	w.replaceManifest(globex, "git@github.com:you/cclayer-globex.git")
	w.run("", "keys", "setup", "globex", "--method", "deploy-key")
	cfg := w.read(".ssh/config")
	acmeBlock := cfg[strings.Index(cfg, "Host cclayer-acme"):]
	acmeBlock = acmeBlock[:strings.Index(acmeBlock, "Host cclayer-globex")]
	if !strings.Contains(acmeBlock, "Port 443") || !strings.Contains(acmeBlock, "ssh.github.com") {
		t.Errorf("acme lost its port 443 routing:\n%s", cfg)
	}
	if !strings.Contains(w.manifest(), "key_port443") {
		t.Errorf("port setting not persisted:\n%s", w.manifest())
	}
	_ = sd
}

func TestKeysTokenStoredPerPath(t *testing.T) {
	w, _ := keysWorld(t)
	w.replaceManifest("git@github.com:you/cclayer-acme.git", "https://github.com/you/cclayer-acme.git")
	store := filepath.Join(w.home, "git-credentials")
	w.write(".gitconfig", w.read(".gitconfig")+"[credential]\n\thelper = store --file "+store+"\n")
	out, code := w.run("TOKEN123\n", "keys", "setup", "acme", "--method", "token")
	if code != 0 || !strings.Contains(out, "token stored") {
		t.Fatalf("token setup: %d %s", code, out)
	}
	cred := w.read("git-credentials")
	if !strings.Contains(cred, "https://cclayer-acme:TOKEN123@github.com/you/cclayer-acme.git") {
		t.Errorf("token must be stored with path and layer username:\n--file: %q\ndefault store: %q\ngitconfig:\n%s\nout:\n%s", cred, w.read(".git-credentials"), w.read(".gitconfig"), out)
	}
	if !strings.Contains(w.manifest(), `acme = "token"`) {
		t.Errorf("auth not recorded:\n%s", w.manifest())
	}
	w.run("", "apply")
	if !strings.Contains(w.read(".gitconfig.cclayer"), `[credential "https://github.com/you/cclayer-acme.git"]`) {
		t.Errorf("useHttpPath not written for the token repository:\n%s", w.read(".gitconfig.cclayer"))
	}
	if strings.Contains(out, "TOKEN123") {
		t.Error("token echoed in output")
	}
}

func TestKeysTokenWithoutHelper(t *testing.T) {
	w, _ := keysWorld(t)
	w.replaceManifest("git@github.com:you/cclayer-acme.git", "https://github.com/you/cclayer-acme.git")
	w.write(".gitconfig", strings.Replace(w.read(".gitconfig"), "[credential]\n\thelper = store\n", "", 1))
	out, code := w.run("TOKEN123\n", "keys", "setup", "acme", "--method", "token")
	if code == 0 || !strings.Contains(out, "credential.helper") {
		t.Fatalf("missing helper must be an error: %d %s", code, out)
	}
	if strings.Contains(out, "token stored") {
		t.Error("claimed success without a helper")
	}
}

func TestKeysSwitchMethodCleansOld(t *testing.T) {
	w, _ := keysWorld(t)
	w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	if w.read(".ssh/cclayer-acme") == "" {
		t.Fatal("key not generated")
	}
	store := filepath.Join(w.home, "git-credentials")
	w.write(".gitconfig", w.read(".gitconfig")+"[credential]\n\thelper = store --file "+store+"\n")
	out, code := w.run("TOKEN123\n", "keys", "setup", "acme", "--method", "token")
	if code != 0 || !strings.Contains(out, "replaced the previous deploy-key") {
		t.Fatalf("switch: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".ssh", "cclayer-acme")); !os.IsNotExist(err) {
		t.Error("old key not removed")
	}
	if strings.Contains(w.read(".ssh/config"), "cclayer-acme") {
		t.Error("old stanza not removed")
	}
}

func TestKeysRemovePrintsRevocation(t *testing.T) {
	w, _ := keysWorld(t)
	w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	out, _ := w.run("", "keys", "remove", "acme")
	if !strings.Contains(out, "Revoke the deploy key") || w.read(".ssh/cclayer-acme") != "" || strings.Contains(w.read(".ssh/config"), "cclayer-acme") {
		t.Fatalf("remove deploy key: %s", out)
	}
	w.replaceManifest("git@github.com:you/cclayer-acme.git", "https://github.com/you/cclayer-acme.git")
	store := filepath.Join(w.home, "git-credentials")
	w.write(".gitconfig", w.read(".gitconfig")+"[credential]\n\thelper = store --file "+store+"\n")
	w.run("TOKEN123\n", "keys", "setup", "acme", "--method", "token")
	out, _ = w.run("", "keys", "remove", "acme")
	if !strings.Contains(out, "git -c credential.useHttpPath=true credential reject") {
		t.Errorf("token removal hint missing:\n%s", out)
	}
}

func TestKeysExtraPositionalRejected(t *testing.T) {
	w, _ := keysWorld(t)
	if out, code := w.run("", "keys", "setup", "acme", "extra", "--method", "deploy-key"); code != 2 {
		t.Errorf("extra argument must be a usage error: %d %s", code, out)
	}
	if out, code := w.run("", "leave", "acme", "extra"); code != 2 {
		t.Errorf("leave with extra argument must be a usage error: %d %s", code, out)
	}
}

func TestKeysSSHBlockFirst(t *testing.T) {
	w, _ := keysWorld(t)
	w.write(".ssh/config", "Host *\n  IdentityFile ~/.ssh/id_ed25519\n  AddKeysToAgent yes\n")
	w.run("", "keys", "setup", "acme", "--method", "deploy-key")
	cfg := w.read(".ssh/config")
	if !strings.HasPrefix(cfg, "# cclayer:begin") || strings.Index(cfg, "Host cclayer-acme") > strings.Index(cfg, "Host *") {
		t.Errorf("managed block must come before the user's Host *:\n%s", cfg)
	}
	if !strings.Contains(cfg, "AddKeysToAgent yes") {
		t.Error("user stanza lost")
	}
}
