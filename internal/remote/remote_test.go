package remote

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"https://github.com/Org/Repo.git":   "github.com/Org/Repo",
		"git@github.com:Org/Repo.git":       "github.com/Org/Repo",
		"ssh://git@github.com/Org/Repo":     "github.com/Org/Repo",
		"ssh://git@GitHub.com:22/Org/Repo/": "github.com/Org/Repo",
		"https://user@gitlab.com/g/sub/r":   "gitlab.com/g/sub/r",
		"":                                  "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         bool
	}{
		{"github.com/acme-inc/*", "github.com/acme-inc/api", true},
		{"github.com/acme-inc/*", "github.com/acme-inc/api/sub", false},
		{"github.com/acme-inc/**", "github.com/acme-inc/api/sub", true},
		{"github.com/acme-inc/*", "github.com/Acme-Inc/api", true},
		{"github.com/Acme-Inc/API", "github.com/acme-inc/api", true},
		{"github.com/acme-inc/api", "github.com/acme-inc/api", true},
		{"github.com/acme-inc/*", "github.com/acme-inc-forks/api", false},
		{"https://github.com/acme-inc/*", "github.com/acme-inc/api", true},
		{"gitlab.com/group/*/web", "gitlab.com/group/x/web", true},
		{"github.com/me/*", "github.com/me", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.url); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.url, got, c.want)
		}
	}
}

func TestIncludeIfPatterns(t *testing.T) {
	got := IncludeIfPatterns("github.com/acme-inc/*")
	want := []string{
		"https://[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/**",
		"git@[gG][iI][tT][hH][uU][bB].[cC][oO][mM]:[aA][cC][mM][eE]-[iI][nN][cC]/**",
		"ssh://git@[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/**",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pattern %d = %q, want %q", i, got[i], want[i])
		}
	}
	exact := IncludeIfPatterns("github.com/acme-inc/only")
	if len(exact) != 6 || exact[0] != "https://[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/[oO][nN][lL][yY]" || exact[3] != "https://[gG][iI][tT][hH][uU][bB].[cC][oO][mM]/[aA][cC][mM][eE]-[iI][nN][cC]/[oO][nN][lL][yY].[gG][iI][tT]" {
		t.Errorf("exact repository pattern wrong: %v", exact)
	}
}

func TestOrgSegment(t *testing.T) {
	if got := OrgSegment("https://github.com/acme-inc/*"); got != "acme-inc" {
		t.Errorf("got %q", got)
	}
	if got := OrgSegment("github.com/*/x"); got != "" {
		t.Errorf("wildcard org should be empty, got %q", got)
	}
}

func TestFoldGlob(t *testing.T) {
	if got := foldGlob("Ab-1.c/**"); got != "[aA][bB]-1.[cC]/**" {
		t.Errorf("foldGlob = %q", got)
	}
}
