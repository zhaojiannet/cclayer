package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/keys"
	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// keyRunner is the runner the keys commands use; tests replace it.
var keyRunner keys.Runner = keys.Exec{}

func runKeys(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("usage: cclayer keys setup <layer> [--method deploy-key|token] [--port-443] | cclayer keys list | cclayer keys remove <layer>")
	}
	switch args[0] {
	case "setup":
		return runKeysSetup(e, args[1:])
	case "list":
		return runKeysList(e)
	case "remove":
		if len(args) != 2 {
			return usagef("usage: cclayer keys remove <layer>")
		}
		return runKeysRemove(e, args[1])
	}
	return usagef("keys: unknown subcommand %q", args[0])
}

func runKeysSetup(e *Env, args []string) error {
	fs := flag.NewFlagSet("keys setup", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	method := fs.String("method", "", "deploy-key or token; asked when omitted")
	port443 := fs.Bool("port-443", false, "reach GitHub over ssh.github.com:443 (networks that block port 22)")
	layer, rest := splitPositional(args)
	if err := fs.Parse(rest); err != nil {
		return usagef("keys setup: %v", err)
	}
	if layer == "" && fs.NArg() == 1 {
		layer = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return usagef("usage: cclayer keys setup <layer> [--method deploy-key|token] [--port-443]")
	}
	if layer == "" {
		return usagef("usage: cclayer keys setup <layer> [--method deploy-key|token] [--port-443]")
	}
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		return err
	}
	d.EnsureMaps()
	url, ok := d.Repo[layer]
	if !ok || url == "" {
		return fmt.Errorf("layer %q has no [repo] URL in the device manifest; a layer that lives in a local directory needs no credentials", layer)
	}
	m, token, err := askCredential(e, url, *method)
	if err != nil {
		return err
	}
	*method = m
	res, err := setupCredential(e, d, layer, url, *method, *port443, token)
	if err != nil {
		return err
	}
	if err := d.Save(e.DevicePath); err != nil {
		return err
	}
	e.printf("%s", res)
	return nil
}

// askCredential settles the method for a layer repository, asking on a
// terminal when none was given, and reads the token for the token method:
// without echo on a terminal, as one line from a pipe.
func askCredential(e *Env, url, method string) (string, string, error) {
	if method == "" {
		if !e.Interactive {
			return "", "", usagef("--method is required without a terminal")
		}
		if e.confirm("Use a deploy key (one SSH key for this repository, created and attached automatically)? Answer no for an HTTPS token you create on GitHub") {
			method = "deploy-key"
		} else {
			method = "token"
		}
	}
	if method != "token" {
		return method, "", nil
	}
	if !e.Interactive {
		return "", "", usagef("the token method cannot run from the SessionStart hook")
	}
	repo, err := keys.ParseRepo(url)
	if err != nil {
		return "", "", err
	}
	e.printf("%s", keys.TokenInstructions(repo.Owner, []string{repo.Name}))
	token, err := readSecret(e, i18n.T("Paste the token (not echoed): "))
	return method, token, err
}

// readSecret reads one line without echo when stdin is a terminal, and a
// plain line otherwise (a pipe cannot echo anyway).
func readSecret(e *Env, prompt string) (string, error) {
	fmt.Fprint(e.Stdout, prompt)
	if f, ok := e.Stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		e.printf("\n")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, _ := e.reader().ReadString('\n')
	return strings.TrimSpace(line), nil
}

