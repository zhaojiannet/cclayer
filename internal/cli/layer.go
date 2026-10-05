package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

const layerUsage = "usage: cclayer layer add <name> <git-url|directory> [--method deploy-key|token|none] [--port-443]"

func runLayer(e *Env, args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return usagef(layerUsage)
	}
	return runLayerAdd(e, args[1:])
}

// runLayerAdd adds one overlay to an existing device manifest: credentials,
// clone, checks, blocklist. It is what setup does for one layer, without
// opening the setup screen. Anything that fails leaves the manifest as it
// was.
func runLayerAdd(e *Env, args []string) error {
	fs := flag.NewFlagSet("layer add", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	method := fs.String("method", "", "deploy-key, token or none; asked when omitted for a repository")
	port443 := fs.Bool("port-443", false, "reach GitHub over ssh.github.com:443 (networks that block port 22)")
	pos, rest := splitPositionals(args, map[string]bool{"--method": true, "-method": true})
	if err := fs.Parse(rest); err != nil {
		return usagef("layer add: %v", err)
	}
	pos = append(pos, fs.Args()...)
	if len(pos) != 2 {
		return usagef(layerUsage)
	}
	name, loc := pos[0], strings.TrimSpace(pos[1])
	switch *method {
	case "", "deploy-key", "token", "none":
	default:
		return usagef("layer add: --method must be deploy-key, token or none")
	}

	before, err := os.ReadFile(e.DevicePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no device manifest at %s yet; run cclayer setup first", e.tilde(e.DevicePath))
		}
		return err
	}
	d, err := manifest.LoadDevice(e.DevicePath)
	if err != nil {
		return err
	}
	d.EnsureMaps()
	if err := validLayerName(d.Layers)(name); err != nil {
		return usagef("layer add: %v", err)
	}

	kind, err := classifyLocation(loc)
	if err != nil {
		return err
	}
	repo := ""
	switch kind {
	case locURL:
		repo = loc
		d.Clone[name] = d.NewClonePath(name)
	case locGitDir:
		repo = manifest.ExpandHome(localPath(loc))
		d.Clone[name] = d.NewClonePath(name)
	case locLayerDir:
		d.Clone[name] = localPath(loc)
	case locNew:
		return fmt.Errorf("nothing at %s; give a repository, or a directory that holds a layer.toml (cclayer setup can write a starter one)", loc)
	}
	if repo != "" {
		d.Repo[name] = repo
	}
	d.Layers = append(d.Layers, name)

	// from here on a failure restores the manifest and removes a fresh clone
	clone := d.ClonePath(name)
	cloneExisted := exists(clone)
	credentialSet := false
	undo := func(cause error) error {
		if credentialSet {
			if rerr := removeCredential(e, d, name, false); rerr != nil {
				e.errorf("could not remove the credential set up for %s: %v\n", name, rerr)
			}
		}
		if werr := os.WriteFile(e.DevicePath, before, 0o600); werr != nil {
			return fmt.Errorf("%w (and the device manifest could not be restored: %v)", cause, werr)
		}
		if repo != "" && !cloneExisted {
			os.RemoveAll(clone)
		}
		return cause
	}

	if repo != "" && isRemoteURL(repo) && *method != "none" {
		m, token, err := askCredential(e, repo, *method)
		if err != nil {
			return undo(err)
		}
		res, err := setupCredential(e, d, name, repo, m, *port443, token)
		if err != nil {
			return undo(err)
		}
		credentialSet = true
		e.printf("%s", res)
	}
	if err := d.Save(e.DevicePath); err != nil {
		return undo(err)
	}
	if err := cloneLayers(e, d); err != nil {
		return undo(err)
	}
	loaded, err := e.Load()
	if err != nil {
		return undo(err)
	}
	if added := fillBlocklist(loaded); len(added) > 0 {
		e.printf("blocklist += %s\n", strings.Join(added, ", "))
	}
	if err := loaded.Device.Save(e.DevicePath); err != nil {
		return undo(err)
	}
	e.printf("layer %s added; run cclayer apply next\n", name)
	return nil
}

// splitPositionals separates positional arguments from flags, so they may
// come in any order. takesValue names the flags whose value is the next
// argument.
func splitPositionals(args []string, takesValue map[string]bool) (pos, flags []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(pos, args[i+1:]...), flags
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
			if takesValue[a] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			pos = append(pos, a)
		}
	}
	return pos, flags
}
