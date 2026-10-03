// Package manifest defines the two TOML documents cclayer reads: the device
// manifest at ~/.config/cclayer/device.toml and the layer.toml at the root
// of every layer repository.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/zhaojiannet/cclayer/internal/gitconf"
)

// Device is ~/.config/cclayer/device.toml. It never leaves the machine.
type Device struct {
	// Layers lists the enabled layers in application order; "base" first.
	Layers []string `toml:"layers"`
	// DefaultIdentity names the one layer whose [user] applies when no
	// overlay matches. Empty means git refuses to commit in such repositories.
	DefaultIdentity string `toml:"default_identity"`
	// Roots are the directories scanned for projects.
	Roots []string `toml:"roots"`
	// AutoPull turns the SessionStart hook from a no-op into apply --hook.
	AutoPull bool `toml:"auto_pull"`
	// Blocklist holds words that must never enter the base layer.
	Blocklist []string `toml:"blocklist"`
	// BlocklistExcept names words the base layer may carry although an
	// overlay would put them on the blocklist, such as the owner of a
	// public plugin marketplace that is also an overlay's owner.
	BlocklistExcept []string `toml:"blocklist_except,omitempty"`
	// TrustExec names layers whose git fragments may set keys that make git
	// run programs (core.hooksPath, credential.helper, shell aliases, ...).
	// Without it such a fragment is refused.
	TrustExec []string `toml:"trust_exec"`
	// Clone maps a layer name to its local clone path.
	Clone map[string]string `toml:"clone"`
	// Repo maps a layer name to the git URL init clones it from.
	Repo map[string]string `toml:"repo,omitempty"`
	// Auth records how this device authenticates to each layer repository:
	// "deploy-key" (ssh alias and key under ~/.ssh) or "token" (HTTPS with a
	// token in the system credential helper). Absent means whatever git has.
	Auth map[string]string `toml:"auth,omitempty"`
	// KeyHost and KeyPort443 remember, per deploy-key layer, the real git
	// host behind the ssh alias and whether to reach it over port 443.
	KeyHost    map[string]string `toml:"key_host,omitempty"`
	KeyPort443 map[string]bool   `toml:"key_port443,omitempty"`
	// Profiles turns on one CLAUDE_CONFIG_DIR per overlay under ProfileDir.
	Profiles bool `toml:"profiles"`
	// ProfileDir holds the per-overlay config directories; default
	// ~/.claude-profiles.
	ProfileDir string `toml:"profile_dir"`
	// Lang is the language of cclayer's messages: en, zh or ja. Empty
	// follows CCLAYER_LANG and the locale.
	Lang string `toml:"lang,omitempty"`
}

// ProfilePath returns the config directory for an overlay when profiles
// are on.
func (d *Device) ProfilePath(layer string) string {
	dir := d.ProfileDir
	if dir == "" {
		dir = "~/.claude-profiles"
	}
	return filepath.Join(ExpandHome(dir), layer)
}

// Layer is the layer.toml at the root of a layer repository.
type Layer struct {
	Layer    LayerMeta `toml:"layer"`
	Claude   *Claude   `toml:"claude"`
	Identity *Identity `toml:"identity"`
	Match    []Match   `toml:"match"`
	Inject   *Inject   `toml:"inject"`
	Git      *Git      `toml:"git"`
}

// LayerMeta names the layer and says whether it is the base or an overlay.
type LayerMeta struct {
	Name string `toml:"name"`
	Kind string `toml:"kind"` // "base" or "overlay"
	// Private marks a base layer kept in a private place for one person:
	// check lets email addresses through. Overlay names (the blocklist) and
	// absolute home paths are still refused, as the base still reaches
	// every device, including ones another organization handed out.
	Private bool `toml:"private,omitempty"`
}

// Claude is allowed in the base layer only: what it writes under ~/.claude.
type Claude struct {
	Paths        []string `toml:"paths"`
	SettingsKeys []string `toml:"settings_keys"`
	Ignore       []string `toml:"ignore"`
}

// Identity is the git identity an overlay applies to the projects it matches.
type Identity struct {
	Name  string `toml:"name"`
	Email string `toml:"email"`
}

// Match is one remote URL pattern.
type Match struct {
	Remote string `toml:"remote"`
}

// Inject describes what an overlay writes into matched project directories.
type Inject struct {
	SettingsKeys []string `toml:"settings_keys"`
	ClaudeLocal  string   `toml:"claude_local"`
}

