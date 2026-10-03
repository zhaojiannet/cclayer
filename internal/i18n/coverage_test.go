package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// messages collects the English texts the program translates, from the
// packages that print them: arguments of T, the formats of the Env helpers
// that translate on their own (printf, confirm, report, rep.add, ask), and
// the titles and options of the setup forms, which must go through T.
func messages(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range []string{"../cli", "../keys"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			consts := map[string]string{}
			ast.Inspect(f, func(n ast.Node) bool {
				if v, ok := n.(*ast.ValueSpec); ok {
					for i, name := range v.Names {
						if i < len(v.Values) {
							if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								consts[name.Name], _ = strconv.Unquote(lit.Value)
							}
						}
					}
				}
				return true
			})
			literal := func(e ast.Expr) (string, bool) {
				switch x := e.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						s, _ := strconv.Unquote(x.Value)
						return s, true
					}
				case *ast.Ident:
					s, ok := consts[x.Name]
					return s, ok
				}
				return "", false
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				pos := fset.Position(call.Pos()).String()
				arg := -1
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					recv, _ := fn.X.(*ast.Ident)
					switch {
					case recv != nil && recv.Name == "i18n" && fn.Sel.Name == "T":
						if s, ok := literal(call.Args[0]); ok {
							found[s] = pos
						} else if id, ok := call.Args[0].(*ast.Ident); ok && (id.Name == "format" || id.Name == "prompt") {
							// inside a helper that translates its parameter; the
							// callers' literals are collected instead
						} else {
							t.Errorf("%s: i18n.T needs a literal or a constant, or the text cannot be checked", pos)
						}
						return true
					case fn.Sel.Name == "printf" || fn.Sel.Name == "confirm":
						arg = 0
					case recv != nil && recv.Name == "rep" && fn.Sel.Name == "add":
						arg = 1
					case fn.Sel.Name == "Title" || fn.Sel.Name == "Description" || recv != nil && recv.Name == "huh" && fn.Sel.Name == "NewOption":
						if s, ok := literal(call.Args[0]); ok && hasWords(s) {
							t.Errorf("%s: %q is shown untranslated; wrap it in i18n.T", pos, s)
						}
					}
				case *ast.Ident:
					switch fn.Name {
					case "ask":
						arg = 0
					case "report":
						arg = 1
					}
				}
				if arg >= 0 && arg < len(call.Args) {
					if s, ok := literal(call.Args[arg]); ok && hasWords(s) {
						found[s] = pos
					}
				}
				return true
			})
		}
	}
	return found
}

var (
	verbRe  = regexp.MustCompile(`%(\[\d+\])?[-+# 0-9.]*[a-zA-Z%]`)
	indexRe = regexp.MustCompile(`\[\d+\]`)
)

// hasWords is false for formats such as "  %s\n" that have nothing to
// translate.
func hasWords(s string) bool {
	return regexp.MustCompile(`[A-Za-z]{2,}`).MatchString(verbRe.ReplaceAllString(s, ""))
}

// verbs lists the formatting verbs of s without argument indexes, so a
// translation may reorder its arguments with %[2]s.
func verbs(s string) []string {
	v := verbRe.FindAllString(s, -1)
	for i := range v {
		v[i] = indexRe.ReplaceAllString(v[i], "")
	}
	sort.Strings(v)
	return v
}

func TestEveryMessageIsTranslated(t *testing.T) {
	found := messages(t)
	if len(found) < 100 {
		t.Fatalf("only %d messages found; the scan is broken", len(found))
	}
	for lang, table := range tables {
		for en, pos := range found {
			tr, ok := table[en]
			if !ok {
				t.Errorf("%s: no %s translation for %q", pos, lang, en)
				continue
			}
			if strings.Join(verbs(tr), " ") != strings.Join(verbs(en), " ") {
				t.Errorf("%s: %s translation of %q has verbs %v, want %v", pos, lang, en, verbs(tr), verbs(en))
			}
			if strings.HasSuffix(en, "\n") != strings.HasSuffix(tr, "\n") {
				t.Errorf("%s translation of %q differs in its trailing newline", lang, en)
			}
		}
		for en := range table {
			if _, ok := found[en]; !ok {
				t.Errorf("%s table holds %q, which the program no longer prints", lang, en)
			}
		}
	}
}
