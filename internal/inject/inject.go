// Package inject writes an overlay's per-project files: declared keys of
// .claude/settings.local.json, a managed block in CLAUDE.local.md, and the
// global git excludes that keep both out of every repository.
package inject

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/fsx"
	"github.com/zhaojiannet/cclayer/internal/settings"
)

const (
	// BeginMarker and EndMarker delimit the managed block in CLAUDE.local.md.
	BeginMarker = "<!-- cclayer:begin -->"
	EndMarker   = "<!-- cclayer:end -->"
)

// SettingsLocal merges the owned keys of tmpl into the project's
// .claude/settings.local.json and reports whether the file changed.
// Everything else in the file, including permissions.allow, is preserved.
func SettingsLocal(projectDir string, tmpl settings.Doc, owned []string) (changed bool, err error) {
	path := filepath.Join(projectDir, ".claude", "settings.local.json")
	cur, err := settings.Load(path)
	if err != nil {
		return false, err
	}
	if len(settings.Merge(cur, tmpl, owned)) == 0 {
		return false, nil
	}
	b, err := cur.Marshal()
	if err != nil {
		return false, err
	}
	return true, fsx.WriteFile(projectDir, path, b, 0o644)
}

// RemoveSettingsLocal deletes the owned keys from the project's
// settings.local.json, leaving the rest. An empty result removes the file.
func RemoveSettingsLocal(projectDir string, owned []string) error {
	path := filepath.Join(projectDir, ".claude", "settings.local.json")
	cur, err := settings.Load(path)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return nil
	}
	for _, k := range owned {
		cur.Delete(k)
	}
	if len(cur) == 0 {
		return fsx.Remove(projectDir, path)
	}
	b, err := cur.Marshal()
	if err != nil {
		return err
	}
	return fsx.WriteFile(projectDir, path, b, 0o644)
}

// ClaudeLocal writes body between the markers of CLAUDE.local.md, creating
// the file when absent and preserving text outside the markers.
func ClaudeLocal(projectDir, body string) (changed bool, err error) {
	path := filepath.Join(projectDir, "CLAUDE.local.md")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	next, err := SetBlock(string(old), body)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if next == string(old) {
		return false, nil
	}
	return true, fsx.WriteFile(projectDir, path, []byte(next), 0o644)
}

// RemoveClaudeLocal drops the managed block; an otherwise empty file is deleted.
func RemoveClaudeLocal(projectDir string) error {
	path := filepath.Join(projectDir, "CLAUDE.local.md")
	old, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	rest := StripBlock(string(old))
	if strings.TrimSpace(rest) == "" {
		return fsx.Remove(projectDir, path)
	}
	return fsx.WriteFile(projectDir, path, []byte(rest), 0o644)
}

// SetBlock returns doc with the managed block replaced by body, appended when
// absent. One marker without the other is an error rather than a guess.
func SetBlock(doc, body string) (string, error) {
	block := BeginMarker + "\n" + strings.TrimRight(body, "\n") + "\n" + EndMarker + "\n"
	bi := strings.Index(doc, BeginMarker)
	ei := strings.Index(doc, EndMarker)
	switch {
	case bi < 0 && ei < 0:
		if doc != "" && !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		if doc != "" {
			doc += "\n"
		}
		return doc + block, nil
	case bi < 0 || ei < 0 || ei < bi:
		return "", fmt.Errorf("found one cclayer marker without the other; fix the file by hand")
	}
	end := ei + len(EndMarker)
	if end < len(doc) && doc[end] == '\n' {
		end++
	}
	return doc[:bi] + block + doc[end:], nil
}

// StripBlock returns doc without the managed block.
func StripBlock(doc string) string {
	bi := strings.Index(doc, BeginMarker)
	ei := strings.Index(doc, EndMarker)
	if bi < 0 || ei < 0 || ei < bi {
		return doc
	}
	end := ei + len(EndMarker)
	if end < len(doc) && doc[end] == '\n' {
		end++
	}
	return strings.TrimRight(doc[:bi], "\n") + strings.TrimRight("\n"+doc[end:], "\n") + "\n"
}

// Block extracts the current managed body, or "" when there is none.
func Block(doc string) string {
	bi := strings.Index(doc, BeginMarker)
	ei := strings.Index(doc, EndMarker)
	if bi < 0 || ei < 0 || ei < bi {
		return ""
	}
	return strings.Trim(doc[bi+len(BeginMarker):ei], "\n")
}

// ExcludePatterns are the two lines cclayer needs in the global excludes.
var ExcludePatterns = []string{"**/.claude/settings.local.json", "**/CLAUDE.local.md"}

// ExcludesFile resolves the global excludes path the way git does:
// core.excludesFile, else $XDG_CONFIG_HOME/git/ignore, else ~/.config/git/ignore.
func ExcludesFile() (string, error) {
	out, err := exec.Command("git", "config", "--global", "--get", "core.excludesFile").Output()
	if err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" {
			return expandHome(p), nil
		}
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "git", "ignore"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "git", "ignore"), nil
}

// EnsureExcludes appends the missing patterns to the excludes file and
// returns the ones it added. Lines already present, by cclayer or by Claude
// Code, are left alone.
func EnsureExcludes(path string) ([]string, error) {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(old), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var added []string
	content := string(old)
	for _, p := range ExcludePatterns {
		if have[p] {
			continue
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += p + "\n"
		added = append(added, p)
	}
	if len(added) == 0 {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = filepath.Dir(path)
	}
	return added, fsx.WriteUserFile(home, path, []byte(content), 0o644)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
