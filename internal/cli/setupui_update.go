package cli

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/manifest"
)

var buttons = []string{"apply", "save", "quit"}

func (m *setupUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	if m.editing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *setupUI) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" {
		m.saved = false
		return m, tea.Quit
	}
	if m.editing {
		return m.editKey(k)
	}
	if s != "q" {
		m.quitArmed = false
	}
	if m.help {
		m.help = false
		return m, nil
	}
	switch s {
	case "?":
		m.help = true
		return m, nil
	case "q":
		return m.quit()
	case "tab":
		m.cycleArea(1)
		return m, nil
	case "shift+tab":
		m.cycleArea(-1)
		return m, nil
	}
	switch m.focus {
	case areaList:
		return m.listKey(s)
	case areaDetail:
		return m.detailKey(s)
	}
	return m.buttonKey(s)
}

func (m *setupUI) cycleArea(dir int) {
	order := []area{areaList, areaDetail, areaButtons}
	i := slices.Index(order, m.focus)
	for range order {
		i = (i + dir + len(order)) % len(order)
		if order[i] != areaDetail || m.hasFields() {
			break
		}
	}
	m.focus = order[i]
	m.field = 0
}

func (m *setupUI) hasFields() bool {
	_, _, fields, _ := m.detail()
	for _, f := range fields {
		if f.kind != fieldInfo {
			return true
		}
	}
	return false
}

func (m *setupUI) listKey(s string) (tea.Model, tea.Cmd) {
	n := len(m.items())
	switch s {
	case "up", "k":
		m.item = max(m.item-1, 0)
	case "down", "j":
		if m.item == n-1 {
			m.focus = areaButtons
		} else {
			m.item++
		}
	case "home":
		m.item = 0
	case "end":
		m.item = n - 1
	case "space":
		switch m.current().key {
		case "autopull", "profiles":
			m.cycle(m.current().key, 1)
		}
	case "enter", "right", "l":
		if m.current().key == "add" {
			return m, m.addOverlay()
		}
		if m.hasFields() {
			m.focus, m.field = areaDetail, m.firstField()
		}
	case "esc":
		return m.quit()
	}
	return m, nil
}

// firstField is the first field that is not information only.
func (m *setupUI) firstField() int {
	_, _, fields, _ := m.detail()
	for i, f := range fields {
		if f.kind != fieldInfo {
			return i
		}
	}
	return 0
}

func (m *setupUI) detailKey(s string) (tea.Model, tea.Cmd) {
	_, _, fields, _ := m.detail()
	if len(fields) == 0 {
		m.focus = areaList
		return m, nil
	}
	m.field = min(m.field, len(fields)-1)
	f := fields[m.field]
	move := func(dir int) {
		for i := m.field + dir; i >= 0 && i < len(fields); i += dir {
			if fields[i].kind != fieldInfo {
				m.field = i
				return
			}
		}
	}
	switch s {
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "esc":
		m.focus = areaList
	case "left", "h":
		if f.kind == fieldChoice {
			m.cycle(f.id, -1)
		} else {
			m.focus = areaList
		}
	case "right", "l":
		if f.kind == fieldChoice {
			m.cycle(f.id, 1)
		}
	case "space":
		if f.kind == fieldChoice {
			m.cycle(f.id, 1)
		}
	case "enter":
		switch f.kind {
		case fieldChoice:
			m.cycle(f.id, 1)
		case fieldText:
			return m, m.startEdit(f)
		case fieldButton:
			m.press(f.id)
		}
	}
	return m, nil
}

func (m *setupUI) buttonKey(s string) (tea.Model, tea.Cmd) {
	switch s {
	case "left", "h":
		m.button = max(m.button-1, 0)
	case "right", "l":
		m.button = min(m.button+1, len(buttons)-1)
	case "up", "k", "esc":
		m.focus = areaList
	case "enter", "space":
		switch buttons[m.button] {
		case "quit":
			return m.quit()
		default:
			return m.save(buttons[m.button] == "apply")
		}
	}
	return m, nil
}

// quit leaves without saving; with unsaved changes it asks for a second
// press first.
func (m *setupUI) quit() (tea.Model, tea.Cmd) {
	if m.dirty && !m.quitArmed {
		m.quitArmed = true
		m.say(i18n.T("There are unsaved changes. Press q again to quit without saving."), true)
		return m, nil
	}
	m.saved = false
	return m, tea.Quit
}

func (m *setupUI) save(applyNow bool) (tea.Model, tea.Cmd) {
	if gaps := m.dr.gaps(); len(gaps) > 0 {
		m.say(m.gapLine(), true)
		m.jumpTo(gaps[0])
		return m, nil
	}
	m.saved, m.applyNow = true, applyNow
	return m, tea.Quit
}

func (m *setupUI) say(s string, isErr bool) {
	m.msg, m.msgIsErr = s, isErr
}

// gapLine is the status line's list of what Save still needs.
func (m *setupUI) gapLine() string {
	var what []string
	for _, g := range m.dr.gaps() {
		what = append(what, g.what)
	}
	if len(what) == 0 {
		return ""
	}
	return i18n.T("Still needed:") + " " + strings.Join(what, i18n.T(", "))
}

