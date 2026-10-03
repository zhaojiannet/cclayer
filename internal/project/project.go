// Package project discovers git repositories under the device roots and
// assigns each to at most one overlay by its remote URLs.
package project

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/remote"
)

// Project is a discovered repository.
type Project struct {
	Dir     string   // working tree, absolute
	GitDir  string   // resolved .git directory (main repository for worktrees)
	Remotes []string // normalized remote URLs
}

// Overlay is the part of a layer manifest matching needs.
type Overlay struct {
	Name     string
	Patterns []string
}

// Assignment is the result of matching one project.
type Assignment struct {
	Project  Project
	Layer    string   // "" when no overlay matches
	Conflict []string // overlays that all matched; non-empty means skip
}

// skipDirs are never descended into while scanning roots.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "vendor": true, ".cache": true,
	".local": true, ".Trash": true, "Library": true,
}

// Discover walks roots and returns every repository found, including
// worktrees and submodules whose .git is a file. It does not descend into a
// repository once found, so nested repositories inside a working tree are
// not reported.
func Discover(roots []string) ([]Project, error) {
	var out []Project
	seen := map[string]bool{}
	for _, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entries are skipped, not fatal
			}
			if !d.IsDir() {
				return nil
			}
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			gitPath := filepath.Join(path, ".git")
			if _, err := os.Lstat(gitPath); err != nil {
				return nil
			}
			if seen[path] {
				return filepath.SkipDir
			}
			seen[path] = true
			p, err := Inspect(path)
			if err == nil {
				out = append(out, p)
			}
			return filepath.SkipDir
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// Inspect reads one repository's git directory and remotes.
func Inspect(dir string) (Project, error) {
	gitDir, err := resolveGitDir(dir)
	if err != nil {
		return Project{}, err
	}
	urls, err := remoteURLs(dir)
	if err != nil {
		return Project{}, err
	}
	norm := make([]string, 0, len(urls))
	for _, u := range urls {
		if n := remote.Normalize(u); n != "" {
			norm = append(norm, n)
		}
	}
	sort.Strings(norm)
	return Project{Dir: dir, GitDir: gitDir, Remotes: norm}, nil
}

// resolveGitDir follows a ".git" file (worktree, submodule) to the main
// repository's git directory so that remotes come from there.
func resolveGitDir(dir string) (string, error) {
	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return gitPath, nil
	}
	b, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("%s: not a gitdir pointer", gitPath)
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	// a worktree points at <main>/.git/worktrees/<name>; remotes live in <main>/.git.
	// A submodule points at <super>/.git/modules/<name>, which has its own remotes.
	if filepath.Base(filepath.Dir(target)) == "worktrees" {
		return filepath.Dir(filepath.Dir(target)), nil
	}
	return target, nil
}

// remoteURLs asks git for every configured remote URL. Reading through git
// rather than parsing config by hand keeps includes and worktrees correct.
func remoteURLs(dir string) ([]string, error) {
	cmd := exec.Command("git", "-C", dir, "config", "--get-regexp", `^remote\..*\.url$`)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil // no remotes
		}
		return nil, fmt.Errorf("git config in %s: %w", dir, err)
	}
	var urls []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		_, url, ok := strings.Cut(sc.Text(), " ")
		if ok {
			urls = append(urls, url)
		}
	}
	return urls, nil
}

// Assign matches every project against the overlays. A project with two
// matching overlays is reported as a conflict and gets no layer.
func Assign(projects []Project, overlays []Overlay) []Assignment {
	out := make([]Assignment, 0, len(projects))
	for _, p := range projects {
		var hits []string
		for _, o := range overlays {
			if matches(o, p) {
				hits = append(hits, o.Name)
			}
		}
		a := Assignment{Project: p}
		switch len(hits) {
		case 0:
		case 1:
			a.Layer = hits[0]
		default:
			a.Conflict = hits
		}
		out = append(out, a)
	}
	return out
}

func matches(o Overlay, p Project) bool {
	for _, pat := range o.Patterns {
		for _, u := range p.Remotes {
			if remote.Match(pat, u) {
				return true
			}
		}
	}
	return false
}
