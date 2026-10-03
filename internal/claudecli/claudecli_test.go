package claudecli

import (
	"errors"
	"strings"
	"testing"
)

type fake struct {
	exists map[string]bool
	calls  []string
}

func (f *fake) Run(dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) >= 3 && args[0] == "mcp" && args[1] == "get" {
		if f.exists[args[2]] {
			return "{}", nil
		}
		return "", errors.New("not found")
	}
	return "", nil
}

func TestPlanMCP(t *testing.T) {
	r := &fake{exists: map[string]bool{"exa": true}}
	servers := []MCPServer{{"exa", `{"type":"http"}`}, {"ctx", `{"type":"stdio"}`}}
	cmds, _ := PlanMCP(r, servers, false)
	if len(cmds) != 1 || cmds[0].Args[4] != "ctx" {
		t.Fatalf("existing server must be skipped: %v", cmds)
	}
	cmds, _ = PlanMCP(r, servers, true)
	if len(cmds) != 3 || cmds[0].Args[1] != "remove" {
		t.Fatalf("force must remove then add: %v", cmds)
	}
}

func TestPlanPluginsAndPurge(t *testing.T) {
	cmds := PlanPlugins("/p", []Marketplace{{"canon", "zhaojiannet/canon"}}, []string{"flow@canon"}, "local")
	if len(cmds) != 2 || cmds[1].String() != "(in /p) claude plugin install --scope local flow@canon" {
		t.Fatalf("%v", cmds)
	}
	if p := PlanPurge([]string{"/a"}); p[0].String() != "claude purge --yes /a" {
		t.Fatalf("%v", p)
	}
}

type listRunner struct{ markets, plugins string }

func (l listRunner) Run(dir, name string, args ...string) (string, error) {
	if strings.Join(args, " ") == "plugin marketplace list --json" {
		return l.markets, nil
	}
	if strings.Join(args, " ") == "plugin list --json" {
		return l.plugins, nil
	}
	return "", nil
}

// Marketplaces and user plugins the device has are not added again;
// unreadable listings leave the plan as it is.
func TestSkipPresent(t *testing.T) {
	cmds := PlanPlugins("", []Marketplace{{Name: "acme", Source: "acme/market"}, {Name: "new", Source: "x/new"}}, []string{"tools@acme", "fresh@acme"}, "user")
	p, ok := ListPresent(listRunner{
		markets: `[{"name":"acme","source":"github","repo":"acme/market"}]`,
		plugins: `[{"id":"tools@acme","scope":"user"},{"id":"fresh@acme","scope":"project"}]`,
	})
	if !ok {
		t.Fatal("listing must parse")
	}
	var got []string
	for _, c := range SkipPresent(cmds, p) {
		got = append(got, c.String())
	}
	if strings.Join(got, "|") != "claude plugin marketplace add x/new|claude plugin install --scope user fresh@acme" {
		t.Errorf("kept %v", got)
	}
	if _, ok := ListPresent(listRunner{markets: "not json"}); ok {
		t.Error("an unreadable listing must report not ok")
	}
	local := PlanPlugins("/p", nil, []string{"tools@acme"}, "local")
	if len(SkipPresent(local, p)) != 1 {
		t.Error("local installs are not skipped by the user listing")
	}
}