// Git points at the layer's raw git config fragment.
type Git struct {
	Fragment string `toml:"fragment"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// LoadDevice reads and validates a device manifest.
func LoadDevice(path string) (*Device, error) {
	var d Device
	if _, err := toml.DecodeFile(path, &d); err != nil {
		return nil, fmt.Errorf("device manifest %s: %w", path, err)
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("device manifest %s: %w", path, err)
	}
	return &d, nil
}

// Validate checks the device manifest for the mistakes that would make
// apply do something surprising.
func (d *Device) Validate() error {
	if len(d.Layers) == 0 {
		return fmt.Errorf("layers is empty")
	}
	if d.Layers[0] != "base" {
		return fmt.Errorf("the first layer must be \"base\", got %q", d.Layers[0])
	}
	switch d.Lang {
	case "", "en", "zh", "ja":
	default:
		return fmt.Errorf("lang %q: use en, zh or ja, or leave it out to follow the locale", d.Lang)
	}
	seen := map[string]bool{}
	for _, l := range d.Layers {
		if !nameRe.MatchString(l) {
			return fmt.Errorf("layer name %q: use lower-case letters, digits and hyphens", l)
		}
		if seen[l] {
			return fmt.Errorf("layer %q listed twice", l)
		}
		seen[l] = true
		if _, ok := d.Clone[l]; !ok {
			return fmt.Errorf("layer %q has no clone path under [clone]", l)
		}
	}
	if d.DefaultIdentity != "" && !seen[d.DefaultIdentity] {
		return fmt.Errorf("default_identity %q is not an enabled layer", d.DefaultIdentity)
	}
	if d.DefaultIdentity == "base" {
		return fmt.Errorf("default_identity cannot be the base layer")
	}
	for _, t := range d.TrustExec {
		if !seen[t] {
			return fmt.Errorf("trust_exec names %q, which is not an enabled layer", t)
		}
	}
	for l, m := range d.Auth {
		if m != "deploy-key" && m != "token" {
			return fmt.Errorf("auth.%s must be \"deploy-key\" or \"token\", got %q", l, m)
		}
	}
	if len(d.Roots) == 0 {
		return fmt.Errorf("roots is empty; list the directories that hold your projects")
	}
	return nil
}

// Save writes the device manifest atomically, creating its directory.
func (d *Device) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// a manifest kept by a dotfiles manager may be a symlink; write its target
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(d); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureMaps makes every optional table non-nil so callers can assign.
func (d *Device) EnsureMaps() {
	if d.Clone == nil {
		d.Clone = map[string]string{}
	}
	if d.Repo == nil {
		d.Repo = map[string]string{}
	}
	if d.Auth == nil {
		d.Auth = map[string]string{}
	}
	if d.KeyHost == nil {
		d.KeyHost = map[string]string{}
	}
	if d.KeyPort443 == nil {
		d.KeyPort443 = map[string]bool{}
	}
}

// RemoveLayer drops a layer from the manifest and clears default_identity
// when it named that layer. It reports whether default_identity was cleared.
func (d *Device) RemoveLayer(name string) (clearedDefault bool) {
	var kept []string
	for _, l := range d.Layers {
		if l != name {
			kept = append(kept, l)
		}
	}
	d.Layers = kept
	delete(d.Clone, name)
	delete(d.Repo, name)
	delete(d.Auth, name)
	delete(d.KeyHost, name)
	delete(d.KeyPort443, name)
	var trusted []string
	for _, t := range d.TrustExec {
		if t != name {
			trusted = append(trusted, t)
		}
	}
	d.TrustExec = trusted
	if d.DefaultIdentity == name {
		d.DefaultIdentity = ""
		clearedDefault = true
	}
	return clearedDefault
}

// Trusted reports whether a layer's git fragment may contain keys that run
// programs.
func (d *Device) Trusted(layer string) bool {
	for _, t := range d.TrustExec {
		if t == layer {
			return true
		}
	}
	return false
}

// ClonePath returns the expanded local clone path of a layer.
func (d *Device) ClonePath(layer string) string {
	return ExpandHome(d.Clone[layer])
}

// LoadLayer reads and validates the layer.toml inside a clone.
func LoadLayer(cloneDir string) (*Layer, error) {
	path := filepath.Join(cloneDir, "layer.toml")
	var l Layer
	if _, err := toml.DecodeFile(path, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := l.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &l, nil
}

// Validate enforces the base/overlay split from the design: only the base
// writes under ~/.claude, only overlays carry identities and matches.
func (l *Layer) Validate() error {
	if !nameRe.MatchString(l.Layer.Name) {
		return fmt.Errorf("layer.name %q: use lower-case letters, digits and hyphens", l.Layer.Name)
	}
	switch l.Layer.Kind {
	case "base":
		if l.Layer.Name != "base" {
			return fmt.Errorf("a base layer must be named \"base\"")
		}
		if l.Identity != nil || len(l.Match) > 0 || l.Inject != nil {
			return fmt.Errorf("the base layer cannot have [identity], [[match]] or [inject]")
		}
		if l.Claude != nil {
			for _, k := range l.Claude.SettingsKeys {
				if !settingsKeyRe.MatchString(k) {
					return fmt.Errorf("settings key %q: write a key path such as theme or permissions.deny", k)
				}
				if isDeviceKey(k) {
					return fmt.Errorf("settings key %q belongs to the device and cannot be owned by a layer", k)
				}
			}
			for _, p := range l.Claude.Paths {
				if err := checkClaudePath(p); err != nil {
					return fmt.Errorf("[claude] paths: %w", err)
				}
			}
			for _, p := range l.Claude.Ignore {
				if err := checkRelPath(p); err != nil {
					return fmt.Errorf("[claude] ignore: %w", err)
				}
			}
		}
	case "overlay":
		if l.Layer.Private {
			return fmt.Errorf("private applies to the base layer only; an overlay is private by keeping its repository private")
		}
		if l.Claude != nil {
			return fmt.Errorf("an overlay cannot have a [claude] section; overlays never write under ~/.claude")
		}
		if l.Identity == nil || l.Identity.Email == "" || l.Identity.Name == "" {
			return fmt.Errorf("an overlay needs [identity] with name and email")
		}
		if err := CheckIdentityName(l.Identity.Name); err != nil {
			return fmt.Errorf("[identity] name: %w", err)
		}
		if err := CheckIdentityEmail(l.Identity.Email); err != nil {
			return fmt.Errorf("[identity] email: %w", err)
		}
		if len(l.Match) == 0 {
			return fmt.Errorf("an overlay needs at least one [[match]]")
		}
		for _, m := range l.Match {
			if strings.TrimSpace(m.Remote) == "" {
				return fmt.Errorf("[[match]] remote is empty")
			}
			// the pattern is written into an includeIf header of the generated
			// git configuration; a quote or newline there would open a section
			if err := checkIdentityValue(m.Remote); err != nil || strings.ContainsAny(m.Remote, " \t") {
				return fmt.Errorf("[[match]] remote %q contains a character not allowed in a git config value", m.Remote)
			}
			// an overlay may claim one owner's repositories, never every
			// owner's: github.com/* would pull other teams' projects in
			if host, owner := remoteHostOwner(m.Remote); host == "" || owner == "" || strings.ContainsAny(host+owner, "*?[") {
				return fmt.Errorf("[[match]] remote %q must name the host and the owner literally, as in github.com/acme-inc/*", m.Remote)
			}
		}
		if l.Inject != nil {
			for _, k := range l.Inject.SettingsKeys {
				if !settingsKeyRe.MatchString(k) {
					return fmt.Errorf("inject key %q: write a key path such as permissions.deny", k)
				}
				if isDeviceKey(k) {
					return fmt.Errorf("inject key %q is never written by cclayer", k)
				}
			}
			if l.Inject.ClaudeLocal != "" {
				if err := checkRelPath(l.Inject.ClaudeLocal); err != nil {
					return fmt.Errorf("[inject] claude_local: %w", err)
				}
			}
		}
	default:
		return fmt.Errorf("layer.kind must be \"base\" or \"overlay\", got %q", l.Layer.Kind)
	}
	if l.Git != nil && l.Git.Fragment != "" {
		if err := checkRelPath(l.Git.Fragment); err != nil {
			return fmt.Errorf("[git] fragment: %w", err)
		}
	}
	return nil
}

// runtimeNames are entries under ~/.claude that hold runtime state. A layer
// may never mirror them, whatever the base repository says.
var runtimeNames = map[string]bool{
	"projects": true, "plugins": true, "file-history": true, "history.jsonl": true,
	".credentials.json": true, ".claude.json": true, "sessions": true, "backups": true,
	"cache": true, "debug": true, "paste-cache": true, "shell-snapshots": true,
	"stats-cache.json": true, "todos": true, "tasks": true, "agent-memory": true,
	"jobs": true, "daemon": true, "settings.json": true, "settings.local.json": true,
}

// settingsKeyRe is a settings key path: a top-level key, then dotted
// segments. An empty or odd path could otherwise name the whole file or a
// key no gate recognizes.
var settingsKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z0-9_@:-]+)*$`)

