// Package keys gives a device its own credentials for layer repositories:
// one deploy key per repository, or one HTTPS token stored in the system
// credential helper. Both are scoped to the repositories the device may see.
package keys

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/fsx"
	"github.com/zhaojiannet/cclayer/internal/i18n"
)

// Runner executes external commands; tests substitute a fake.
type Runner interface {
	Run(dir string, name string, args ...string) (stdout string, err error)
}

// Exec is the real runner.
type Exec struct{}

// Run executes name with args in dir.
func (Exec) Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// never block on a password prompt or a slow network while verifying
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Repo is a parsed GitHub-style repository reference.
type Repo struct {
	Host  string // github.com
	Owner string
	Name  string
}

var repoRe = regexp.MustCompile(`^(?:https?://(?:[^@/]+@)?|ssh://(?:[^@/]+@)?|[^@/:]+@)?([^/:]+)(?::\d+)?[:/]([^/]+)/([^/]+?)(?:\.git)?/?$`)

// ParseRepo understands https://host/owner/repo(.git), git@host:owner/repo
// and ssh://git@host/owner/repo. An ssh alias host is returned as written.
func ParseRepo(url string) (Repo, error) {
	m := repoRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return Repo{}, fmt.Errorf("%q is not a repository URL", url)
	}
	return Repo{Host: strings.ToLower(m[1]), Owner: m[2], Name: m[3]}, nil
}

// Alias is the ssh config host alias cclayer writes for a layer.
func Alias(layer string) string { return "cclayer-" + layer }

// KeyPath is where the layer's private key lives.
func KeyPath(home, layer string) string {
	return filepath.Join(home, ".ssh", "cclayer-"+layer)
}

// SSHURL is the clone URL that goes through the alias.
func SSHURL(layer string, r Repo) string {
	return fmt.Sprintf("git@%s:%s/%s.git", Alias(layer), r.Owner, r.Name)
}

// Generate creates an ed25519 key pair without passphrase for the layer and
// returns the public key line. An existing key is kept.
func Generate(r Runner, home, layer, comment string) (pub string, created bool, err error) {
	priv := KeyPath(home, layer)
	if _, err := os.Stat(priv); err == nil {
		b, err := os.ReadFile(priv + ".pub")
		return strings.TrimSpace(string(b)), false, err
	}
	if err := os.MkdirAll(filepath.Dir(priv), 0o700); err != nil {
		return "", false, err
	}
	if _, err := r.Run("", "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", priv); err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(priv + ".pub")
	return strings.TrimSpace(string(b)), true, err
}

const (
	beginMarker = "# cclayer:begin"
	endMarker   = "# cclayer:end"
)

// HostBlock renders the ssh config stanza for one layer. port443 routes
// through ssh.github.com:443 for networks that block port 22.
func HostBlock(layer string, r Repo, port443 bool) string {
	host, port := r.Host, ""
	if port443 && r.Host == "github.com" {
		host, port = "ssh.github.com", "  Port 443\n"
	}
	return fmt.Sprintf("Host %s\n  HostName %s\n%s  User git\n  IdentityFile %s\n  IdentitiesOnly yes\n",
		Alias(layer), host, port, "~/.ssh/cclayer-"+layer)
}

// EnsureSSHConfig writes the managed block holding every layer stanza at the
// top of ~/.ssh/config, keeping everything outside the markers untouched. The
// block goes first because ssh takes the first value of each option: a
// user's `Host *` with its own IdentityFile must not shadow the deploy key.
func EnsureSSHConfig(home string, stanzas []string) (changed bool, err error) {
	path := filepath.Join(home, ".ssh", "config")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	block := ""
	if len(stanzas) > 0 {
		block = beginMarker + "\n" + strings.Join(stanzas, "\n") + endMarker + "\n"
	}
	next := replaceBlock(string(old), block)
	if next == string(old) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	return true, fsx.WriteUserFile(home, path, []byte(next), 0o600)
}

func replaceBlock(doc, block string) string {
	bi := strings.Index(doc, beginMarker)
	ei := strings.Index(doc, endMarker)
	if bi >= 0 && ei > bi {
		end := ei + len(endMarker)
		if end < len(doc) && doc[end] == '\n' {
			end++
		}
		doc = doc[:bi] + doc[end:]
	}
	doc = strings.TrimLeft(strings.TrimRight(doc, "\n"), "\n")
	if block == "" {
		if doc == "" {
			return ""
		}
		return doc + "\n"
	}
	if doc == "" {
		return block
	}
	return block + "\n" + doc + "\n"
}

// DeployKeyCommand is the gh invocation that attaches pub to the repository
// with write access. It is returned rather than run so the caller can show
// it, run it, or hand it to the user to run on a machine where gh is logged
// in with admin rights on the repository.
func DeployKeyCommand(r Repo, title, pub string) []string {
	return []string{"api", "-X", "POST", fmt.Sprintf("repos/%s/%s/keys", r.Owner, r.Name),
		"-f", "title=" + title, "-f", "key=" + pub, "-F", "read_only=false"}
}

// GHAuthenticated reports whether gh is logged in to the host.
func GHAuthenticated(r Runner, host string) bool {
	_, err := r.Run("", "gh", "auth", "status", "--hostname", host)
	return err == nil
}

