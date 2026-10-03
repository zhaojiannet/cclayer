// Package settings merges Claude Code settings.json documents by owned JSON
// paths. Everything outside the owned paths is left exactly as found.
package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Doc is a parsed settings.json. Keys keep their JSON types; objects are
// map[string]any, arrays []any.
type Doc map[string]any

// Load reads a settings file. A missing file is an empty document.
func Load(path string) (Doc, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Doc{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse decodes JSON into a Doc.
func Parse(b []byte) (Doc, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return Doc{}, nil
	}
	var d Doc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	if d == nil {
		d = Doc{}
	}
	return d, nil
}

// Marshal renders a Doc with two-space indentation and sorted keys, the
// layout Claude Code itself writes. HTML escaping is off so hook commands
// with && or > survive a round trip unchanged.
func (d Doc) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any(d)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Get returns the value at a dotted path and whether it exists.
func (d Doc) Get(path string) (any, bool) {
	var cur any = map[string]any(d)
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Set writes a value at a dotted path, creating intermediate objects. It
// fails when an intermediate value exists and is not an object.
func (d Doc) Set(path string, v any) error {
	keys := strings.Split(path, ".")
	cur := map[string]any(d)
	for i, k := range keys[:len(keys)-1] {
		next, ok := cur[k]
		if !ok {
			m := map[string]any{}
			cur[k] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("settings: %q is not an object", strings.Join(keys[:i+1], "."))
		}
		cur = m
	}
	cur[keys[len(keys)-1]] = v
	return nil
}

// Delete removes the value at a dotted path and prunes objects that become
// empty along the way.
func (d Doc) Delete(path string) {
	keys := strings.Split(path, ".")
	var walk func(m map[string]any, keys []string) bool
	walk = func(m map[string]any, keys []string) bool {
		if len(keys) == 1 {
			delete(m, keys[0])
			return len(m) == 0
		}
		child, ok := m[keys[0]].(map[string]any)
		if !ok {
			return false
		}
		if walk(child, keys[1:]) {
			delete(m, keys[0])
		}
		return len(m) == 0
	}
	walk(d, keys)
}

// Merge copies the owned paths from src into dst and returns the paths whose
// value changed. Paths missing in src are removed from dst, because the
// owner has the final say over them. Paths not listed are untouched.
func Merge(dst, src Doc, owned []string) []string {
	var changed []string
	for _, p := range owned {
		sv, ok := src.Get(p)
		dv, had := dst.Get(p)
		switch {
		case ok && (!had || !equal(sv, dv)):
			_ = dst.Set(p, sv)
			changed = append(changed, p)
		case !ok && had:
			dst.Delete(p)
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed
}

// Diff returns the owned paths whose values differ between a and b.
func Diff(a, b Doc, owned []string) []string {
	var out []string
	for _, p := range owned {
		av, aok := a.Get(p)
		bv, bok := b.Get(p)
		if aok != bok || (aok && !equal(av, bv)) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Extract returns a new Doc holding only the owned paths of d.
func Extract(d Doc, owned []string) Doc {
	out := Doc{}
	for _, p := range owned {
		if v, ok := d.Get(p); ok {
			_ = out.Set(p, v)
		}
	}
	return out
}

func equal(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
