package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/claudecli"
	"github.com/zhaojiannet/cclayer/internal/project"
)

func runLeave(e *Env, args []string) error {
	fs := flag.NewFlagSet("leave", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	force := fs.Bool("force", false, "remove the clone even with uncommitted or unpushed changes")
	name, rest := splitPositional(args)
	if err := fs.Parse(rest); err != nil {
		return usagef("leave: %v", err)
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return usagef("usage: cclayer leave [--force] <layer>")
	}
	if name == "" {
		return usagef("usage: cclayer leave [--force] <layer>")
	}
	if name == "base" {
		return usagef("the base layer cannot be left; remove the device manifest instead")
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	layer, ok := loaded.Find(name)
	if !ok {
		return fmt.Errorf("layer %q is not enabled on this device", name)
	}
	// a directory the user pointed cclayer at is theirs and stays; only a
	// clone made from [repo] is deleted at the end
	cloned := loaded.Device.Repo[name] != ""
	applied, err := e.State.Load()
	if err != nil {
		return err
	}

	// refuse to throw away work that has not left the clone; a directory the
	// user owns is not deleted, so its (or a parent repository's) state is
	// not cclayer's business
	if !*force && cloned {
		if clean, err := isClean(layer.Dir); err == nil && !clean {
			return fmt.Errorf("%s has uncommitted changes; commit and push them or use --force", e.tilde(layer.Dir))
		}
		if ahead, _, err := aheadBehind(layer.Dir); err == nil && ahead > 0 {
			return fmt.Errorf("%s has %d unpushed commit(s); push them or use --force", e.tilde(layer.Dir), ahead)
		}
	}

	dirs, unknown, err := projectsOf(e, loaded, layer, applied.Projects)
	if err != nil {
		return err
	}
	e.printf("Leaving layer %s. Claude Code state of these projects will be purged:\n", name)
	for _, d := range dirs {
		note := ""
		if !exists(d) {
			note = " (directory gone; state only)"
		}
		e.printf("  %s%s\n", e.tilde(d), note)
	}
	if len(dirs) == 0 {
		e.printf("  (none)\n")
	}
	for _, u := range unknown {
		if e.confirm("%s has Claude Code state but is not known to belong to any layer. Purge it as part of %s?", e.tilde(u), name) {
			dirs = append(dirs, u)
		}
	}
	if !e.confirm("Proceed?") {
		return fmt.Errorf("leave cancelled")
	}

	// remember what the overlay registered on this device before its clone goes
	var marketplaces []claudecli.Marketplace
	if tmpl, err := overlayTemplate(layer); err == nil {
		marketplaces, _ = pluginsFrom(tmpl)
	}

	// 1. purge project state through claude
	if len(dirs) > 0 {
		if !claudeAvailable() {
			return fmt.Errorf("claude is not on PATH; cannot purge project state. Install Claude Code or purge by hand with `claude purge <path>`")
		}
		if err := claudecli.Execute(e.Runner, claudecli.PlanPurge(dirs)); err != nil {
			return err
		}
	}
	// 2. the layer's own config directory, when profiles are on
	if loaded.Device.Profiles {
		if dir := loaded.Device.ProfilePath(name); exists(dir) {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			e.printf("removed %s (logins, sessions and history of this layer)\n", e.tilde(dir))
		}
	}
	// 3. take the injected files out of projects that still exist
	for _, d := range dirs {
		if exists(d) {
			if err := removeInjected(layer, d); err != nil {
				return err
			}
		}
	}
	for d, l := range applied.Projects {
		if l == name {
			delete(applied.Projects, d)
		}
	}
	// 4. regenerate the git files without this layer, then persist
	remaining := &Loaded{Device: loaded.Device}
	for _, l := range loaded.Layers {
		if l.Name != name {
			remaining.Layers = append(remaining.Layers, l)
		}
	}
	if err := removeCredential(e, remaining.Device, name, false); err != nil {
		return err
	}
	cleared := remaining.Device.RemoveLayer(name)
	var rep report
	backup := e.State.NewBackup()
	if err := applyGit(e, remaining, applied, backup, &rep); err != nil {
		return err
	}
	if err := e.State.Save(applied); err != nil {
		return err
	}
	if err := remaining.Device.Save(e.DevicePath); err != nil {
		return err
	}
	// 5. the clone goes last; if this fails, every earlier step is already durable
	if cloned {
		if err := os.RemoveAll(layer.Dir); err != nil {
			return fmt.Errorf("layer removed from this device but its clone could not be deleted: %w", err)
		}
	}

	e.printf("\nLayer %s removed from this device.\n", name)
	if !cloned {
		e.printf("%s was not cloned by cclayer and is left in place.\n", e.tilde(layer.Dir))
	}
	if cleared {
		e.printf("default_identity named this layer and was cleared; git now refuses to commit in unmatched repositories.\n")
	}
	e.printf("Still to do by hand:\n")
	if cloned {
		e.printf("  - revoke the git credential that could reach %s\n", name)
	}
	for _, m := range marketplaces {
		e.printf("  - claude plugin marketplace remove %s   (registered on this device by apply)\n", m.Name)
	}
	e.printf("  - review %s for old snapshots of ~/.claude.json that may still list these projects\n", e.tilde(filepath.Join(e.ClaudeDir, "backups")))
	e.printf("  - review sessions started in a parent directory that holds projects of several organizations; claude purge works per project\n")
	return nil
}

// projectsOf lists the project directories the layer is responsible for:
// current matches plus the recorded map. It also returns paths under the
// roots that carry Claude Code state but belong to no discovered project, so
// the user can decide about them.
func projectsOf(e *Env, loaded *Loaded, layer Layer, remembered map[string]string) (dirs, unknown []string, err error) {
	set := map[string]bool{}
	var roots []string
	for _, r := range loaded.Device.Roots {
		roots = append(roots, expand(e, r))
	}
	projects, err := project.Discover(roots)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]bool{}
	for _, p := range projects {
		known[p.Dir] = true
	}
	for _, a := range project.Assign(projects, []project.Overlay{{Name: layer.Name, Patterns: patterns(layer)}}) {
		if a.Layer == layer.Name {
			set[a.Project.Dir] = true
		}
	}
	for dir, l := range remembered {
		known[dir] = true
		if l == layer.Name {
			set[dir] = true
		}
	}
	// Claude Code state for paths under the roots that no discovered or
	// remembered project explains
	seenUnknown := map[string]bool{}
	for _, p := range statePaths(e) {
		if known[p] || seenUnknown[p] || !underAny(p, roots) {
			continue
		}
		seenUnknown[p] = true
		unknown = append(unknown, p)
	}
	for d := range set {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	sort.Strings(unknown)
	return dirs, unknown, nil
}

// statePaths returns project paths Claude Code knows about: the keys of
// "projects" in ~/.claude.json. Directory names under ~/.claude/projects/
// are derived from paths by replacing every non-alphanumeric character with
// "-", which cannot be reversed reliably, so they are not used.
func statePaths(e *Env) []string {
	b, err := os.ReadFile(filepath.Join(e.Home, ".claude.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Projects))
	for p := range doc.Projects {
		out = append(out, p)
	}
	return out
}

func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
