package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/claudecli"
	"github.com/zhaojiannet/cclayer/internal/settings"
)

// layerPath joins rel to a layer directory after checking that no part of
// rel is a symbolic link: a link in a pulled repository could point at
// ~/.ssh or any other file, which apply would then copy, print or inject.
// The layer directory itself may be a link, as a cloud drive folder often is.
func layerPath(dir, rel string) (string, error) {
	p := dir
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "" || seg == "." {
			continue
		}
		p = filepath.Join(p, seg)
		fi, err := os.Lstat(p)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a symbolic link; a layer holds regular files only", filepath.ToSlash(filepath.Join(filepath.Base(dir), rel)))
		}
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}

// baseSettings loads claude/settings.json from the base clone.
func baseSettings(base Layer) (settings.Doc, error) {
	p, err := layerPath(base.Dir, "claude/settings.json")
	if err != nil {
		return nil, err
	}
	return settings.Load(p)
}

// pluginsFrom reads enabledPlugins (true entries) and extraKnownMarketplaces
// out of a settings document in the shape Claude Code writes.
func pluginsFrom(doc settings.Doc) (marketplaces []claudecli.Marketplace, plugins []string) {
	if v, ok := doc.Get("extraKnownMarketplaces"); ok {
		if m, ok := v.(map[string]any); ok {
			for name, entry := range m {
				src := marketplaceSource(entry)
				if src != "" {
					marketplaces = append(marketplaces, claudecli.Marketplace{Name: name, Source: src})
				}
			}
		}
	}
	if v, ok := doc.Get("enabledPlugins"); ok {
		if m, ok := v.(map[string]any); ok {
			for name, on := range m {
				if b, ok := on.(bool); ok && b {
					plugins = append(plugins, name)
				}
			}
		}
	}
	sort.Slice(marketplaces, func(i, j int) bool { return marketplaces[i].Name < marketplaces[j].Name })
	sort.Strings(plugins)
	return marketplaces, plugins
}

// marketplaceSource turns an extraKnownMarketplaces entry into the argument
// `claude plugin marketplace add` takes: owner/repo for github, else the url
// or path as written.
func marketplaceSource(entry any) string {
	m, ok := entry.(map[string]any)
	if !ok {
		return ""
	}
	src, ok := m["source"].(map[string]any)
	if !ok {
		return ""
	}
	switch src["source"] {
	case "github":
		if repo, ok := src["repo"].(string); ok {
			return repo
		}
	case "git", "url":
		if u, ok := src["url"].(string); ok {
			return u
		}
	case "directory", "path":
		if p, ok := src["path"].(string); ok {
			return p
		}
	}
	return ""
}

// mcpServers reads mcp/<name>.json files from the base clone.
func mcpServers(base Layer) ([]claudecli.MCPServer, error) {
	dir, err := layerPath(base.Dir, "mcp")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []claudecli.MCPServer
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("mcp/%s is not a regular file; a layer holds regular files only", e.Name())
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, claudecli.MCPServer{Name: strings.TrimSuffix(e.Name(), ".json"), JSON: strings.TrimSpace(string(b))})
	}
	return out, nil
}

// overlayTemplate loads project/settings.local.json from an overlay clone,
// or an empty document when the overlay has none.
func overlayTemplate(o Layer) (settings.Doc, error) {
	p, err := layerPath(o.Dir, "project/settings.local.json")
	if err != nil {
		return nil, err
	}
	return settings.Load(p)
}

// overlayClaudeLocal returns the managed block body, or "" when the overlay
// declares none.
func overlayClaudeLocal(o Layer) (string, error) {
	if o.Manifest.Inject == nil || o.Manifest.Inject.ClaudeLocal == "" {
		return "", nil
	}
	p, err := layerPath(o.Dir, o.Manifest.Inject.ClaudeLocal)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// injectKeys returns the settings paths an overlay writes into projects.
func injectKeys(o Layer) []string {
	if o.Manifest.Inject == nil {
		return nil
	}
	return o.Manifest.Inject.SettingsKeys
}

// fragment reads a layer's raw git fragment, "" when it has none.
func fragment(l Layer) (string, error) {
	if l.Manifest.Git == nil || l.Manifest.Git.Fragment == "" {
		return "", nil
	}
	p, err := layerPath(l.Dir, l.Manifest.Git.Fragment)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// patterns returns an overlay's match patterns.
func patterns(o Layer) []string {
	var out []string
	for _, m := range o.Manifest.Match {
		out = append(out, m.Remote)
	}
	return out
}
