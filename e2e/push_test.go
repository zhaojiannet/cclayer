package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

func remoteFile(t *testing.T, bare, rel string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bare, "show", "main:"+rel).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// push captures a local edit, commits it in the layer clone and pushes it;
// a second push finds nothing to do.
func TestPushUploadsCapturedChanges(t *testing.T) {
	w, sd := applied(t)
	w.write(".claude/CLAUDE.md", "# shared, edited here\n")
	out, code := w.run("", "push", "--yes", "-m", "edit from the laptop")
	if code != 0 || !strings.Contains(out, "base: pushed") {
		t.Fatalf("push: %d %s", code, out)
	}
	if got := remoteFile(t, sd.base, "claude/CLAUDE.md"); got != "# shared, edited here\n" {
		t.Errorf("remote CLAUDE.md = %q", got)
	}
	msg, _ := exec.Command("git", "-C", sd.base, "log", "-1", "--format=%s", "main").Output()
	if strings.TrimSpace(string(msg)) != "edit from the laptop" {
		t.Errorf("commit message %q", msg)
	}
	if out, code := w.run("", "push", "--yes"); code != 0 || !strings.Contains(out, "nothing to push") {
		t.Errorf("second push: %d %s", code, out)
	}
}

// Without --yes the user is asked; no means nothing is committed.
func TestPushAsksFirst(t *testing.T) {
	w, sd := applied(t)
	w.write(".claude/CLAUDE.md", "# changed\n")
	out, code := w.run("n\n", "push")
	if code == 0 || !strings.Contains(out, "To commit and push") {
		t.Fatalf("declined push: %d %s", code, out)
	}
	if got := remoteFile(t, sd.base, "claude/CLAUDE.md"); got != "# shared\n" {
		t.Errorf("a declined push reached the remote: %q", got)
	}
}

// Another device pushed meanwhile: this device's commit goes on top of
// theirs, and the user is told to apply what came along.
func TestPushRebasesOnNewerRemote(t *testing.T) {
	w, sd := applied(t)
	w.pushToRemote("base", "claude/rules/other.md", "from the other machine\n")
	w.write(".claude/CLAUDE.md", "# edited here\n")
	out, code := w.run("", "push", "--yes")
	if code != 0 || !strings.Contains(out, "run cclayer apply") {
		t.Fatalf("push over a newer remote: %d %s", code, out)
	}
	if remoteFile(t, sd.base, "claude/rules/other.md") == "" || remoteFile(t, sd.base, "claude/CLAUDE.md") != "# edited here\n" {
		t.Error("the remote must hold both changes")
	}
}

// A conflicting change on the remote stops the push and leaves no rebase
// in progress.
func TestPushStopsOnConflict(t *testing.T) {
	w, sd := applied(t)
	w.pushToRemote("base", "claude/CLAUDE.md", "# theirs\n")
	w.write(".claude/CLAUDE.md", "# mine\n")
	out, code := w.run("", "push", "--yes")
	if code == 0 || !strings.Contains(out, "merge them by hand") {
		t.Fatalf("conflicting push: %d %s", code, out)
	}
	if remoteFile(t, sd.base, "claude/CLAUDE.md") != "# theirs\n" {
		t.Error("the remote must keep the other device's version")
	}
	clone := w.home + "/.local/share/cclayer/base"
	if st, _ := exec.Command("git", "-C", clone, "status").CombinedOutput(); strings.Contains(string(st), "rebase") {
		t.Errorf("a rebase was left in progress:\n%s", st)
	}
}
