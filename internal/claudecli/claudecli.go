// Package claudecli wraps the claude commands cclayer drives: plugins, MCP
// servers and purge. Commands are planned first so the caller can show them
// and ask once before anything runs.
package claudecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Runner executes a command; tests substitute a fake.
type Runner interface {
	Run(dir string, name string, args ...string) (stdout string, err error)
}

// Exec is the real runner.
type Exec struct{}

// Run executes name with args in dir, returning combined output on failure.
func (Exec) Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Command is one planned invocation.
type Command struct {
	Dir  string // working directory, "" for the current one
	Args []string
	Why  string
	// Marketplace or UserPlugin names what the command adds, so commands for
	// things the device already has can be dropped before anything runs.
	Marketplace string
	UserPlugin  string
}

func (c Command) String() string {
	s := "claude " + strings.Join(c.Args, " ")
	if c.Dir != "" {
		s = "(in " + c.Dir + ") " + s
	}
	return s
}

// Marketplace is one entry of extraKnownMarketplaces.
type Marketplace struct {
	Name   string
	Source string // "owner/repo" for github sources, a URL or path otherwise
}

// nameRe is what a plugin, marketplace or MCP server name taken from a layer
// may look like before it becomes an argument: it cannot start with "-",
// where the claude CLI would read it as an option.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]*$`)

// CheckArgs refuses names and marketplace sources from a layer that the
// claude CLI or the git it runs would not take as plain values: a leading
// "-" is an option, and "::" selects a git remote helper such as ext::,
// which runs a command.
func CheckArgs(marketplaces []Marketplace, plugins []string, servers []MCPServer) error {
	for _, m := range marketplaces {
		if !nameRe.MatchString(m.Name) {
			return fmt.Errorf("marketplace name %q is not a plain name", m.Name)
		}
		if strings.HasPrefix(m.Source, "-") || strings.Contains(m.Source, "::") || strings.ContainsAny(m.Source, "\x00\n") {
			return fmt.Errorf("marketplace %s: source %q is not a repository, URL or path", m.Name, m.Source)
		}
	}
	for _, p := range plugins {
		if !nameRe.MatchString(p) {
			return fmt.Errorf("plugin name %q is not a plain name", p)
		}
	}
	for _, sv := range servers {
		if !nameRe.MatchString(sv.Name) {
			return fmt.Errorf("MCP server name %q is not a plain name", sv.Name)
		}
	}
	return nil
}

// PlanPlugins returns the commands that make the enabled plugins present at
// the given scope. scope is "user" for the base, "local" for an overlay
// applied inside a matched project directory.
func PlanPlugins(dir string, marketplaces []Marketplace, plugins []string, scope string) []Command {
	var out []Command
	for _, m := range marketplaces {
		out = append(out, Command{Dir: dir, Args: []string{"plugin", "marketplace", "add", m.Source}, Why: "register marketplace " + m.Name, Marketplace: m.Name})
	}
	for _, p := range plugins {
		c := Command{Dir: dir, Args: []string{"plugin", "install", "--scope", scope, p}, Why: "install plugin"}
		if scope == "user" {
			c.UserPlugin = p
		}
		out = append(out, c)
	}
	return out
}

// Present is what the device already has, as the claude CLI reports it.
type Present struct {
	Marketplaces map[string]bool // by name
	UserPlugins  map[string]bool // plugin@marketplace installed at user scope
}

// ListPresent asks claude for its marketplaces and installed plugins. ok is
// false when either listing fails or cannot be read, and the caller then
// keeps every planned command.
func ListPresent(r Runner) (p Present, ok bool) {
	var markets []struct {
		Name string `json:"name"`
	}
	var plugins []struct {
		ID    string `json:"id"`
		Scope string `json:"scope"`
	}
	out, err := r.Run("", "claude", "plugin", "marketplace", "list", "--json")
	if err != nil || json.Unmarshal([]byte(out), &markets) != nil {
		return Present{}, false
	}
	out, err = r.Run("", "claude", "plugin", "list", "--json")
	if err != nil || json.Unmarshal([]byte(out), &plugins) != nil {
		return Present{}, false
	}
	p = Present{Marketplaces: map[string]bool{}, UserPlugins: map[string]bool{}}
	for _, m := range markets {
		p.Marketplaces[m.Name] = true
	}
	for _, pl := range plugins {
		if pl.Scope == "user" {
			p.UserPlugins[pl.ID] = true
		}
	}
	return p, true
}

// SkipPresent drops the commands for marketplaces and user plugins the
// device already has. Running `marketplace add` again is not harmless:
// claude rewrites the marketplace's entry in settings.json and drops
// settings such as autoUpdate that the layer had set.
func SkipPresent(cmds []Command, p Present) []Command {
	var out []Command
	for _, c := range cmds {
		if (c.Marketplace != "" && p.Marketplaces[c.Marketplace]) || (c.UserPlugin != "" && p.UserPlugins[c.UserPlugin]) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// MCPServer is one definition from the base layer's mcp/ directory.
type MCPServer struct {
	Name string
	JSON string
}

// PlanMCP returns the add-json commands for servers that do not exist yet,
// consulting `claude mcp get` for each. With force, existing servers are
// removed first and re-added.
func PlanMCP(r Runner, servers []MCPServer, force bool) ([]Command, error) {
	var out []Command
	for _, s := range servers {
		_, err := r.Run("", "claude", "mcp", "get", s.Name)
		exists := err == nil
		switch {
		case exists && !force:
			continue
		case exists && force:
			out = append(out, Command{Args: []string{"mcp", "remove", "--scope", "user", s.Name}, Why: "replace MCP server"})
		}
		out = append(out, Command{Args: []string{"mcp", "add-json", "--scope", "user", s.Name, s.JSON}, Why: "add MCP server"})
	}
	return out, nil
}

// PlanPurge returns the purge commands for project paths. Confirmation is
// cclayer's, so claude's own prompt is skipped with --yes.
func PlanPurge(projectDirs []string) []Command {
	var out []Command
	for _, d := range projectDirs {
		out = append(out, Command{Args: []string{"purge", "--yes", d}, Why: "remove Claude Code state for " + d})
	}
	return out
}

// Execute runs planned commands in order and stops at the first failure.
func Execute(r Runner, cmds []Command) error {
	for _, c := range cmds {
		if _, err := r.Run(c.Dir, "claude", c.Args...); err != nil {
			return err
		}
	}
	return nil
}

// Available reports whether the claude binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// PurgeDryRun shows what claude purge would delete for one project.
func PurgeDryRun(r Runner, projectDir string) (string, error) {
	return r.Run("", "claude", "purge", "--dry-run", projectDir)
}
