// Package check refuses content that must not enter a layer repository.
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding is one rejected line.
type Finding struct {
	File string
	Line int // 0 when the whole file is rejected
	Rule string
	Text string
}

func (f Finding) String() string {
	if f.Line == 0 {
		return fmt.Sprintf("%s: %s", f.File, f.Rule)
	}
	return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Rule, strings.TrimSpace(f.Text))
}

// Options scope the rules to one layer kind.
type Options struct {
	Base      bool     // apply the base-only rules
	Private   bool     // a base marked private in layer.toml: emails pass
	Blocklist []string // device blocklist words, base only
	HomeDir   string   // absolute home path to reject in the base
}

var (
	emailRe     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	includeIfRe = regexp.MustCompile(`(?i)^\s*\[includeIf\b`)
	userSectRe  = regexp.MustCompile(`(?i)^\s*\[user\]`)
	remoteURLRe = regexp.MustCompile(`(?i)^\s*url\s*=`)
	// secretRes catches the token shapes that leak most often. Entropy
	// scanning is deliberately left to gitleaks.
	secretRes = []*regexp.Regexp{
		regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`),
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
		regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd)["']?\s*[:=]\s*["']?[A-Za-z0-9/+_-]{16,}`),
	}
	tokenSplitRe = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// forbiddenNames are runtime files that never belong in any layer.
var forbiddenNames = []string{".claude.json", ".credentials.json", "history.jsonl"}

// File runs every rule on one file. relPath is the path inside the layer
// repository; it decides which file-level rules apply.
func File(relPath string, content []byte, opt Options) []Finding {
	var out []Finding
	base := filepath.Base(relPath)
	for _, n := range forbiddenNames {
		if strings.EqualFold(base, n) {
			out = append(out, Finding{File: relPath, Rule: "runtime file never belongs in a layer"})
			return out
		}
	}
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if strings.EqualFold(seg, "projects") {
			out = append(out, Finding{File: relPath, Rule: "projects/ holds transcripts and memory, never in a layer"})
			return out
		}
	}
	slashRel := filepath.ToSlash(relPath)
	isGit := strings.HasSuffix(relPath, ".gitconfig") || strings.HasPrefix(slashRel, "git/") || strings.Contains(slashRel, "/git/")
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		n := i + 1
		for _, re := range secretRes {
			if re.MatchString(line) {
				out = append(out, Finding{relPath, n, "looks like a secret", line})
				break
			}
		}
		if isGit {
			if userSectRe.MatchString(line) {
				out = append(out, Finding{relPath, n, "unconditional [user]; declare identities in layer.toml", line})
			}
			if remoteURLRe.MatchString(line) {
				out = append(out, Finding{relPath, n, "remote url in a git fragment", line})
			}
		}
		if !opt.Base {
			continue
		}
		if !opt.Private && emailRe.MatchString(line) && !isExampleEmail(line) {
			out = append(out, Finding{relPath, n, "email address in the base layer", line})
		}
		if includeIfRe.MatchString(line) {
			out = append(out, Finding{relPath, n, "includeIf in the base layer", line})
		}
		if opt.HomeDir != "" && (strings.Contains(line, opt.HomeDir) || strings.Contains(line, filepath.ToSlash(opt.HomeDir))) {
			out = append(out, Finding{relPath, n, "absolute home path; use ~ or $HOME", line})
		}
		if w := blockedWord(line, opt.Blocklist); w != "" {
			out = append(out, Finding{relPath, n, "blocklist word " + w, line})
		}
	}
	return out
}

// isExampleEmail lets documentation examples through.
func isExampleEmail(line string) bool {
	for _, m := range emailRe.FindAllString(line, -1) {
		if !strings.HasSuffix(strings.ToLower(m), "@example.com") && !strings.HasSuffix(strings.ToLower(m), ".example") {
			return false
		}
	}
	return true
}

// blockedWord returns the first blocklist word appearing as a whole token in
// line, comparing case-insensitively after splitting on non-alphanumerics.
func blockedWord(line string, blocklist []string) string {
	if len(blocklist) == 0 {
		return ""
	}
	tokens := map[string]bool{}
	for _, t := range tokenSplitRe.Split(strings.ToLower(line), -1) {
		if t != "" {
			tokens[t] = true
		}
	}
	for _, w := range blocklist {
		parts := tokenSplitRe.Split(strings.ToLower(w), -1)
		hit := true
		for _, p := range parts {
			if p != "" && !tokens[p] {
				hit = false
				break
			}
		}
		if hit && len(parts) > 0 {
			return w
		}
	}
	return ""
}

// Tree checks every regular file under dir, skipping .git.
func Tree(dir string, opt Options) ([]Finding, error) {
	// the layer directory itself may be a link, as a cloud drive folder often
	// is; links below it are findings
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	var out []Finding
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// a link is never followed: its target may be any file on the device
			rel, _ := filepath.Rel(dir, path)
			out = append(out, Finding{File: rel, Rule: "not a regular file; a layer holds regular files only"})
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, File(rel, b, opt)...)
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, err
}