// remoteHostOwner returns the host and the first path segment of a match
// pattern written as a URL, an scp address or host/owner/path.
func remoteHostOwner(pattern string) (host, owner string) {
	return RemoteHostOwner(pattern)
}

// BlockedWords is the blocklist check applies: the blocklist without the
// exceptions and without single characters, which identify nobody and
// would refuse every "-m" or "x" in the base layer.
func (d *Device) BlockedWords() []string {
	except := map[string]bool{}
	for _, w := range d.BlocklistExcept {
		except[strings.ToLower(strings.TrimSpace(w))] = true
	}
	var out []string
	for _, w := range d.Blocklist {
		w = strings.TrimSpace(w)
		if len([]rune(w)) < 2 || except[strings.ToLower(w)] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// RemoteHostOwner returns the host and the first path segment of a match
// pattern written as a URL, an scp address or host/owner/path.
func RemoteHostOwner(pattern string) (host, owner string) {
	s := strings.TrimSpace(pattern)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
		s = s[:i] + "/" + s[i+1:]
	}
	if i := strings.Index(s, "@"); i >= 0 && (!strings.Contains(s, "/") || i < strings.Index(s, "/")) {
		s = s[i+1:]
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// checkIdentityValue rejects characters that would let a value break out
// of its line in a generated git config.
func checkIdentityValue(v string) error {
	if !gitconf.ValueOK(v) {
		return fmt.Errorf("%q contains a character not allowed in a git config value", v)
	}
	return nil
}

// CheckIdentityName is the [identity] name rule. The setup wizard validates
// its input with the same functions LoadLayer applies, so a starter overlay
// it writes is never refused a moment later.
func CheckIdentityName(v string) error { return checkIdentityValue(v) }

// CheckIdentityEmail is the [identity] email rule.
func CheckIdentityEmail(v string) error {
	if err := checkIdentityValue(v); err != nil {
		return err
	}
	if strings.Count(v, "@") != 1 || strings.ContainsAny(v, " \t") {
		return fmt.Errorf("%q is not an address", v)
	}
	return nil
}

// checkRelPath accepts only a clean relative path that stays inside its root.
func checkRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsAny(p, "\\\x00") {
		return fmt.Errorf("%q must use forward slashes", p)
	}
	slash := filepath.ToSlash(p)
	if filepath.IsAbs(p) || strings.HasPrefix(slash, "/") || strings.HasPrefix(slash, "~") {
		return fmt.Errorf("%q must be relative", p)
	}
	clean := filepath.ToSlash(filepath.Clean(slash))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%q escapes the layer", p)
	}
	trimmed := strings.TrimSuffix(slash, "/")
	if filepath.ToSlash(filepath.Clean(trimmed)) != trimmed {
		return fmt.Errorf("%q is not a clean path", p)
	}
	// Windows resolves names its own way before the runtime-name check could
	// see them: a trailing dot or space is dropped, ":" opens an alternate
	// data stream, "~" with digits may be an 8.3 short name of another entry
	for _, seg := range strings.Split(trimmed, "/") {
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") || strings.ContainsAny(seg, ":~*?\"<>|") {
			return fmt.Errorf("%q: path segment %q would resolve differently on some file systems", p, seg)
		}
	}
	return nil
}

