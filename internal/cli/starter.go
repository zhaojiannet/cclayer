package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// A layer location typed into setup is one of these.
type locationKind int

const (
	locURL      locationKind = iota // a git URL or scp-style address: cloned
	locLayerDir                     // a directory holding layer.toml: used in place
	locGitDir                       // a git repository without layer.toml (bare, usually): cloned
	locNew                          // nothing there yet, or an empty directory: a starter layer can be written
	locBad                          // an existing directory with other content
)

var scpRe = regexp.MustCompile(`^[^/@:\s]+@[^/:\s]+:.+`)

// isRemoteURL reports whether s names a repository on another host, the only
// kind of layer source that can need credentials.
func isRemoteURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "://") && !strings.HasPrefix(s, "file://") || scpRe.MatchString(s)
}

// localPath turns a typed directory into what the device manifest stores:
// `~` is kept for the manifest to expand, anything else becomes absolute so
// the hook, which runs in a project directory, resolves the same place.
func localPath(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "file://")
	if s == "~" || strings.HasPrefix(s, "~/") || filepath.IsAbs(s) {
		return s
	}
	if abs, err := filepath.Abs(s); err == nil {
		return abs
	}
	return s
}

// classifyLocation decides what setup does with a layer address.
func classifyLocation(s string) (locationKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return locBad, errors.New("enter a git URL or a directory")
	}
	if isRemoteURL(s) {
		return locURL, nil
	}
	dir := manifest.ExpandHome(strings.TrimPrefix(s, "file://"))
	fi, err := os.Stat(dir)
	switch {
	case err != nil && os.IsNotExist(err):
		// setup asks before it creates anything, missing parents included.
		// The nearest existing ancestor must be a directory: Windows reports
		// a path below a file as missing rather than as not a directory.
		for up := filepath.Dir(dir); ; up = filepath.Dir(up) {
			if fi, err := os.Stat(up); err == nil {
				if !fi.IsDir() {
					return locBad, fmt.Errorf("%s: %s is a file, not a directory", s, up)
				}
				break
			}
			if filepath.Dir(up) == up {
				break
			}
		}
		return locNew, nil
	case err != nil:
		return locBad, err
	case !fi.IsDir():
		return locBad, fmt.Errorf("%s is a file, not a directory or URL", s)
	}
	if exists(filepath.Join(dir, "layer.toml")) {
		return locLayerDir, nil
	}
	if exists(filepath.Join(dir, ".git")) || exists(filepath.Join(dir, "HEAD")) {
		return locGitDir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return locBad, err
	}
	if len(entries) == 0 {
		return locNew, nil
	}
	return locBad, fmt.Errorf("%s holds neither a layer.toml nor a git repository", s)
}

// validLocation is the input validator: anything setup can act on passes,
// including a path that does not exist yet.
func validLocation(s string) error {
	_, err := classifyLocation(s)
	return err
}

// starterBase writes the layer.toml of a new base layer into dir. The
// paths and settings keys are the ones every device wants; capture --add
// fills the directory from ~/.claude afterwards.
func starterBase(dir string) error {
	return writeStarter(dir, `[layer]
name = "base"
kind = "base"
# private = true   # only you use this layer and it lives somewhere private:
#                  # check then lets email addresses through

[claude]
paths = ["CLAUDE.md", "rules/", "skills/", "output-styles/", "agents/", "hooks/"]
settings_keys = ["attribution", "permissions.deny", "hooks", "statusLine",
  "enabledPlugins", "extraKnownMarketplaces", "outputStyle", "effortLevel", "theme"]
ignore = ["skills/.trash/"]
`)
}

// starterOverlay writes the layer.toml of a new overlay into dir.
func starterOverlay(dir, name, fullName, email, remote string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "[layer]\nname = %q\nkind = \"overlay\"\n\n", name)
	fmt.Fprintf(&b, "[identity]\nname = %q\nemail = %q\n\n", fullName, email)
	fmt.Fprintf(&b, "[[match]]\nremote = %q\n\n", remote)
	b.WriteString("[inject]\nsettings_keys = [\"permissions.deny\", \"enabledPlugins\", \"extraKnownMarketplaces\"]\nclaude_local = \"project/CLAUDE.local.md\"\n")
	if err := writeStarter(dir, b.String()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "project"), 0o755); err != nil {
		return err
	}
	for file, body := range map[string]string{
		"settings.local.json": "{}\n",
		"CLAUDE.local.md":     "<!-- cclayer:begin -->\n<!-- cclayer:end -->\n",
	} {
		p := filepath.Join(dir, "project", file)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeStarter creates dir and its layer.toml, refusing to replace one.
func writeStarter(dir, layerToml string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "layer.toml"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(layerToml); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := manifest.LoadLayer(dir); err != nil {
		// a refused layer.toml must not stay behind: setup would treat the
		// directory as an existing layer next time and fail on the same file
		os.Remove(filepath.Join(dir, "layer.toml"))
		return err
	}
	return nil
}