// jumpToNext puts the cursor on the first thing Save needs, or on the
// buttons when nothing is missing.
func (m *setupUI) jumpToNext() {
	if gaps := m.dr.gaps(); len(gaps) > 0 {
		m.jumpTo(gaps[0])
		return
	}
	m.focus, m.button = areaButtons, 0
}

func (m *setupUI) jumpTo(g gap) {
	for i, it := range m.items() {
		if g.layer != nil && it.layer == g.layer || g.layer == nil && it.key == g.field {
			m.item = i
			break
		}
	}
	m.focus = areaDetail
	_, _, fields, _ := m.detail()
	m.field = m.firstField()
	for i, f := range fields {
		if f.id == g.field {
			m.field = i
		}
	}
}

func (m *setupUI) addOverlay() tea.Cmd {
	l := m.dr.newOverlay()
	m.dr.layers = append(m.dr.layers, l)
	m.dirty = true
	for i, it := range m.items() {
		if it.layer == l {
			m.item = i
		}
	}
	m.focus, m.field = areaDetail, 0
	_, _, fields, _ := m.detail()
	return m.startEdit(fields[0])
}

func (m *setupUI) startEdit(f uiField) tea.Cmd {
	m.editing, m.editErr = true, ""
	m.input.SetValue(f.value)
	m.input.CursorEnd()
	m.input.Placeholder = ""
	if f.id == "loc" {
		m.input.Placeholder = "https://github.com/you/cclayer-" + cmpOr(m.current().layer.name, "base") + ".git"
	}
	return m.input.Focus()
}

func (m *setupUI) editKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.editing, m.editErr = false, ""
		m.input.Blur()
		// a new overlay left without a name is not kept
		if it := m.current(); it.key == "layer" && it.layer.name == "" && !it.layer.saved && it.layer.loc == "" {
			m.dr.remove(it.layer)
			m.focus = areaList
		}
		return m, nil
	case "enter":
		_, _, fields, _ := m.detail()
		if err := m.set(fields[m.field].id, strings.TrimSpace(m.input.Value())); err != nil {
			m.editErr = err.Error()
			return m, nil
		}
		m.editing, m.editErr = false, ""
		m.input.Blur()
		m.dirty = true
		m.say("", false)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// set validates and stores the value typed into a text field.
func (m *setupUI) set(id, v string) error {
	l := m.current().layer
	switch id {
	case "name":
		var others []string
		for _, o := range m.dr.layers {
			if o != l {
				others = append(others, o.name)
			}
		}
		if err := validLayerName(others)(v); err != nil {
			return err
		}
		m.dr.renameNew(l, v)
	case "loc":
		if err := validLocation(v); err != nil {
			return err
		}
		l.loc = v
		if !isRemoteURL(v) || l.access == accessKeep && v != l.orig {
			l.access = accessNone
		}
		if l.loc == l.orig && l.auth != "" {
			l.access = accessKeep
		}
	case "author":
		if v == "" {
			return errors.New("required")
		}
		if err := manifest.CheckIdentityName(v); err != nil {
			return err
		}
		l.author = v
	case "email":
		if v == "" {
			return errors.New("required")
		}
		if err := manifest.CheckIdentityEmail(v); err != nil {
			return err
		}
		l.email = v
	case "remote":
		if v == "" {
			return errors.New("required")
		}
		l.remote = v
	case "roots":
		if len(splitRoots(v)) == 0 {
			return errors.New("give at least one directory")
		}
		m.dr.roots = strings.Join(splitRoots(v), ", ")
	}
	return nil
}

// cycle steps a choice field forwards or backwards.
func (m *setupUI) cycle(id string, dir int) {
	step := func(values []string, cur string) string {
		i := slices.Index(values, cur)
		return values[(max(i, 0)+dir+len(values))%len(values)]
	}
	switch {
	case id == "access":
		l := m.current().layer
		l.access = step(accessValues(l), l.access)
	case id == "identity":
		m.dr.identity = step(append([]string{""}, m.dr.names()[1:]...), m.dr.identity)
	case id == "autopull":
		m.dr.autoPull = !m.dr.autoPull
	case id == "profiles":
		m.dr.profiles = !m.dr.profiles
	case id == "lang":
		m.dr.lang = step(append([]string{""}, i18n.Languages...), m.dr.lang)
		i18n.Set(i18n.Detect(m.w.e.getenv, m.dr.lang))
	case strings.HasPrefix(id, "trust:"):
		n := strings.TrimPrefix(id, "trust:")
		if i := slices.Index(m.dr.trust, n); i >= 0 {
			m.dr.trust = slices.Delete(m.dr.trust, i, i+1)
		} else {
			m.dr.trust = append(m.dr.trust, n)
		}
	default:
		return
	}
	m.dirty = true
	m.say("", false)
}

func (m *setupUI) press(id string) {
	if id == "delete" {
		m.dr.remove(m.current().layer)
		m.dirty = true
		m.focus = areaList
		m.item = min(m.item, len(m.items())-1)
	}
}
