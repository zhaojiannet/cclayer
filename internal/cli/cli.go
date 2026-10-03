// Package cli implements the cclayer commands.
package cli

import (
	"errors"
	"fmt"

	"github.com/zhaojiannet/cclayer/internal/i18n"
)

const usage = `cclayer - layered sync of Claude Code configuration

Usage:
  cclayer setup                   guided first-time setup of this device
  cclayer init                    create the device manifest and clone the layers (plain prompts)
  cclayer layer add <name> <git-url|directory> [--method deploy-key|token|none] [--port-443]
                                  add one overlay to this device: credentials, clone, checks
  cclayer apply [--pull] [--mcp-force] [--replay-plugins]
                                  apply the enabled layers to this device
  cclayer apply --hook            non-interactive variant for the SessionStart hook
  cclayer capture [--add <path>]... [--from <project>]
                                  write local changes back into the layer clones
  cclayer check                   refuse content that must not enter a layer
  cclayer status                  per-layer state and matched projects
  cclayer leave <layer>           purge a layer's projects and remove it from this device
  cclayer doctor                  check for known Claude Code and git pitfalls
  cclayer keys setup <layer> [--method deploy-key|token] [--port-443]
                                  give this device credentials for one layer repository
  cclayer keys list | keys remove <layer>
  cclayer env [dir]               print the CLAUDE_CONFIG_DIR export for a project (profiles mode)
  cclayer run [dir] -- <cmd...>   run a command with CLAUDE_CONFIG_DIR set for a project (profiles mode)
  cclayer version

Environment:
  CCLAYER_DEVICE   path of the device manifest (default ~/.config/cclayer/device.toml)
  CCLAYER_STATE    state directory (default ~/.local/state/cclayer)
  CCLAYER_LANG     message language: en, zh or ja (default: lang in the device manifest, then the locale)
`

// Version is set by the build.
var Version = "dev"

// Run dispatches a command line and returns the exit code.
func Run(e *Env, args []string) int {
	e.setLanguage()
	if len(args) == 0 {
		e.printf("%s", i18n.T(usage))
		return 2
	}
	var err error
	switch args[0] {
	case "init":
		err = runInit(e, args[1:])
	case "apply":
		err = runApply(e, args[1:])
	case "capture":
		err = runCapture(e, args[1:])
	case "check":
		err = runCheck(e, args[1:])
	case "status":
		err = runStatus(e, args[1:])
	case "leave":
		err = runLeave(e, args[1:])
	case "doctor":
		err = runDoctor(e, args[1:])
	case "keys":
		err = runKeys(e, args[1:])
	case "setup":
		err = runSetup(e, args[1:])
	case "layer":
		err = runLayer(e, args[1:])
	case "env":
		err = runEnv(e, args[1:])
	case "run":
		err = runRun(e, args[1:])
	case "version", "--version", "-v":
		e.printf("cclayer %s\n", Version)
	case "help", "--help", "-h":
		e.printf("%s", i18n.T(usage))
	default:
		e.errorf("unknown command %q\n\n%s", args[0], i18n.T(usage))
		return 2
	}
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			e.errorf("%v\n", err)
			return 2
		}
		var xe exitError
		if errors.As(err, &xe) {
			return xe.code
		}
		e.errorf("cclayer: %v\n", err)
		return 1
	}
	return 0
}

// exitError carries a child process's exit code through to the caller.
type exitError struct{ code int }

func (x exitError) Error() string { return fmt.Sprintf("exit status %d", x.code) }

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func usagef(format string, args ...any) error {
	return usageError{fmt.Sprintf(format, args...)}
}
