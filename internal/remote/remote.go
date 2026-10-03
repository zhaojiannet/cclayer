// Package remote normalizes git remote URLs and matches them against layer
// patterns, mirroring what git's includeIf "hasconfig:remote.*.url" sees.
package remote

import (
	"strings"
)

// Normalize reduces the common spellings of a git remote URL to
// "host/path" without scheme, user, port, ".git" suffix or trailing slash.
// The host is lower-cased; the path keeps its case for display, and Match
// compares it without case.
//
//	https://github.com/Org/Repo.git  -> github.com/Org/Repo
//	git@github.com:Org/Repo.git      -> github.com/Org/Repo
//	ssh://git@github.com/Org/Repo    -> github.com/Org/Repo
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// scp-like syntax: [user@]host:path, only when no scheme is present
	if !strings.Contains(s, "://") {
		if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
			s = s[:i] + "/" + strings.TrimPrefix(s[i+1:], "/")
		}
	} else {
		s = s[strings.Index(s, "://")+3:]
	}
	if i := strings.Index(s, "@"); i >= 0 && (strings.Index(s, "/") < 0 || i < strings.Index(s, "/")) {
		s = s[i+1:]
	}
	host, path, _ := strings.Cut(s, "/")
	if i := strings.Index(host, ":"); i >= 0 { // port
		host = host[:i]
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	path = strings.TrimSuffix(path, "/")
	return strings.ToLower(host) + "/" + path
}

// Match reports whether a normalized URL matches a layer pattern such as
// "github.com/acme-inc/*". "*" matches within one path segment, "**"
// matches across segments. The pattern is normalized the same way as the URL
// so "https://github.com/org/*" and "github.com/org/*" are equivalent.
// Case does not matter: hosting services treat owner and repository names
// without case, so github.com/Acme-Inc/api is the same repository as
// github.com/acme-inc/api, and the includeIf lines say the same to git.
func Match(pattern, normalizedURL string) bool {
	p := strings.ToLower(Normalize(pattern))
	return globMatch(strings.Split(p, "/"), strings.Split(strings.ToLower(normalizedURL), "/"))
}

func globMatch(pat, parts []string) bool {
	for len(pat) > 0 {
		switch pat[0] {
		case "**":
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(parts); i++ {
				if globMatch(pat[1:], parts[i:]) {
					return true
				}
			}
			return false
		default:
			if len(parts) == 0 || !segmentMatch(pat[0], parts[0]) {
				return false
			}
			pat, parts = pat[1:], parts[1:]
		}
	}
	return len(parts) == 0
}

// segmentMatch matches one path segment with "*" as the only wildcard.
func segmentMatch(pat, s string) bool {
	if pat == "*" {
		return s != ""
	}
	if !strings.Contains(pat, "*") {
		return pat == s
	}
	pieces := strings.Split(pat, "*")
	if !strings.HasPrefix(s, pieces[0]) {
		return false
	}
	s = s[len(pieces[0]):]
	for i := 1; i < len(pieces); i++ {
		piece := pieces[i]
		if i == len(pieces)-1 {
			return strings.HasSuffix(s, piece)
		}
		idx := strings.Index(s, piece)
		if idx < 0 {
			return false
		}
		s = s[idx+len(piece):]
	}
	return true
}

// IncludeIfPatterns returns the URL spellings git may see for a layer
// pattern, in the form used by includeIf "hasconfig:remote.*.url:<pattern>".
// A pattern ending in a wildcard segment becomes "<prefix>/**", which git
// matches against every repository under it with or without ".git". A
// pattern naming one repository yields the exact URL and the ".git" form,
// because "/**" needs a slash that a repository URL does not have.
func IncludeIfPatterns(pattern string) []string {
	p := Normalize(pattern)
	host, path, _ := strings.Cut(p, "/")
	var tails []string
	switch {
	case strings.HasSuffix(path, "/**"), strings.HasSuffix(path, "/*"):
		tails = []string{strings.TrimRight(strings.TrimSuffix(strings.TrimSuffix(path, "/**"), "/*"), "/") + "/**"}
	default:
		tails = []string{path, path + ".git"}
	}
	host = foldGlob(host)
	var out []string
	for _, t := range tails {
		t = foldGlob(t)
		out = append(out,
			"https://"+host+"/"+t,
			"git@"+host+":"+t,
			"ssh://git@"+host+"/"+t,
		)
	}
	return out
}

// foldGlob spells every letter as a two-case bracket class, [aA], so git's
// case-sensitive wildmatch accepts the URL in any case, as Match does.
func foldGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		lo, up := strings.ToLower(string(r)), strings.ToUpper(string(r))
		if lo != up {
			b.WriteString("[" + lo + up + "]")
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// OrgSegment returns the organization part of a pattern: the first path
// segment after the host. It feeds the device blocklist.
func OrgSegment(pattern string) string {
	p := Normalize(pattern)
	_, path, _ := strings.Cut(p, "/")
	seg, _, _ := strings.Cut(path, "/")
	if strings.ContainsAny(seg, "*") {
		return ""
	}
	return seg
}