// setupCredential does the work for one layer and returns a human summary.
// It is shared with the setup wizard. Switching methods drops the previous
// credential first.
func setupCredential(e *Env, d *manifest.Device, layer, url, method string, port443 bool, token string) (string, error) {
	d.EnsureMaps()
	repo, err := keys.ParseRepo(url)
	if err != nil {
		return "", err
	}
	if h, ok := d.KeyHost[layer]; ok && strings.HasPrefix(repo.Host, "cclayer-") {
		repo.Host = h // already aliased by an earlier run; recover the real host
	}
	var out strings.Builder
	if prev := d.Auth[layer]; prev != "" && prev != method {
		if err := removeCredential(e, d, layer, false); err != nil {
			return "", err
		}
		fmt.Fprintf(&out, i18n.T("replaced the previous %s credential\n"), prev)
	}
	switch method {
	case "deploy-key":
		hostname, _ := os.Hostname()
		pub, created, err := keys.Generate(keyRunner, e.Home, layer, "cclayer-"+layer+"@"+hostname)
		if err != nil {
			return "", err
		}
		if created {
			fmt.Fprintf(&out, i18n.T("generated %s\n"), e.tilde(keys.KeyPath(e.Home, layer)))
		}
		d.Auth[layer] = "deploy-key"
		d.KeyHost[layer] = repo.Host
		d.KeyPort443[layer] = port443
		d.Repo[layer] = keys.SSHURL(layer, repo)
		if err := writeSSHStanzas(e, d); err != nil {
			return "", err
		}
		title := "cclayer " + layer + " @ " + hostname
		cmd := "gh " + strings.Join(keys.DeployKeyCommand(repo, title, pub), " ")
		if keys.GHAuthenticated(keyRunner, repo.Host) {
			if err := keys.AddDeployKey(keyRunner, repo, title, pub); err != nil {
				fmt.Fprintf(&out, i18n.T("gh could not attach the deploy key (%s).\n"), firstLine(err.Error()))
				fmt.Fprintf(&out, i18n.T("Attach it yourself, on a machine where gh is logged in as an administrator of %s/%s:\n  %s\n"), repo.Owner, repo.Name, cmd)
			} else {
				fmt.Fprintf(&out, i18n.T("deploy key attached to %s/%s with write access\n"), repo.Owner, repo.Name)
			}
		} else {
			fmt.Fprintf(&out, i18n.T("gh is not logged in here. Public key:\n  %s\nAttach it on a machine where gh is logged in as an administrator of %s/%s:\n  %s\nOr paste it under Settings > Deploy keys of the repository with \"Allow write access\".\n"),
				pub, repo.Owner, repo.Name, cmd)
		}
		if err := keys.Verify(keyRunner, d.Repo[layer]); err != nil {
			fmt.Fprintf(&out, i18n.T("not reachable yet (%s); run `cclayer keys setup %s` again after the key is attached\n"), firstLine(err.Error()), layer)
		} else {
			fmt.Fprintf(&out, i18n.T("%s reachable over ssh\n"), d.Repo[layer])
		}
	case "token":
		if !keys.HelperConfigured(keyRunner) {
			return "", fmt.Errorf("git has no credential.helper, so the token could not be stored anywhere. Configure one first, for example `git config --global credential.helper osxkeychain` (macOS), `manager` (Windows) or `libsecret` (Linux)")
		}
		// verify before storing: a rejected attempt through the helper would
		// make git erase the credential it just saved
		verr := keys.VerifyHTTPS(repo, keys.TokenUsername(layer), token)
		if keys.AuthError(verr) {
			return "", fmt.Errorf("the repository rejected the token (%s); nothing stored. Check the token's repository access and permissions", firstLine(verr.Error()))
		}
		if err := keys.StoreToken(repo, keys.TokenUsername(layer), token); err != nil {
			return "", err
		}
		d.Auth[layer] = "token"
		d.Repo[layer] = keys.HTTPSURL(repo)
		fmt.Fprintf(&out, i18n.T("token stored in the git credential helper for %s (username %s)\n"), d.Repo[layer], keys.TokenUsername(layer))
		if verr != nil {
			fmt.Fprintf(&out, i18n.T("could not confirm access (%s); the token is stored, check with `git ls-remote %s`\n"), firstLine(verr.Error()), d.Repo[layer])
		} else {
			fmt.Fprintf(&out, i18n.T("%s reachable over https\n"), d.Repo[layer])
		}
	default:
		return "", usagef("method must be deploy-key or token, got %q", method)
	}
	// a clone that already exists must follow the new URL
	if clone := d.ClonePath(layer); exists(filepath.Join(clone, ".git")) {
		if _, err := git(clone, "remote", "set-url", "origin", d.Repo[layer]); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

// writeSSHStanzas regenerates cclayer's block in ~/.ssh/config from every
// layer that uses a deploy key, with each layer's own host and port.
func writeSSHStanzas(e *Env, d *manifest.Device) error {
	var list []string
	for _, l := range d.Layers {
		if d.Auth[l] != "deploy-key" {
			continue
		}
		r, err := keys.ParseRepo(d.Repo[l])
		if err != nil {
			continue
		}
		if h := d.KeyHost[l]; h != "" {
			r.Host = h
		}
		list = append(list, keys.HostBlock(l, r, d.KeyPort443[l]))
	}
	_, err := keys.EnsureSSHConfig(e.Home, list)
	return err
}

// tokenRepos lists the HTTPS URLs of layers that authenticate with a token.
func tokenRepos(d *manifest.Device) []string {
	var out []string
	for _, l := range d.Layers {
		if d.Auth[l] == "token" {
			out = append(out, d.Repo[l])
		}
	}
	return out
}

func runKeysList(e *Env) error {
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		return err
	}
	for _, l := range d.Layers {
		m := d.Auth[l]
		if m == "" {
			m = "git default"
		}
		e.printf("%-12s %-11s %s\n", l, m, d.Repo[l])
	}
	return nil
}

// runKeysRemove deletes the device's key material for a layer and prints
// what to revoke on GitHub. It is also called by leave.
func runKeysRemove(e *Env, layer string) error {
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		return err
	}
	return removeCredential(e, d, layer, true)
}

func removeCredential(e *Env, d *manifest.Device, layer string, save bool) error {
	d.EnsureMaps()
	method := d.Auth[layer]
	hostname, _ := os.Hostname()
	switch method {
	case "deploy-key":
		for _, p := range []string{keys.KeyPath(e.Home, layer), keys.KeyPath(e.Home, layer) + ".pub"} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		r, perr := keys.ParseRepo(d.Repo[layer])
		if perr == nil && d.KeyHost[layer] != "" {
			r.Host = d.KeyHost[layer]
			d.Repo[layer] = fmt.Sprintf("git@%s:%s/%s.git", r.Host, r.Owner, r.Name)
		}
		delete(d.Auth, layer)
		delete(d.KeyHost, layer)
		delete(d.KeyPort443, layer)
		if err := writeSSHStanzas(e, d); err != nil {
			return err
		}
		if perr == nil {
			e.printf("Revoke the deploy key on GitHub: repository %s/%s, Settings > Deploy keys, the key titled \"cclayer %s @ %s\"\n", r.Owner, r.Name, layer, hostname)
		}
	case "token":
		delete(d.Auth, layer)
		if r, err := keys.ParseRepo(d.Repo[layer]); err == nil {
			e.printf("Revoke the token on GitHub (Settings > Developer settings > Fine-grained tokens) and drop it from the credential helper:\n  %s\n", keys.RejectCommand(r, keys.TokenUsername(layer)))
		}
	default:
		return nil
	}
	if save {
		return d.Save(e.DevicePath)
	}
	return nil
}

// splitPositional lets a positional argument come before the flags, as in
// `keys setup acme --method token`, which Go's flag package would otherwise
// treat as the end of the flags.
func splitPositional(args []string) (first string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
