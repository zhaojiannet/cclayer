package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zhaojiannet/cclayer/internal/i18n"
)

// The 16 basic colors follow the terminal's own theme, light or dark.
var (
	accent    = lipgloss.Color("6")
	warn      = lipgloss.Color("1")
	styleHead = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styleDim  = lipgloss.NewStyle().Faint(true)
	styleErr  = lipgloss.NewStyle().Foreground(warn)
	styleSel  = lipgloss.NewStyle().Reverse(true)
	styleBold = lipgloss.NewStyle().Bold(true)
)

func (m *setupUI) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *setupUI) render() string {
	w := max(m.width, 60)
	var out []string
	out = append(out, m.headerLine(w))

	if w >= 100 {
		lw := min(max(w*42/100, 40), 56)
		lcol := m.leftBoxes(lw)
		rcol := m.detailBox(w-lw, lipgloss.Height(lcol))
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top, lcol, rcol))
	} else {
		out = append(out, m.leftBoxes(w), m.detailBox(w, 0))
	}
	out = append(out, m.statusLine(w), m.keysLine(w))
	return strings.Join(out, "\n")
}

func (m *setupUI) headerLine(w int) string {
	title := styleHead.Render("cclayer " + Version + " · " + i18n.T("Set up this device"))
	state := i18n.T("new")
	if m.existing {
		state = i18n.T("editing")
	}
	if m.dirty {
		state += i18n.T(", unsaved changes")
	}
	path := styleDim.Render(fmt.Sprintf(i18n.T("%s (%s)"), m.w.e.tilde(m.w.e.DevicePath), state))
	gap := w - lipgloss.Width(title) - lipgloss.Width(path) - 2
	if gap < 1 {
		return " " + title
	}
	return " " + title + strings.Repeat(" ", gap) + path
}

// leftBoxes are the layers box and the device box, stacked.
func (m *setupUI) leftBoxes(w int) string {
	items := m.items()
	labelW := 0
	for _, it := range items {
		if it.key != "add" {
			labelW = max(labelW, lipgloss.Width(it.label))
		}
	}
	var sections [2][]string
	for i, it := range items {
		text := it.label
		if it.key != "add" {
			text = pad(it.label, labelW) + "  " + it.value
		}
		line := "  " + text
		selected := i == m.item
		if selected {
			line = "▸ " + text
		}
		line = pad(trunc(line, w-4), w-4)
		switch {
		case selected && m.focus == areaList:
			line = styleSel.Render(line)
		case selected:
			line = styleBold.Render(line)
		case it.key == "layer" && it.layer.loc == "" || it.key == "roots" && it.value == "":
			line = styleErr.Render(line)
		case it.key == "add":
			line = styleDim.Render(line)
		}
		sections[it.section] = append(sections[it.section], line)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		box(i18n.T("Layers"), sections[0], w, m.focus == areaList && m.current().section == 0),
		box(i18n.T("This device"), sections[1], w, m.focus == areaList && m.current().section == 1),
	)
}

func (m *setupUI) detailBox(w, height int) string {
	inner := w - 4
	if m.help {
		return box(i18n.T("Keys"), padLines(m.helpLines(inner), height-2), w, true)
	}
	title, desc, fields, note := m.detail()
	lines := wrap(desc, inner)
	if len(fields) > 0 {
		lines = append(lines, "")
	}
	labelW := 0
	for _, f := range fields {
		if f.kind != fieldButton {
			labelW = max(labelW, lipgloss.Width(f.label))
		}
	}
	hint := ""
	for i, f := range fields {
		focused := m.focus == areaDetail && i == m.field
		var line string
		switch f.kind {
		case fieldButton:
			lines = append(lines, "")
			line = "[ " + f.label + " ]"
		case fieldChoice:
			value := f.value
			if focused {
				value = "‹ " + value + " ›"
			}
			line = pad(f.label, labelW) + "  " + value
		default:
			value := f.value
			if value == "" && f.kind == fieldText {
				value = i18n.T("not set")
			}
			line = pad(f.label, labelW) + "  " + value
		}
		if focused && m.editing {
			m.input.SetWidth(max(inner-labelW-3, 10))
			lines = append(lines, pad(f.label, labelW)+"  "+m.input.View())
			if m.editErr != "" {
				lines = append(lines, styleErr.Render(pad("", labelW)+"  "+m.editErr))
			}
			hint = f.hint
			continue
		}
		line = pad(trunc(line, inner), inner)
		switch {
		case focused:
			line = styleSel.Render(line)
			hint = f.hint
		case f.kind == fieldInfo:
			line = styleDim.Render(line)
		case f.kind == fieldText && f.value == "":
			line = styleErr.Render(line)
		}
		lines = append(lines, line)
	}
	if hint != "" {
		lines = append(lines, "")
		for _, l := range wrap(hint, inner) {
			lines = append(lines, styleDim.Render(l))
		}
	}
	if note != "" {
		lines = append(lines, "")
		lines = append(lines, wrap(note, inner)...)
	}
	return box(title, padLines(lines, height-2), w, m.focus == areaDetail)
}