// checkClaudePath is checkRelPath plus the runtime-name ban for paths that
// are mirrored into ~/.claude. settings.json is excluded too: it is merged
// by key, never mirrored.
func checkClaudePath(p string) error {
	if err := checkRelPath(p); err != nil {
		return err
	}
	// case-insensitive file systems make Projects/ the same directory as
	// projects/; they fold case the Unicode way (ſ to s, for one), which
	// EqualFold follows and ToLower does not
	first := strings.SplitN(strings.TrimSuffix(filepath.ToSlash(p), "/"), "/", 2)[0]
	for name := range runtimeNames {
		if strings.EqualFold(first, name) {
			return fmt.Errorf("%q is runtime state or merged by key; it cannot be mirrored", p)
		}
	}
	return nil
}

// deviceKeys are settings paths that stay on the device whatever any layer
// says. permissions.allow is included because Claude Code writes approvals
// there.
var deviceKeys = []string{"env", "permissions.defaultMode", "permissions.allow", "permissions.ask"}

func isDeviceKey(k string) bool {
	for _, d := range deviceKeys {
		if k == d || strings.HasPrefix(k, d+".") || strings.HasPrefix(d, k+".") {
			return true
		}
	}
	return false
}

// ExpandHome replaces a leading "~/" with the home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// DefaultDevicePath is where the device manifest lives unless overridden.
func DefaultDevicePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "cclayer", "device.toml")
	}
	return ExpandHome("~/.config/cclayer/device.toml")
}

// DefaultStateDir is where hashes, the project map, the lock and backups live.
func DefaultStateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "cclayer")
	}
	return ExpandHome("~/.local/state/cclayer")
}
