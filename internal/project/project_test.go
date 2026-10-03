package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDiscoverAndAssign(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	acme := filepath.Join(root, "acme-api")
	me := filepath.Join(root, "mine")
	fork := filepath.Join(root, "fork")
	none := filepath.Join(root, "noremote")
	for _, d := range []string{acme, me, fork, none} {
		os.MkdirAll(d, 0o755)
		run(t, d, "init", "-q")
	}
	run(t, acme, "remote", "add", "origin", "git@github.com:acme-inc/api.git")
	run(t, me, "remote", "add", "origin", "https://github.com/me/mine.git")
	run(t, fork, "remote", "add", "origin", "https://github.com/me/api.git")
	run(t, fork, "remote", "add", "upstream", "https://github.com/acme-inc/api.git")
	// a nested node_modules repo must be skipped
	nested := filepath.Join(me, "node_modules", "dep")
	os.MkdirAll(nested, 0o755)
	run(t, nested, "init", "-q")
	// a worktree of acme-api: .git is a file pointing into the main repo
	run(t, acme, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init")
	wt := filepath.Join(root, "acme-api-wt")
	run(t, acme, "worktree", "add", "-q", wt)

	projects, err := Discover([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 5 {
		for _, p := range projects {
			t.Log(p.Dir, p.Remotes)
		}
		t.Fatalf("expected 5 projects, got %d", len(projects))
	}

	overlays := []Overlay{
		{Name: "acme", Patterns: []string{"github.com/acme-inc/*"}},
		{Name: "personal", Patterns: []string{"github.com/me/*"}},
	}
	got := map[string]Assignment{}
	for _, a := range Assign(projects, overlays) {
		got[filepath.Base(a.Project.Dir)] = a
	}
	if got["acme-api"].Layer != "acme" {
		t.Errorf("acme-api -> %q", got["acme-api"].Layer)
	}
	if got["acme-api-wt"].Layer != "acme" {
		t.Errorf("worktree must follow the main repo, got %q (remotes %v)", got["acme-api-wt"].Layer, got["acme-api-wt"].Project.Remotes)
	}
	if got["mine"].Layer != "personal" {
		t.Errorf("mine -> %q", got["mine"].Layer)
	}
	if len(got["fork"].Conflict) != 2 {
		t.Errorf("fork with origin=me and upstream=acme must conflict, got %+v", got["fork"])
	}
	if got["noremote"].Layer != "" || got["noremote"].Conflict != nil {
		t.Errorf("noremote should be unassigned: %+v", got["noremote"])
	}
}
