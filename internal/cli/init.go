package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/manifest"
	"github.com/zhaojiannet/cclayer/internal/remote"
)

// runInit creates the device manifest when missing, clones every enabled
// layer that is not on disk yet, fills the blocklist from the overlays and
// asks once about auto_pull.
func runInit(e *Env, args []string) error {
	if len(args) > 0 {
		return usagef("init takes no arguments")
	}
	d, err := manifest.LoadDevice(e.DevicePath)
	if os.IsNotExist(underlying(err)) || (err != nil && strings.Contains(err.Error(), "no such file")) {
		if !e.Interactive {
			return fmt.Errorf("no device manifest at %s", e.DevicePath)
		}
		d, err = interactiveDevice(e)
		if err != nil {
			return err
		}
		if err := d.Save(e.DevicePath); err != nil {
			return err
		}
		e.printf("wrote %s\n", e.tilde(e.DevicePath))
	} else if err != nil {
		return err
	}

	if err := cloneLayers(e, d); err != nil {
		return err
	}

	loaded, err := e.Load()
	if err != nil {
		return err
	}
	added := fillBlocklist(loaded)
	if len(added) > 0 {
		e.printf("blocklist += %s\n", strings.Join(added, ", "))
	}
	d.Blocklist = loaded.Device.Blocklist
	if e.Interactive && !d.AutoPull {
		if e.confirm("Pull and apply the layers at the start of every Claude Code session (SessionStart hook)?") {
			d.AutoPull = true
		}
	}
	if err := d.Save(e.DevicePath); err != nil {
		return err
	}
	e.printf("init done; run `cclayer apply` next\n")
	return nil
}

// cloneLayers clones every enabled layer that is not on disk yet. A clone
// path that already holds a layer.toml is used as it is, git repository or
// plain directory.
func cloneLayers(e *Env, d *manifest.Device) error {
	for _, name := range d.Layers {
		dir := d.ClonePath(name)
		if isGitDir(dir) || exists(filepath.Join(dir, "layer.toml")) {
			continue
		}
		url, ok := d.Repo[name]
		if !ok || url == "" {
			return fmt.Errorf("layer %q: %s holds no layer.toml and [repo] has no URL to clone from", name, e.tilde(dir))
		}
		e.printf("cloning %s -> %s\n", url, e.tilde(dir))
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return err
		}
		args := []string{"clone", "--quiet", url, dir}
		if d.Auth[name] == "token" {
			args = append([]string{"-c", "credential.useHttpPath=true"}, args...)
		}
		if _, err := git(filepath.Dir(dir), args...); err != nil {
			return err
		}
	}
	return nil
}

func underlying(err error) error {
	for err != nil {
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
			continue
		}
		return err
	}
	return nil
}

// fillBlocklist adds every overlay's identifying words to the device
// blocklist and returns what it added.
func fillBlocklist(loaded *Loaded) []string {
	d := loaded.Device
	have := map[string]bool{}
	for _, w := range d.Blocklist {
		have[strings.ToLower(w)] = true
	}
	for _, w := range d.BlocklistExcept {
		have[strings.ToLower(strings.TrimSpace(w))] = true
	}
	var added []string
	add := func(w string) {
		w = strings.TrimSpace(w)
		if len([]rune(w)) < 2 || have[strings.ToLower(w)] {
			return
		}
		have[strings.ToLower(w)] = true
		d.Blocklist = append(d.Blocklist, w)
		added = append(added, w)
	}
	for _, o := range loaded.Overlays() {
		add(o.Name)
		if id := o.Manifest.Identity; id != nil {
			add(id.Name)
			if local, domain, ok := strings.Cut(id.Email, "@"); ok {
				add(local)
				add(domain)
			}
		}
		for _, p := range patterns(o) {
			add(remote.OrgSegment(p))
		}
	}
	return added
}

// interactiveDevice asks the few questions a fresh device needs.
func interactiveDevice(e *Env) (*manifest.Device, error) {
	r := e.reader()
	ask := func(prompt, def string) string {
		if def != "" {
			e.printf("%s [%s]: ", i18n.T(prompt), def)
		} else {
			e.printf("%s: ", i18n.T(prompt))
		}
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}
	d := &manifest.Device{Clone: map[string]string{}, Repo: map[string]string{}}
	baseURL := ask("git URL of the base layer", "")
	if baseURL == "" {
		return nil, fmt.Errorf("the base layer URL is required")
	}
	d.Layers = []string{"base"}
	d.Repo["base"] = baseURL
	d.Clone["base"] = d.NewClonePath("base")
	for {
		name := ask("overlay layer name (empty to finish)", "")
		if name == "" {
			break
		}
		url := ask("git URL of this overlay", "")
		if url == "" {
			return nil, fmt.Errorf("the URL of %s is required", name)
		}
		d.Layers = append(d.Layers, name)
		d.Repo[name] = url
		d.Clone[name] = d.NewClonePath(name)
	}
	roots := ask("directories that hold your projects, comma separated", "~/Projects")
	for _, rt := range strings.Split(roots, ",") {
		if rt = strings.TrimSpace(rt); rt != "" {
			d.Roots = append(d.Roots, rt)
		}
	}
	if len(d.Layers) > 1 {
		d.DefaultIdentity = ask("layer whose identity applies to repositories no overlay matches (empty: git refuses to commit there)", "")
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}