func (m *setupUI) statusLine(w int) string {
	var bs []string
	for i, b := range buttons {
		label := map[string]string{"apply": i18n.T("Save and apply"), "save": i18n.T("Save only"), "quit": i18n.T("Quit")}[b]
		label = "[ " + label + " ]"
		if m.focus == areaButtons && i == m.button {
			label = styleSel.Render(label)
		}
		bs = append(bs, label)
	}
	right := strings.Join(bs, "  ")
	left := m.msg
	style := styleDim
	if m.msgIsErr {
		style = styleErr
	}
	if left == "" {
		if g := m.gapLine(); g != "" {
			left, style = g, styleErr
		} else {
			left = i18n.T("Ready to save.")
		}
	}
	room := w - lipgloss.Width(right) - 3
	left = trunc(left, max(room, 0))
	return " " + style.Render(left) + strings.Repeat(" ", max(room-lipgloss.Width(left), 1)) + right
}

func (m *setupUI) keysLine(w int) string {
	var keys string
	switch {
	case m.editing:
		keys = i18n.T("Enter confirm  Esc cancel")
	case m.focus == areaButtons:
		keys = i18n.T("←→ choose  Enter press  ↑ back to the list  q quit")
	case m.focus == areaDetail:
		keys = i18n.T("↑↓ choose  Enter edit  ←→ change  Esc back  ? keys")
	default:
		keys = i18n.T("↑↓ choose  Enter open  Tab next area  ? keys  q quit")
	}
	return " " + styleDim.Render(trunc(keys, w-2))
}

func (m *setupUI) helpLines(inner int) []string {
	rows := [][2]string{
		{"↑ ↓", i18n.T("move within a box")},
		{"Enter", i18n.T("open the item, edit a field, change a choice, press a button")},
		{"← →", i18n.T("change a choice; ← also goes back to the list")},
		{"Space", i18n.T("switch auto pull and profiles on or off from the list")},
		{"Tab", i18n.T("move between the list, the details and the buttons")},
		{"Esc", i18n.T("cancel an edit, or go back to the list")},
		{"q", i18n.T("quit without saving (asks again when something changed)")},
		{"?", i18n.T("show or hide these keys")},
	}
	var out []string
	for _, r := range rows {
		out = append(out, trunc(pad(r[0], 7)+r[1], inner))
	}
	return append(out, "", styleDim.Render(trunc(i18n.T("Press any key to close."), inner)))
}

// box draws a rounded frame with its title in the top edge; the frame is
// in the accent color while the box has the focus.
func box(title string, lines []string, w int, focused bool) string {
	inner := w - 4
	frame := styleDim
	if focused {
		frame = lipgloss.NewStyle().Foreground(accent)
	}
	title = trunc(title, max(inner-2, 1))
	top := frame.Render("╭─ ") + styleBold.Render(title) + frame.Render(" "+strings.Repeat("─", max(w-5-lipgloss.Width(title), 0))+"╮")
	out := []string{top}
	for _, l := range lines {
		out = append(out, frame.Render("│ ")+pad(l, inner)+frame.Render(" │"))
	}
	out = append(out, frame.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

func padLines(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// pad and trunc measure display cells, so CJK text lines up.
func pad(s string, n int) string {
	if d := n - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func trunc(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > n-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// wrap breaks text into lines of at most n cells. Latin words stay whole;
// CJK text breaks between any two characters, as it is written without
// spaces.
// CUSTOM: lipgloss wraps at spaces only, which leaves a CJK sentence after
// a Latin word as one unbreakable run.
func wrap(s string, n int) []string {
	if s == "" {
		return nil
	}
	var tokens []string
	word := ""
	flush := func() {
		if word != "" {
			tokens = append(tokens, word)
			word = ""
		}
	}
	for _, r := range s {
		switch {
		case r == ' ':
			flush()
			tokens = append(tokens, " ")
		case lipgloss.Width(string(r)) > 1:
			flush()
			tokens = append(tokens, string(r))
		default:
			word += string(r)
		}
	}
	flush()
	var lines []string
	line := ""
	for _, t := range tokens {
		if lipgloss.Width(line)+lipgloss.Width(t) > n {
			lines = append(lines, strings.TrimRight(line, " "))
			line = ""
			if t == " " {
				continue
			}
			for lipgloss.Width(t) > n {
				lines = append(lines, trunc(t, n))
				t = ""
			}
		}
		line += t
	}
	if line = strings.TrimRight(line, " "); line != "" {
		lines = append(lines, line)
	}
	return lines
}
