package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/gitconf"
	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/inject"
	"github.com/zhaojiannet/cclayer/internal/project"
)

// runDoctor checks for the conditions behind known Claude Code bugs and for
// cclayer's own assumptions. It only reports.
func runDoctor(e *Env, args []string) error {
	if len(args) > 0 {
		return usagef("doctor takes no arguments")
	}
	problems := 0
	report := func(ok bool, format string, args ...any) {
		mark := "ok  "
		if !ok {
			mark = "warn"
			problems++
		}
		fmt.Fprintf(e.Stdout, "%s %s\n", mark, fmt.Sprintf(i18n.T(format), args...))
	}

	if _, err := os.Stat(filepath.Join(e.ClaudeDir, ".git")); err == nil {
		report(false, "~/.claude is a git repository; plugin versions get polluted (claude-code #80304). Keep the layer clones elsewhere")
	} else {
		report(true, "~/.claude is not a git repository")
	}
	for _, name := range []string{"settings.json", "projects"} {
		p := filepath.Join(e.ClaudeDir, name)
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			report(false, "~/.claude/%s is a symlink (claude-code #97264, #98044); cclayer copies files instead", name)
		} else {
			report(true, "~/.claude/%s is not a symlink", name)
		}
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		if filepath.Clean(expand(e, v)) == filepath.Clean(e.ClaudeDir) {
			report(false, "CLAUDE_CONFIG_DIR is set to the default path, which selects a different Keychain entry (claude-code #92252); unset it")
		} else if layer := profileOf(e, v); layer != "" {
			report(true, "CLAUDE_CONFIG_DIR selects the profile of layer %s", layer)
		} else {
			report(false, "CLAUDE_CONFIG_DIR points at %s, which cclayer does not write; sessions started here do not see the layers. Unset it, or use cclayer env with profiles on", v)
		}
	} else {
		report(true, "CLAUDE_CONFIG_DIR is not set")
	}

	if loaded, err := e.Load(); err == nil && loaded.Device.Profiles {
		for _, o := range loaded.Overlays() {
			dir := loaded.Device.ProfilePath(o.Name)
			for _, name := range []string{"settings.json", "projects", "CLAUDE.md"} {
				p := filepath.Join(dir, name)
				if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
					report(false, "%s is a symlink; profiles must hold copies (claude-code #97264, #98044)", e.tilde(p))
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
				report(false, "%s exists; ~/.claude/CLAUDE.md loads anyway (claude-code #88528), so the profile copy loads twice", e.tilde(filepath.Join(dir, "CLAUDE.md")))
			}
		}
	}

	// git include block
	gc, _ := gitconf.ReadFile(e.GitConfig)
	if !strings.Contains(gc, gitconf.Marker) {
		if strings.Contains(gc, gitconf.IncludePath) {
			report(false, "the cclayer include block in ~/.gitconfig lost its marker comment; run cclayer apply")
		} else {
			report(false, "~/.gitconfig has no cclayer include block; run cclayer apply")
		}
	} else if _, changed := gitconf.EnsureIncludeBlock(gc); changed {
		report(false, "the cclayer include block is not at the end of ~/.gitconfig; settings after it override the layers. apply moves it back")
	} else {
		report(true, "~/.gitconfig include block is in place")
	}

	// excludes
	exPath, err := inject.ExcludesFile()
	if err == nil {
		b, _ := os.ReadFile(exPath)
		missing := []string{}
		for _, p := range inject.ExcludePatterns {
			if !strings.Contains(string(b), p) {
				missing = append(missing, p)
			}
		}
		if len(missing) > 0 {
			report(false, "%s lacks %s; run cclayer apply", e.tilde(exPath), strings.Join(missing, ", "))
		} else {
			report(true, "global git excludes cover the injected files (%s)", e.tilde(exPath))
		}
	}

	// identities in every repository under the roots
	loaded, err := e.Load()
	if err != nil {
		report(false, "cannot load layers: %v", err)
	} else {
		var roots []string
		for _, r := range loaded.Device.Roots {
			roots = append(roots, expand(e, r))
		}
		projects, err := project.Discover(roots)
		if err == nil {
			var pov []project.Overlay
			byName := map[string]Layer{}
			for _, o := range loaded.Overlays() {
				pov = append(pov, project.Overlay{Name: o.Name, Patterns: patterns(o)})
				byName[o.Name] = o
			}
			wrong := 0
			for _, a := range project.Assign(projects, pov) {
				want := ""
				if a.Layer != "" {
					want = byName[a.Layer].Manifest.Identity.Email
				} else if d := loaded.Device.DefaultIdentity; d != "" {
					want = byName[d].Manifest.Identity.Email
				}
				got := gitUserEmail(a.Project.Dir)
				switch {
				case want == "" && got != "":
					report(false, "%s: no overlay matches but git would commit as %s", e.tilde(a.Project.Dir), got)
					wrong++
				case want != "" && got != want:
					report(false, "%s: git user.email is %s, layer expects %s", e.tilde(a.Project.Dir), orNone(got), want)
					wrong++
				}
			}
			if wrong == 0 {
				if len(projects) == 1 {
					report(true, "git identities match the layers in 1 repository")
				} else {
					report(true, "git identities match the layers in %d repositories", len(projects))
				}
			}
		}
	}
	if _, err := exec.LookPath("claude"); err != nil {
		report(false, "claude is not on PATH; plugin, MCP and purge steps are skipped")
	}
	if problems > 0 {
		e.printf("%d warning(s)\n", problems)
	}
	return nil
}

func gitUserEmail(dir string) string {
	out, err := git(dir, "config", "--get", "user.email")
	if err != nil {
		return ""
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return i18n.T("unset")
	}
	return s
}

// profileOf returns the overlay whose profile directory dir is, or "".
func profileOf(e *Env, dir string) string {
	loaded, err := e.Load()
	if err != nil || !loaded.Device.Profiles {
		return ""
	}
	want := filepath.Clean(expand(e, dir))
	for _, o := range loaded.Overlays() {
		if filepath.Clean(loaded.Device.ProfilePath(o.Name)) == want {
			return o.Name
		}
	}
	return ""
}
