package i18n

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"zh_CN.UTF-8": "zh", "zh-TW": "zh", "ja_JP.eucJP": "ja", "JA": "ja", "en_US": "en",
		"C": "", "POSIX": "", "fr_FR.UTF-8": "", "": "", "zhx": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		env        map[string]string
		configured string
		want       string
	}{
		{map[string]string{"LANG": "ja_JP.UTF-8"}, "", "ja"},
		{map[string]string{"LANG": "ja_JP.UTF-8"}, "zh", "zh"},
		{map[string]string{"CCLAYER_LANG": "en", "LANG": "zh_CN.UTF-8"}, "ja", "en"},
		// the first locale variable that is set decides, as in the C library
		{map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "zh_CN.UTF-8"}, "", "en"},
		{map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}, "", "zh"},
		{map[string]string{"CCLAYER_LANG": "klingon", "LANG": "ja_JP"}, "", "ja"},
		{map[string]string{}, "", "en"},
	}
	for _, c := range cases {
		if got := Detect(env(c.env), c.configured); got != c.want {
			t.Errorf("Detect(%v, %q) = %q, want %q", c.env, c.configured, got, c.want)
		}
	}
}

func TestT(t *testing.T) {
	defer Set("en")
	Set("zh_CN.UTF-8")
	if T("Proceed?") != "继续吗？" {
		t.Errorf("zh: %q", T("Proceed?"))
	}
	if T("a text without a translation") != "a text without a translation" {
		t.Error("an untranslated text must come back as it is")
	}
	Set("fr")
	if Lang() != "en" || T("Proceed?") != "Proceed?" {
		t.Errorf("an unsupported language must select English, got %s", Lang())
	}
}
