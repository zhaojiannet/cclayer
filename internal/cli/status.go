package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/project"
)

func runStatus(e *Env, args []string) error {
	if len(args) > 0 {
		return usagef("status takes no arguments")
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	applied, err := e.State.Load()
	if err != nil {
		return err
	}
	for _, l := range loaded.Layers {
		line := l.Name + ": " + e.tilde(l.Dir)
		if !cloned(loaded.Device, l.Name, l.Dir) {
			e.printf("%s, local directory\n", line)
			continue
		}
		clean, err := isClean(l.Dir)
		if err == nil && !clean {
			line += i18n.T(", uncommitted changes")
		}
		if a, b, err := aheadBehind(l.Dir); err == nil && (a > 0 || b > 0) {
			line += fmt.Sprintf(i18n.T(", %d ahead, %d behind"), a, b)
		}
		e.printf("%s\n", line)
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
	for _, o := range loaded.Overlays() {
		pov = append(pov, project.Overlay{Name: o.Name, Patterns: patterns(o)})
	}
	byLayer := map[string][]string{}
	var conflicts, unmatched []string
	for _, a := range project.Assign(projects, pov) {
		switch {
		case len(a.Conflict) > 0:
			conflicts = append(conflicts, e.tilde(a.Project.Dir)+" ("+strings.Join(a.Conflict, ", ")+")")
		case a.Layer == "":
			unmatched = append(unmatched, e.tilde(a.Project.Dir))
		default:
			byLayer[a.Layer] = append(byLayer[a.Layer], e.tilde(a.Project.Dir))
		}
	}
	names := make([]string, 0, len(byLayer))
	for n := range byLayer {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e.printf("\nprojects of %s:\n", n)
		for _, p := range byLayer[n] {
			e.printf("  %s\n", p)
		}
	}
	if len(unmatched) > 0 {
		e.printf("\nprojects no overlay matches:\n")
		for _, p := range unmatched {
			e.printf("  %s\n", p)
		}
	}
	if len(conflicts) > 0 {
		e.printf("\nprojects matching more than one overlay:\n")
		for _, p := range conflicts {
			e.printf("  %s\n", p)
		}
	}
	// projects remembered from earlier applies whose directory is gone
	var gone []string
	for dir, layer := range applied.Projects {
		if _, err := filepath.Abs(dir); err == nil {
			if !exists(dir) {
				gone = append(gone, e.tilde(dir)+" ("+layer+")")
			}
		}
	}
	if len(gone) > 0 {
		sort.Strings(gone)
		e.printf("\nremembered projects whose directory is gone (leave still purges them):\n")
		for _, p := range gone {
			e.printf("  %s\n", p)
		}
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
