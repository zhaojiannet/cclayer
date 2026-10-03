// Package i18n translates cclayer's messages. The English text is the key,
// so a message without a translation still reads correctly. Errors stay in
// English: they are what people paste into searches and issues.
package i18n

import "strings"

// Languages lists the supported languages, English first.
var Languages = []string{"en", "zh", "ja"}

var tables = map[string]map[string]string{"zh": zh, "ja": ja}

var current = "en"

// Set selects the language for T; anything unsupported selects English.
func Set(lang string) {
	current = Normalize(lang)
	if current == "" {
		current = "en"
	}
}

// Lang is the language T translates into.
func Lang() string { return current }

// T returns s in the current language.
func T(s string) string {
	if t, ok := tables[current][s]; ok {
		return t
	}
	return s
}

// Normalize maps a language or locale name such as zh_CN.UTF-8 to en, zh
// or ja, and anything else to "".
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	for _, l := range Languages {
		if s == l || strings.HasPrefix(s, l+"_") || strings.HasPrefix(s, l+"-") {
			return l
		}
	}
	return ""
}

// Detect picks the language: CCLAYER_LANG, then the device manifest's lang,
// then the locale variables in the order the C library reads them. The
// first value naming any language decides, so LC_ALL=fr_FR means English
// rather than falling through to LANG.
func Detect(getenv func(string) string, configured string) string {
	if l := Normalize(getenv("CCLAYER_LANG")); l != "" {
		return l
	}
	if l := Normalize(configured); l != "" {
		return l
	}
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(k)
		if v == "" {
			continue
		}
		if l := Normalize(v); l != "" {
			return l
		}
		return "en"
	}
	return "en"
}
