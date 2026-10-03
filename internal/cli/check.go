package cli

import (
	"fmt"
	"path/filepath"

	"github.com/zhaojiannet/cclayer/internal/check"
	"github.com/zhaojiannet/cclayer/internal/gitconf"
)

func runCheck(e *Env, args []string) error {
	if len(args) > 0 {
		return usagef("check takes no arguments")
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	findings, err := checkLayers(e, loaded)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		e.printf("check: nothing to refuse in %d layer(s)\n", len(loaded.Layers))
		return nil
	}
	for _, f := range findings {
		e.printf("%s\n", f)
	}
	return fmt.Errorf("check: %d finding(s)", len(findings))
}

// checkLayers runs the rules over every layer clone. Findings carry the
// layer name in their file path.
func checkLayers(e *Env, loaded *Loaded) ([]check.Finding, error) {
	var all []check.Finding
	for _, l := range loaded.Layers {
		opt := check.Options{Base: l.Name == "base", Private: l.Name == "base" && l.Manifest.Layer.Private, Blocklist: loaded.Device.BlockedWords(), HomeDir: e.Home}
		fs, err := check.Tree(l.Dir, opt)
		if err != nil {
			return nil, err
		}
		for i := range fs {
			fs[i].File = filepath.Join(l.Name, fs[i].File)
		}
		all = append(all, fs...)
		if frag, err := fragment(l); err != nil {
			return nil, err
		} else if frag != "" {
			if err := gitconf.CheckFragment(frag, loaded.Device.Trusted(l.Name)); err != nil {
				all = append(all, check.Finding{File: filepath.Join(l.Name, l.Manifest.Git.Fragment), Rule: err.Error()})
			}
		}
	}
	return all, nil
}
