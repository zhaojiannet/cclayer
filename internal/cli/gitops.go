package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// isGitDir reports whether dir is a git work tree.
func isGitDir(dir string) bool {
	return exists(filepath.Join(dir, ".git"))
}

// cloned reports whether cclayer runs git in a layer: only in a clone it made
// from [repo], whose .git/config it wrote. A directory layer is synced by
// other means, so a .git inside it carries configuration from wherever it
// came from, and core.fsmonitor, clean filters or remote.*.uploadpack there
// would run programs on a plain git status or fetch.
func cloned(d *manifest.Device, name, dir string) bool {
	return d.Repo[name] != "" && isGitDir(dir)
}

// git runs a git command in dir and returns trimmed stdout.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// isClean reports whether the clone has no local changes.
func isClean(dir string) (bool, error) {
	out, err := git(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// pullFastForward fetches and fast-forwards a clean clone. A dirty clone or
// a diverged branch is reported, never forced.
func pullFastForward(d *manifest.Device, name, dir string) (string, error) {
	if !cloned(d, name, dir) {
		return "local directory, nothing to pull", nil
	}
	clean, err := isClean(dir)
	if err != nil {
		return "", err
	}
	if !clean {
		return "skipped: uncommitted changes", nil
	}
	if _, err := git(dir, "rev-parse", "--abbrev-ref", "@{u}"); err != nil {
		return "skipped: no upstream branch", nil
	}
	if _, err := git(dir, "fetch", "--quiet"); err != nil {
		return "skipped: fetch failed (offline?)", nil
	}
	if _, err := git(dir, "merge", "--ff-only", "--quiet", "@{u}"); err != nil {
		return "skipped: not a fast-forward, merge by hand", nil
	}
	return "up to date", nil
}

// aheadBehind returns commits ahead of and behind the upstream.
func aheadBehind(dir string) (ahead, behind int, err error) {
	out, err := git(dir, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	ahead, _ = strconv.Atoi(parts[0])
	behind, _ = strconv.Atoi(parts[1])
	return ahead, behind, nil
}
