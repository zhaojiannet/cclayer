package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/manifest"
)

func newUI(t *testing.T, d *manifest.Device, existing bool) *setupUI {
	t.Helper()
	home := t.TempDir()
	e := &Env{Home: home, DevicePath: filepath.Join(home, ".config", "cclayer", "device.toml"), Stdout: &bytes.Buffer{}}
	d.EnsureMaps()
	w := &wizard{e: e}
	m := newSetupUI(w, d, existing)
	m.width = 110
	return m
}

// press sends keys: names such as "enter" and "down", or text to type.
func press(m *setupUI, keys ...string) {
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp,
		"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "space": tea.KeySpace}
	for _, k := range keys {
		if c, ok := codes[k]; ok {
			m.Update(tea.KeyPressMsg{Code: c})
			continue
		}
		for _, r := range k {
			m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func screenText(m *setupUI) string { return ansiRe.ReplaceAllString(m.render(), "") }

// A new device opens on the base layer's address, the first thing Save
// needs; Save without it stays and says what is missing.
func TestSetupScreenFreshDevice(t *testing.T) {
	m := newUI(t, &manifest.Device{}, false)
	if m.focus != areaDetail || m.current().key != "layer" || m.current().layer.name != "base" {
		t.Fatalf("cursor should wait on the base address, got focus %d item %+v", m.focus, m.current())
	}
	text := screenText(m)
	for _, want := range []string{"Layers", "This device", "Base layer", "Still needed: base layer address", "Save and apply"} {
		if !strings.Contains(text, want) {
			t.Errorf("screen lacks %q:\n%s", want, text)
		}
	}
	m.focus, m.button = areaButtons, 0
	press(m, "enter")
	if m.saved || !strings.Contains(m.msg, "base layer address") {
		t.Errorf("save without a base must be refused with the reason, msg %q", m.msg)
	}
	if m.focus != areaDetail {
		t.Error("a refused save must take the cursor to the missing field")
	}
}

// Typing an address, adding and naming an overlay, then saving.
func TestSetupScreenEditAndSave(t *testing.T) {
	m := newUI(t, &manifest.Device{}, false)
	base := t.TempDir()
	os.WriteFile(filepath.Join(base, "layer.toml"), []byte("[layer]\nname = \"base\"\nkind = \"base\"\n"), 0o644)
	press(m, "enter", base, "enter")
	if m.dr.layers[0].loc != base || m.editing {
		t.Fatalf("base address not taken: %q editing=%v err=%q", m.dr.layers[0].loc, m.editing, m.editErr)
	}
	// list: base, add; the add row opens a name field right away
	m.focus, m.item = areaList, 1
	press(m, "enter", "acme", "enter")
	if len(m.dr.layers) != 2 || m.dr.layers[1].name != "acme" {
		t.Fatalf("overlay not added: %+v", m.dr.layers)
	}
	if m.dr.identity != "" {
		t.Errorf("naming a new overlay must not make it the default identity, got %q", m.dr.identity)
	}
	press(m, "down", "enter", "git@github.com:you/cclayer-acme.git", "enter")
	if m.dr.layers[1].loc != "git@github.com:you/cclayer-acme.git" {
		t.Fatalf("overlay address not taken: %q (%s)", m.dr.layers[1].loc, m.editErr)
	}
	press(m, "down", "right")
	if m.dr.layers[1].access != accessKey {
		t.Errorf("right on the access field should pick the deploy key, got %s", m.dr.layers[1].access)
	}
	if !strings.Contains(screenText(m), "~/.local/share/cclayer/acme") {
		t.Errorf("the overlay should say where saving clones it:\n%s", screenText(m))
	}
	m.focus, m.button = areaButtons, 1
	press(m, "enter")
	if !m.saved || m.applyNow {
		t.Errorf("Save only: saved=%v applyNow=%v msg=%q", m.saved, m.applyNow, m.msg)
	}
}

// A wrong value stays in the field with the reason; Esc on a new overlay
// with no name drops it; Remove drops one that has a name.
func TestSetupScreenCorrections(t *testing.T) {
	m := newUI(t, &manifest.Device{}, false)
	file := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	press(m, "enter", file, "enter")
	if !m.editing || !strings.Contains(m.editErr, "is a file") || m.dr.layers[0].loc != "" {
		t.Errorf("a file as the base address must stay in the editor with the reason: editing=%v err=%q", m.editing, m.editErr)
	}
	press(m, "esc")
	m.focus, m.item = areaList, 1
	press(m, "enter", "base", "enter")
	if !m.editing || m.editErr == "" {
		t.Errorf("an overlay named base must be refused in place: editing=%v err=%q", m.editing, m.editErr)
	}
	press(m, "esc")
	press(m, "enter", "esc")
	if len(m.dr.layers) != 1 {
		t.Errorf("an overlay left without a name must go: %d layers", len(m.dr.layers))
	}
	press(m, "enter", "globex", "enter")
	_, _, fields, _ := m.detail()
	m.focus, m.field = areaDetail, len(fields)-1
	press(m, "enter")
	if len(m.dr.layers) != 1 {
		t.Errorf("Remove must drop the overlay: %d layers", len(m.dr.layers))
	}
}

// Space switches auto pull from the list; q with changes asks twice.
func TestSetupScreenToggleAndQuit(t *testing.T) {
	m := newUI(t, &manifest.Device{}, false)
	for i, it := range m.items() {
		if it.key == "autopull" {
			m.focus, m.item = areaList, i
		}
	}
	press(m, "space")
	if !m.dr.autoPull || !m.dirty {
		t.Fatal("space must switch auto pull on")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil || !m.quitArmed {
		t.Fatal("q with unsaved changes must ask first")
	}
	if _, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil || m.saved {
		t.Error("a second q must quit without saving")
	}
}

// The language row switches the screen at once.
func TestSetupScreenLanguage(t *testing.T) {
	defer i18n.Set("en")
	m := newUI(t, &manifest.Device{}, false)
	for i, it := range m.items() {
		if it.key == "lang" {
			m.focus, m.item = areaList, i
		}
	}
	press(m, "enter", "right", "right")
	if m.dr.lang != "zh" || !strings.Contains(screenText(m), "这台设备") {
		t.Errorf("lang %q; screen:\n%s", m.dr.lang, screenText(m))
	}
}

// Both layouts render every box, at a wide and at a narrow width.
func TestSetupScreenLayouts(t *testing.T) {
	defer i18n.Set("en")
	for _, lang := range []string{"en", "zh", "ja"} {
		i18n.Set(lang)
		for _, w := range []int{120, 80} {
			m := newUI(t, &manifest.Device{}, false)
			m.width = w
			text := screenText(m)
			for _, line := range strings.Split(text, "\n") {
				if lw := len([]rune(line)); lw > 0 && displayWidth(line) > w {
					t.Errorf("%s at %d columns: a line is %d cells wide:\n%s", lang, w, displayWidth(line), line)
				}
			}
			if testing.Verbose() {
				t.Logf("%s, %d columns:\n%s", lang, w, text)
			}
		}
	}
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe30 && r <= 0xfe4f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// An overlay the manifest lists stays a saved one while its directory is
// missing, say a cloud folder still syncing: no name field, no Remove, and
// saving it unchanged writes nothing into the folder.
func TestSetupScreenSavedOverlayWithMissingDir(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "Drive", "acme")
	d := &manifest.Device{Layers: []string{"base", "acme"}, Roots: []string{"~/Projects"},
		Clone: map[string]string{"base": t.TempDir(), "acme": gone}}
	m := newUI(t, d, true)
	for i, it := range m.items() {
		if it.key == "layer" && it.layer.name == "acme" {
			m.focus, m.item = areaList, i
		}
	}
	_, _, fields, note := m.detail()
	for _, f := range fields {
		if f.id == "name" || f.id == "delete" {
			t.Errorf("a saved overlay must not offer %s", f.id)
		}
	}
	if !strings.Contains(note, "cclayer leave acme") {
		t.Errorf("the note should point to leave: %q", note)
	}
	if err := m.w.commit(d, m.dr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gone); err == nil {
		t.Error("saving an unchanged missing directory must not create it")
	}
}

// A starter that cannot be written stops the save before any credential
// is touched.
func TestSetupCommitStarterFailsFirst(t *testing.T) {
	home := t.TempDir()
	blocker := filepath.Join(home, "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	d := &manifest.Device{Layers: []string{"base"}, Roots: []string{"~/Projects"},
		Clone: map[string]string{"base": "~/.local/share/cclayer/base"},
		Repo:  map[string]string{"base": "git@github.com:you/cclayer-base.git"},
		Auth:  map[string]string{"base": "token"}}
	m := newUI(t, d, true)
	m.dr.layers[0].access = accessNone
	o := m.dr.newOverlay()
	// a path below a regular file passes as new but cannot be created
	o.name, o.loc, o.author, o.email, o.remote = "acme", filepath.Join(blocker, "x", "acme"), "A", "a@acme.example", "github.com/acme-inc/*"
	m.dr.layers = append(m.dr.layers, o)
	if err := m.w.commit(d, m.dr); err == nil {
		t.Skip("the platform created a directory below a file")
	}
	if d.Auth["base"] != "token" {
		t.Error("the base credential was dropped although the save failed")
	}
}
