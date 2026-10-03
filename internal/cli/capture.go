package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/check"
	"github.com/zhaojiannet/cclayer/internal/fsx"
	"github.com/zhaojiannet/cclayer/internal/inject"
	"github.com/zhaojiannet/cclayer/internal/mirror"
	"github.com/zhaojiannet/cclayer/internal/project"
	"github.com/zhaojiannet/cclayer/internal/settings"
	"github.com/zhaojiannet/cclayer/internal/state"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// captureReport collects what capture did for the summary.
type captureReport struct {
	updated, candidates, conflicts, refused []string
}

func runCapture(e *Env, args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	var add multiFlag
	fs.Var(&add, "add", "admit a device-only file or directory (path relative to ~/.claude); repeatable")
	from := fs.String("from", "", "take injected-file changes from this project only")
	if err := fs.Parse(args); err != nil {
		return usagef("capture: %v", err)
	}
	for i, a := range add {
		dir := strings.HasSuffix(filepath.ToSlash(a), "/")
		clean := filepath.ToSlash(filepath.Clean(a))
		if filepath.IsAbs(a) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(filepath.ToSlash(a), "/") {
			return usagef("capture: --add %q must be a clean path relative to ~/.claude", a)
		}
		// a directory admits every device-only file below it
		if fi, err := os.Stat(filepath.Join(e.ClaudeDir, clean)); dir || (err == nil && fi.IsDir()) {
			clean += "/"
		}
		add[i] = clean
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	if err := checkAdds(add, loaded.Base()); err != nil {
		return err
	}
	applied, err := e.State.Load()
	if err != nil {
		return err
	}
	base := loaded.Base()
	var rep captureReport
	baseOpt := check.Options{Base: true, Private: base.Manifest.Layer.Private, Blocklist: loaded.Device.BlockedWords(), HomeDir: e.Home}
	allowBase := func(rel string, content []byte) error {
		if fs := check.File("claude/"+rel, content, baseOpt); len(fs) > 0 {
			return fmt.Errorf("%s", fs[0].Rule)
		}
		return nil
	}

	// 1. mirrored directories and files
	if base.Manifest.Claude != nil {
		for _, p := range base.Manifest.Claude.Paths {
			src, err := layerPath(base.Dir, "claude/"+p)
			if err != nil {
				return err
			}
			dst := filepath.Join(e.ClaudeDir, p)
			if strings.HasSuffix(p, "/") {
				var adds []string
				for _, a := range add {
					if a == p {
						adds = append(adds, "/") // the whole mirrored directory
					} else if rel, ok := strings.CutPrefix(a, p); ok {
						adds = append(adds, rel)
					}
				}
				res, err := mirror.Capture(src, dst, mirror.CaptureOptions{
					Ignore: scopedIgnore(base.Manifest.Claude.Ignore, p), Admit: adds, LastApplied: applied.Hashes,
					Allow: func(rel string, content []byte) error { return allowBase(p+rel, content) },
				})
				if err != nil {
					return err
				}
				for _, x := range res.Updated {
					rep.updated = append(rep.updated, "base: claude/"+p+x)
				}
				for _, x := range res.Candidates {
					rep.candidates = append(rep.candidates, p+x)
				}
				for _, x := range res.Conflicts {
					rep.conflicts = append(rep.conflicts, "claude/"+p+x)
				}
				for _, x := range res.Refused {
					rep.refused = append(rep.refused, "claude/"+p+x)
				}
				continue
			}
			if err := captureFile(src, dst, base.Dir, applied.Hashes, allowBase, p, &rep); err != nil {
				return err
			}
		}
		// 2. owned settings keys, only when the device file changed since apply
		if keys := base.Manifest.Claude.SettingsKeys; len(keys) > 0 {
			target := filepath.Join(e.ClaudeDir, "settings.json")
			cur, err := state.HashFile(target)
			if err != nil {
				return err
			}
			if cur != "" && cur != applied.Hashes[target] {
				device, err := settings.Load(target)
				if err != nil {
					return err
				}
				layerDoc, err := baseSettings(base)
				if err != nil {
					return err
				}
				if err := refuseOverlayPlugins(loaded, device, layerDoc); err != nil {
					return err
				}
				if changed := settings.Merge(layerDoc, device, keys); len(changed) > 0 {
					b, err := layerDoc.Marshal()
					if err != nil {
						return err
					}
					if fs := check.File("claude/settings.json", b, baseOpt); len(fs) > 0 {
						rep.refused = append(rep.refused, "claude/settings.json: "+fs[0].String())
					} else {
						if err := fsx.WriteFile(base.Dir, filepath.Join(base.Dir, "claude", "settings.json"), b, 0o644); err != nil {
							return err
						}
						rep.updated = append(rep.updated, "base: claude/settings.json ("+strings.Join(changed, ", ")+")")
					}
				}
			}
		}
	}

	// 3. overlays: injected keys and managed blocks, consistent across projects
	if err := captureOverlays(e, loaded, *from, &rep); err != nil {
		return err
	}

	sort.Strings(rep.updated)
	if len(rep.updated) == 0 {
		e.printf("capture: no local changes to write back\n")
	} else {
		e.printf("Written to the layers (review; commit with git where the layer is a repository):\n")
		for _, u := range rep.updated {
			e.printf("  %s\n", u)
		}
	}
	if len(rep.candidates) > 0 {
		sort.Strings(rep.candidates)
		e.printf("Device-only files, not captured (use --add <path> to admit one):\n")
		for _, c := range rep.candidates {
			e.printf("  %s\n", c)
		}
	}
	if len(rep.conflicts) > 0 {
		e.printf("Changed both here and in the layer since the last apply, not captured (run apply first):\n")
		for _, c := range rep.conflicts {
			e.printf("  %s\n", c)
		}
	}
	for _, l := range loaded.Layers {
		if !cloned(loaded.Device, l.Name, l.Dir) {
			continue
		}
		if out, err := git(l.Dir, "status", "--short"); err == nil && out != "" {
			e.printf("\n%s (%s):\n%s\n", l.Name, e.tilde(l.Dir), indent(out))
		}
	}
	if len(rep.refused) > 0 {
		for _, r := range rep.refused {
			e.errorf("refused: %s\n", r)
		}
		return fmt.Errorf("check refused %d item(s); they were not written", len(rep.refused))
	}
	return nil
}

// captureFile is Capture for one mirrored file such as CLAUDE.md.
func captureFile(src, dst, layerRoot string, lastApplied map[string]string, allow func(string, []byte) error, rel string, rep *captureReport) error {
	content, err := os.ReadFile(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cur := state.Hash(content)
	layerHash, err := state.HashFile(src)
	if err != nil {
		return err
	}
	last := lastApplied[dst]
	if cur == layerHash || cur == last {
		return nil
	}
	if last != "" && layerHash != last {
		rep.conflicts = append(rep.conflicts, "claude/"+rel)
		return nil
	}
	if err := allow(rel, content); err != nil {
		rep.refused = append(rep.refused, "claude/"+rel+": "+err.Error())
		return nil
	}
	if err := copyInto(dst, src, layerRoot); err != nil {
		return err
	}
	rep.updated = append(rep.updated, "base: claude/"+rel)
	return nil
}

// refuseOverlayPlugins stops a plugin or marketplace declared by an overlay
// at project level from being captured into the base at user level.
func refuseOverlayPlugins(loaded *Loaded, device, layerDoc settings.Doc) error {
	overlayOwned := map[string]string{}
	for _, o := range loaded.Overlays() {
		tmpl, err := overlayTemplate(o)
		if err != nil {
			return err
		}
		m, p := pluginsFrom(tmpl)
		for _, x := range m {
			overlayOwned["marketplace "+x.Name] = o.Name
		}
		for _, x := range p {
			overlayOwned["plugin "+x] = o.Name
		}
	}
	if len(overlayOwned) == 0 {
		return nil
	}
	dm, dp := pluginsFrom(device)
	lm, lp := pluginsFrom(layerDoc)
	have := map[string]bool{}
	for _, x := range lm {
		have["marketplace "+x.Name] = true
	}
	for _, x := range lp {
		have["plugin "+x] = true
	}
	for _, x := range dm {
		if o, ok := overlayOwned["marketplace "+x.Name]; ok && !have["marketplace "+x.Name] {
			return fmt.Errorf("marketplace %s belongs to overlay %s and must not enter the base; remove it from ~/.claude/settings.json or install it with --scope local", x.Name, o)
		}
	}
	for _, x := range dp {
		if o, ok := overlayOwned["plugin "+x]; ok && !have["plugin "+x] {
			return fmt.Errorf("plugin %s belongs to overlay %s and must not enter the base; remove it from ~/.claude/settings.json or install it with --scope local", x, o)
		}
	}
	return nil
}

// captureOverlays writes injected-key and managed-block changes back when
// every matched project agrees, or when --from names the source.
func captureOverlays(e *Env, loaded *Loaded, from string, rep *captureReport) error {
	overlays := loaded.Overlays()
	if len(overlays) == 0 {
		return nil
	}
	var roots []string
	for _, r := range loaded.Device.Roots {
		roots = append(roots, expand(e, r))
	}
	projects, err := project.Discover(roots)
	if err != nil {
		return err
	}
	var pov []project.Overlay
	for _, o := range overlays {
		pov = append(pov, project.Overlay{Name: o.Name, Patterns: patterns(o)})
	}
	byLayer := map[string][]string{}
	for _, a := range project.Assign(projects, pov) {
		if a.Layer != "" {
			byLayer[a.Layer] = append(byLayer[a.Layer], a.Project.Dir)
		}
	}
	if from != "" {
		abs, err := filepath.Abs(expand(e, from))
		if err != nil {
			return err
		}
		found := false
		for _, dirs := range byLayer {
			if filterDirs(dirs, abs) != nil {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("--from %s is not a project any overlay matches", from)
		}
		from = abs
	}
	for _, o := range overlays {
		dirs := byLayer[o.Name]
		if from != "" {
			dirs = filterDirs(dirs, from)
		}
		if len(dirs) == 0 {
			continue
		}
		overlayOpt := check.Options{Base: false}
		keys := injectKeys(o)
		if len(keys) > 0 {
			var docs []settings.Doc
			for _, d := range dirs {
				doc, err := settings.Load(filepath.Join(d, ".claude", "settings.local.json"))
				if err != nil {
					return err
				}
				docs = append(docs, settings.Extract(doc, keys))
			}
			if err := allEqual(e, o.Name, dirs, docs, keys); err != nil {
				return err
			}
			tmpl, err := overlayTemplate(o)
			if err != nil {
				return err
			}
			if changed := settings.Merge(tmpl, docs[0], keys); len(changed) > 0 {
				b, err := tmpl.Marshal()
				if err != nil {
					return err
				}
				if fs := check.File("project/settings.local.json", b, overlayOpt); len(fs) > 0 {
					rep.refused = append(rep.refused, o.Name+": project/settings.local.json: "+fs[0].Rule)
				} else {
					p := filepath.Join(o.Dir, "project", "settings.local.json")
					if err := fsx.WriteFile(o.Dir, p, b, 0o644); err != nil {
						return err
					}
					rep.updated = append(rep.updated, o.Name+": project/settings.local.json ("+strings.Join(changed, ", ")+")")
				}
			}
		}
		if o.Manifest.Inject != nil && o.Manifest.Inject.ClaudeLocal != "" {
			var blocks []string
			for _, d := range dirs {
				b, _ := os.ReadFile(filepath.Join(d, "CLAUDE.local.md"))
				blocks = append(blocks, inject.Block(string(b)))
			}
			for i := 1; i < len(blocks); i++ {
				if blocks[i] != blocks[0] {
					return fmt.Errorf("%s: CLAUDE.local.md blocks differ between %s and %s; use --from <project>", o.Name, e.tilde(dirs[0]), e.tilde(dirs[i]))
				}
			}
			cur, _ := overlayClaudeLocal(o)
			if blocks[0] != "" && blocks[0] != cur {
				body := []byte(blocks[0] + "\n")
				if fs := check.File(o.Manifest.Inject.ClaudeLocal, body, overlayOpt); len(fs) > 0 {
					rep.refused = append(rep.refused, o.Name+": "+o.Manifest.Inject.ClaudeLocal+": "+fs[0].Rule)
					continue
				}
				if err := fsx.WriteFile(o.Dir, filepath.Join(o.Dir, o.Manifest.Inject.ClaudeLocal), body, 0o644); err != nil {
					return err
				}
				rep.updated = append(rep.updated, o.Name+": "+o.Manifest.Inject.ClaudeLocal)
			}
		}
	}
	return nil
}

func allEqual(e *Env, layer string, dirs []string, docs []settings.Doc, keys []string) error {
	for i := 1; i < len(docs); i++ {
		if diff := settings.Diff(docs[0], docs[i], keys); len(diff) > 0 {
			return fmt.Errorf("%s: %s differs between %s and %s; use --from <project>", layer, strings.Join(diff, ", "), e.tilde(dirs[0]), e.tilde(dirs[i]))
		}
	}
	return nil
}

// filterDirs keeps the discovered directory that names the same place as
// keep. Both sides are resolved through symlinks before comparing: macOS puts
// temporary and some user directories behind links, so the typed path and
// the discovered one can spell one directory two ways. The discovered
// spelling is what the caller gets back, since state is keyed by it.
func filterDirs(dirs []string, keep string) []string {
	k := canonDir(keep)
	for _, d := range dirs {
		if d == keep || canonDir(d) == k {
			return []string{d}
		}
	}
	return nil
}

func canonDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// checkAdds refuses an --add that no path of the base layer covers, which
// would otherwise do nothing without a word.
func checkAdds(add []string, base Layer) error {
	var paths []string
	if base.Manifest.Claude != nil {
		paths = base.Manifest.Claude.Paths
	}
	for _, a := range add {
		covered := false
		for _, p := range paths {
			if a == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(a, p)) {
				covered = true
				break
			}
		}
		if !covered {
			return usagef("capture: --add %s is outside the paths the base layer mirrors; add it to [claude] paths in layer.toml first", a)
		}
	}
	return nil
}

func copyInto(src, dst, root string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return fsx.WriteFile(root, dst, b, 0o644)
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
