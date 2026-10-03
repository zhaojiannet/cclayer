package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/claudecli"
	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/manifest"
	"github.com/zhaojiannet/cclayer/internal/state"
)

// Env is everything a command needs from the outside world. Tests build one
// pointing at temporary directories.
type Env struct {
	DevicePath string
	Home       string
	ClaudeDir  string // ~/.claude
	GitConfig  string // ~/.gitconfig
	State      state.Dir
	Runner     claudecli.Runner
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// Interactive is false under --hook: never ask, never block.
	Interactive bool
	// Getenv reads the environment for the language choice; nil reads
	// nothing, which keeps tests in English.
	Getenv func(string) string

	stdin *bufio.Reader
}

// reader returns one buffered reader over Stdin, so consecutive prompts do
// not lose input buffered by an earlier read.
func (e *Env) reader() *bufio.Reader {
	if e.stdin == nil {
		e.stdin = bufio.NewReader(e.Stdin)
	}
	return e.stdin
}

// DefaultEnv wires the real locations. CCLAYER_DEVICE and CCLAYER_STATE
// override the manifest path and the state directory.
func DefaultEnv() (*Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	e := &Env{
		DevicePath:  manifest.DefaultDevicePath(),
		Home:        home,
		ClaudeDir:   filepath.Join(home, ".claude"),
		GitConfig:   filepath.Join(home, ".gitconfig"),
		State:       state.Dir{Path: manifest.DefaultStateDir()},
		Runner:      claudecli.Exec{},
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Interactive: true,
		Getenv:      os.Getenv,
	}
	if p := os.Getenv("CCLAYER_DEVICE"); p != "" {
		e.DevicePath = p
	}
	if p := os.Getenv("CCLAYER_STATE"); p != "" {
		e.State = state.Dir{Path: p}
	}
	return e, nil
}

// claudeAvailable reports whether the claude binary can be run; tests
// replace it.
var claudeAvailable = claudecli.Available

// Layer is a loaded layer: its manifest and where its clone lives.
type Layer struct {
	Name     string
	Dir      string
	Manifest *manifest.Layer
}

// Loaded is the device manifest plus every enabled layer.
type Loaded struct {
	Device *manifest.Device
	Layers []Layer // in application order, base first
}

// Base returns the base layer.
func (l *Loaded) Base() Layer { return l.Layers[0] }

// Overlays returns the overlays in order.
func (l *Loaded) Overlays() []Layer { return l.Layers[1:] }

// Find returns the layer with the given name.
func (l *Loaded) Find(name string) (Layer, bool) {
	for _, x := range l.Layers {
		if x.Name == name {
			return x, true
		}
	}
	return Layer{}, false
}

// Load reads the device manifest and every layer manifest it enables.
func (e *Env) Load() (*Loaded, error) {
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		return nil, err
	}
	out := &Loaded{Device: d}
	for _, name := range d.Layers {
		dir := d.ClonePath(name)
		m, err := manifest.LoadLayer(dir)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w (run cclayer init to clone missing layers)", name, err)
		}
		if m.Layer.Name != name {
			return nil, fmt.Errorf("layer %q: layer.toml says name = %q", name, m.Layer.Name)
		}
		out.Layers = append(out.Layers, Layer{Name: name, Dir: dir, Manifest: m})
	}
	if err := checkOverlayOwners(out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkOverlayOwners refuses two overlays that claim the same host and owner.
// git applies an overlay's identity and fragment through includeIf on the
// remote URL, whatever cclayer decides about a project matched twice, so an
// overlay naming another team's owner would take over that team's
// repositories. Within one overlay any number of patterns may share an owner.
func checkOverlayOwners(l *Loaded) error {
	claimed := map[string]string{}
	for _, o := range l.Overlays() {
		for _, m := range o.Manifest.Match {
			host, owner := manifest.RemoteHostOwner(m.Remote)
			key := strings.ToLower(host) + "/" + strings.ToLower(owner)
			if prev, ok := claimed[key]; ok && prev != o.Name {
				return fmt.Errorf("overlays %s and %s both claim %s; give each owner to one overlay", prev, o.Name, key)
			}
			claimed[key] = o.Name
		}
	}
	return nil
}

// confirm asks a yes/no question. Non-interactive runs answer no.
// confirm asks a yes/no question; format is translated like printf's.
func (e *Env) confirm(format string, args ...any) bool {
	if !e.Interactive {
		return false
	}
	fmt.Fprintf(e.Stdout, "%s [y/N] ", fmt.Sprintf(i18n.T(format), args...))
	line, _ := e.reader().ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

// printf writes a message in the selected language: format is the English
// text and the key of its translation.
func (e *Env) printf(format string, args ...any) {
	fmt.Fprintf(e.Stdout, i18n.T(format), args...)
}

// getenv reads the environment, or nothing when Getenv is unset.
func (e *Env) getenv(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// setLanguage selects the message language from the environment and the
// device manifest; a manifest that does not load yet leaves its lang out.
func (e *Env) setLanguage() {
	configured := ""
	if d, err := manifest.LoadDevice(e.DevicePath); err == nil {
		configured = d.Lang
	}
	i18n.Set(i18n.Detect(e.getenv, configured))
}

func (e *Env) errorf(format string, args ...any) {
	fmt.Fprintf(e.Stderr, format, args...)
}

// tilde shortens a path under home for display.
func (e *Env) tilde(p string) string {
	if strings.HasPrefix(p, e.Home+string(filepath.Separator)) {
		return "~" + p[len(e.Home):]
	}
	return p
}
