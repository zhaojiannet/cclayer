package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/project"
	"github.com/zhaojiannet/cclayer/internal/settings"
	"github.com/zhaojiannet/cclayer/internal/state"
)

// Profiles mode gives every overlay its own CLAUDE_CONFIG_DIR so that
// logins, sessions and prompt history stay apart per organization. The
// shared base configuration is copied into each profile on apply (copies,
// not symlinks: claude-code #97264 and #98044), except CLAUDE.md, which
// Claude Code reads from ~/.claude regardless of CLAUDE_CONFIG_DIR (#88528).

// profileFor returns the overlay matching a project directory, or "".
func profileFor(e *Env, loaded *Loaded, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	// walk up to the repository root
	for cur := abs; ; cur = filepath.Dir(cur) {
		if _, err := os.Lstat(filepath.Join(cur, ".git")); err == nil {
			abs = cur
			break
		}
		if filepath.Dir(cur) == cur {
			return "", nil
		}
	}
	p, err := project.Inspect(abs)
	if err != nil {
		return "", err
	}
	var pov []project.Overlay
	for _, o := range loaded.Overlays() {
		pov = append(pov, project.Overlay{Name: o.Name, Patterns: patterns(o)})
	}
	a := project.Assign([]project.Project{p}, pov)[0]
	if len(a.Conflict) > 0 {
		return "", fmt.Errorf("%s matches %s", e.tilde(abs), describeOverlays(loaded, a.Conflict))
	}
	return a.Layer, nil
}

// runEnv prints the shell line that selects the profile for a directory.
func runEnv(e *Env, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	if !loaded.Device.Profiles {
		return fmt.Errorf("profiles are off; set profiles = true in %s and run cclayer apply", e.tilde(e.DevicePath))
	}
	layer, err := profileFor(e, loaded, dir)
	if err != nil {
		return err
	}
	if layer == "" {
		if runtime.GOOS == "windows" {
			fmt.Fprint(e.Stdout, "Remove-Item Env:CLAUDE_CONFIG_DIR -ErrorAction SilentlyContinue\n")
		} else {
			fmt.Fprint(e.Stdout, "unset CLAUDE_CONFIG_DIR\n")
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(e.Stdout, "$env:CLAUDE_CONFIG_DIR = %s\n", psQuote(loaded.Device.ProfilePath(layer)))
	} else {
		fmt.Fprintf(e.Stdout, "export CLAUDE_CONFIG_DIR=%s\n", shQuote(loaded.Device.ProfilePath(layer)))
	}
	return nil
}

// shQuote wraps s in single quotes for sh, where nothing expands.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote wraps s in single quotes for PowerShell.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// runRun executes a command with CLAUDE_CONFIG_DIR set for a directory.
func runRun(e *Env, args []string) error {
	dir := "."
	i := 0
	if len(args) > 0 && args[0] != "--" {
		dir = args[0]
		i = 1
	}
	if i < len(args) && args[i] == "--" {
		i++
	}
	cmdArgs := args[i:]
	if len(cmdArgs) == 0 {
		return usagef("usage: cclayer run [dir] -- <command> [args...]")
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	if !loaded.Device.Profiles {
		return fmt.Errorf("profiles are off; set profiles = true in %s and run cclayer apply", e.tilde(e.DevicePath))
	}
	layer, err := profileFor(e, loaded, dir)
	if err != nil {
		return err
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, e.Stdout, e.Stderr
	// start from an environment without an inherited profile, so a shell
	// that already exported one does not leak it into an unmatched project
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			env = append(env, kv)
		}
	}
	if layer != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+loaded.Device.ProfilePath(layer))
	}
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return exitError{code: ee.ExitCode()}
		}
		return err
	}
	return nil
}

// applyProfiles populates one config directory per overlay with the base
// layer's mirrored paths and the owned settings keys that passed the hooks
// gate for ~/.claude.
func applyProfiles(e *Env, loaded *Loaded, applied *state.Applied, backup *state.Backup, ctx *applyCtx, rep *report) error {
	if !loaded.Device.Profiles {
		return nil
	}
	base := loaded.Base()
	for _, o := range loaded.Overlays() {
		dir := loaded.Device.ProfilePath(o.Name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := mirrorInto(e, base, dir, true, applied, backup, ctx, rep); err != nil {
			return err
		}
		if keys := ctx.settingsKeys; len(keys) > 0 {
			src, err := baseSettings(base)
			if err != nil {
				return err
			}
			target := filepath.Join(dir, "settings.json")
			dst, err := settings.Load(target)
			if err != nil {
				return err
			}
			if changed := settings.Merge(dst, src, keys); len(changed) > 0 {
				b, err := dst.Marshal()
				if err != nil {
					return err
				}
				if err := writeManaged(e, target, b, applied, backup, rep); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