// AddDeployKey attaches the key through gh. The error carries gh's own
// message, which names the missing permission when the account is not an
// administrator of the repository.
func AddDeployKey(r Runner, repo Repo, title, pub string) error {
	_, err := r.Run("", "gh", DeployKeyCommand(repo, title, pub)...)
	return err
}

// HelperConfigured reports whether git has a credential helper; without
// one `git credential approve` exits 0 and stores nothing.
func HelperConfigured(r Runner) bool {
	out, err := r.Run("", "git", "config", "--get-all", "credential.helper")
	return err == nil && strings.TrimSpace(out) != ""
}

// TokenUsername is the username stored with a layer's token. It is not an
// account name, so it cannot collide with the user's own GitHub login in
// the credential helper.
func TokenUsername(layer string) string { return "cclayer-" + layer }

// StoreToken hands an HTTPS token to the configured git credential helper
// for one repository path. useHttpPath is forced for the call so the helper
// keys the entry by host and path rather than replacing the host-wide
// credential of the user's own account.
func StoreToken(repo Repo, username, token string) error {
	if token == "" {
		return fmt.Errorf("empty token")
	}
	input := fmt.Sprintf("protocol=https\nhost=%s\npath=%s/%s.git\nusername=%s\npassword=%s\n\n", repo.Host, repo.Owner, repo.Name, username, token)
	cmd := exec.Command("git", "-c", "credential.useHttpPath=true", "credential", "approve")
	cmd.Stdin = strings.NewReader(input)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git credential approve: %w\n%s", err, strings.TrimSpace(errb.String()))
	}
	return nil
}

// authErrRe matches git's own words for a rejected credential. Bare status
// numbers are not matched: the URL in the message may contain them.
var authErrRe = regexp.MustCompile(`(?i)authentication failed|could not read username|returned error: 40[13]\b|invalid username or token|write access to repository not granted`)

// AuthError reports whether a git failure was the server rejecting the
// credentials rather than the network or the repository being absent.
func AuthError(err error) bool {
	return err != nil && authErrRe.MatchString(err.Error())
}

// VerifyHTTPS checks the token against the repository without touching any
// credential helper, so a rejected attempt cannot make git erase a stored
// credential. The token reaches git through GIT_ASKPASS reading an
// environment variable: never on a command line, never in a file. The
// username rides in the URL so that askpass is only ever asked for the
// password; otherwise git asks for the username first and the token would be
// answered there and end up in URLs and logs. An empty repository is fine:
// without --exit-code, no refs is still exit 0.
func VerifyHTTPS(repo Repo, username, token string) error {
	dir, err := os.MkdirTemp("", "cclayer-askpass-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "askpass.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$CCLAYER_TOKEN\"\n"
	if isWindows() {
		script = filepath.Join(dir, "askpass.cmd")
		body = "@echo %CCLAYER_TOKEN%\r\n"
	}
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("git", "-c", "credential.helper=", "-c", "credential.useHttpPath=true", "ls-remote", "--heads", httpsURLWithUser(repo, username))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS="+script, "CCLAYER_TOKEN="+token)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git ls-remote %s: %w\n%s", HTTPSURL(repo), err, strings.TrimSpace(errb.String()))
	}
	return nil
}

// RejectCommand is the shell line that drops the stored token again.
func RejectCommand(repo Repo, username string) string {
	return fmt.Sprintf("printf 'protocol=https\\nhost=%s\\npath=%s/%s.git\\nusername=%s\\n\\n' | git -c credential.useHttpPath=true credential reject",
		repo.Host, repo.Owner, repo.Name, username)
}

// HTTPSURL is the clone URL for the token path.
func HTTPSURL(r Repo) string {
	return fmt.Sprintf("https://%s/%s/%s.git", r.Host, r.Owner, r.Name)
}

func httpsURLWithUser(r Repo, username string) string {
	return fmt.Sprintf("https://%s@%s/%s/%s.git", url.PathEscape(username), r.Host, r.Owner, r.Name)
}

// Verify checks that the URL can be reached with the credentials in place.
// HTTPS lookups match by path, the way the token was stored.
func Verify(r Runner, url string) error {
	_, err := r.Run("", "git", "-c", "credential.useHttpPath=true", "ls-remote", "--heads", url)
	return err
}

// TokenPageURL is where a fine-grained token is created; GitHub has no API
// for it.
const TokenPageURL = "https://github.com/settings/personal-access-tokens/new"

// TokenInstructions explains what to pick on the token page for a set of
// repositories under one owner.
func TokenInstructions(owner string, repos []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, i18n.T("Open %s and create a fine-grained token:\n"), TokenPageURL)
	fmt.Fprintf(&b, i18n.T("  Resource owner: %s\n"), owner)
	fmt.Fprintf(&b, i18n.T("  Repository access: Only select repositories -> %s\n"), strings.Join(repos, ", "))
	b.WriteString(i18n.T("  Permissions: Contents = Read and write (Metadata is added automatically)\n"))
	b.WriteString(i18n.T("  Expiration: your choice; cclayer will ask again when git starts failing\n"))
	return b.String()
}
