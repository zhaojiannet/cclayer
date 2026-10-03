package cli

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// The setup screen. The left column lists what the device is made of: its
// layers, then the settings that belong to the device itself. The right
// column explains the selected item, says what saving will do with it and
// holds its fields. The bottom line says what Save still needs, next to the
// buttons. Everything edits the draft; the disk is written after Save.

type area int

const (
	areaList area = iota
	areaDetail
	areaButtons
)

type fieldKind int

const (
	fieldText   fieldKind = iota // Enter edits it in place
	fieldChoice                  // Left and Right step through the values
	fieldInfo                    // read from the layer, not editable
	fieldButton                  // Enter acts
)

type uiItem struct {
	key     string // "layer", "add", "roots", "identity", "autopull", "profiles", "trust", "lang"
	layer   *layerDraft
	label   string
	value   string
	section int // 0 layers, 1 this device
}

type uiField struct {
	id    string
	kind  fieldKind
	label string
	value string
	hint  string
}

type setupUI struct {
	w        *wizard
	d        *manifest.Device
	dr       *draft
	existing bool

	width, height int
	focus         area
	item, field   int
	button        int

	editing bool
	input   textinput.Model
	editErr string

	msg       string // the last outcome, shown in the status line
	msgIsErr  bool
	help      bool
	quitArmed bool
	dirty     bool

	saved, applyNow bool
}

// screen runs the setup screen and returns the draft to save, or nil when
// the user quit, and whether to apply after saving.
func (w *wizard) screen(d *manifest.Device, existing bool) (*draft, bool, error) {
	m := newSetupUI(w, d, existing)
	final, err := tea.NewProgram(m, tea.WithInput(w.e.Stdin), tea.WithOutput(w.e.Stdout)).Run()
	if err != nil {
		return nil, false, err
	}
	m = final.(*setupUI)
	if !m.saved {
		return nil, false, nil
	}
	return m.dr, m.applyNow, nil
}

func newSetupUI(w *wizard, d *manifest.Device, existing bool) *setupUI {
	m := &setupUI{w: w, d: d, dr: newDraft(w, d), existing: existing, width: 100, height: 30}
	m.input = textinput.New()
	m.input.Prompt = ""
	m.jumpToNext()
	return m
}

func (m *setupUI) Init() tea.Cmd { return nil }

// items are the rows of the left column.
func (m *setupUI) items() []uiItem {
	var out []uiItem
	for _, l := range m.dr.layers {
		label := l.name
		if l.name == "base" {
			label = i18n.T("Base layer")
		} else if l.name == "" {
			label = i18n.T("(new overlay)")
		}
		value := l.loc
		if value == "" {
			value = i18n.T("not set")
		}
		out = append(out, uiItem{key: "layer", layer: l, label: label, value: value})
	}
	out = append(out, uiItem{key: "add", label: i18n.T("+ Add an overlay")})
	out = append(out, uiItem{key: "roots", label: i18n.T("Project dirs"), value: m.dr.roots, section: 1})
	if len(m.dr.layers) > 1 {
		id := m.dr.identity
		if id == "" {
			id = i18n.T("none")
		}
		out = append(out, uiItem{key: "identity", label: i18n.T("Default identity"), value: id, section: 1})
	}
	trust := strings.Join(m.dr.trust, ", ")
	if trust == "" {
		trust = i18n.T("none")
	}
	out = append(out,
		uiItem{key: "autopull", label: i18n.T("Auto pull"), value: onOff(m.dr.autoPull), section: 1},
		uiItem{key: "profiles", label: i18n.T("Profiles"), value: onOff(m.dr.profiles), section: 1},
		uiItem{key: "trust", label: i18n.T("Trusted layers"), value: trust, section: 1},
		uiItem{key: "lang", label: i18n.T("Language"), value: languageShort(m.dr.lang), section: 1},
	)
	return out
}

func (m *setupUI) current() uiItem {
	items := m.items()
	m.item = min(max(m.item, 0), len(items)-1)
	return items[m.item]
}

// languageShort is the language as the list shows it.
func languageShort(lang string) string {
	if lang == "" {
		return fmt.Sprintf(i18n.T("%s (system)"), nativeNames[i18n.Lang()])
	}
	return nativeNames[lang]
}

// accessName is how a credential method reads in a field.
func accessName(l *layerDraft, v string) string {
	switch v {
	case accessKeep:
		if l.auth == accessToken {
			return i18n.T("keep the token")
		}
		return i18n.T("keep the deploy key")
	case accessKey:
		return i18n.T("deploy key")
	case accessKey443:
		return i18n.T("deploy key over port 443")
	case accessToken:
		return i18n.T("HTTPS token")
	}
	return i18n.T("nothing")
}

func accessHint(v string) string {
	switch v {
	case accessKeep:
		return i18n.T("The credential set up on an earlier run stays.")
	case accessKey:
		return i18n.T("Deploy key: an SSH key for this one repository, created here and attached with gh")
	case accessKey443:
		return i18n.T("Deploy key over port 443, for networks that block SSH's port 22")
	case accessToken:
		return i18n.T("HTTPS token: you create a fine-grained token on GitHub, stored in the credential helper")
	}
	return i18n.T("Nothing: git already has access (public repository, or credentials you manage)")
}

func accessValues(l *layerDraft) []string {
	var v []string
	if l.auth != "" && l.loc == l.orig {
		v = append(v, accessKeep)
	}
	return append(v, accessNone, accessKey, accessKey443, accessToken)
}

// isNewOverlayDir reports whether saving writes a starter layer.toml for l,
// which then needs the identity the starter carries.
func isNewOverlayDir(l *layerDraft) bool {
	return l.name != "base" && l.loc != "" && l.loc != l.orig && isNewLocation(l.loc)
}

// layerInfo reads identity and match rules from the layer.toml the device
// can already see, or returns nil.
func (m *setupUI) layerInfo(l *layerDraft) *manifest.Layer {
	dir := ""
	switch {
	case l.loc == "":
	case l.loc == l.orig && m.d.Clone[l.name] != "":
		dir = m.d.ClonePath(l.name)
	default:
		if k, err := classifyLocation(l.loc); err == nil && k == locLayerDir {
			dir = manifest.ExpandHome(localPath(l.loc))
		}
	}
	if dir == "" {
		return nil
	}
	lt, err := manifest.LoadLayer(dir)
	if err != nil {
		return nil
	}
	return lt
}

// detail is the right column for the selected item: what it is, its
// fields, and what saving will do.
func (m *setupUI) detail() (title, desc string, fields []uiField, note string) {
	it := m.current()
	switch it.key {
	case "layer":
		l := it.layer
		base := l.name == "base"
		if base {
			title = i18n.T("Base layer")
			desc = i18n.T("The layer every machine shares: CLAUDE.md, rules, skills, agents and the shared keys of settings.json. Keep it in a private repository, or in a folder your cloud drive syncs.")
		} else {
			title = fmt.Sprintf(i18n.T("Overlay %s"), cmpOr(l.name, "…"))
			desc = i18n.T("A private layer for one team. It applies only in the repositories its match rule names, with the team's git identity and project files.")
		}
		if !base && !l.saved {
			fields = append(fields, uiField{id: "name", kind: fieldText, label: i18n.T("Name"), value: l.name, hint: i18n.T("lower-case letters, digits, hyphens; the team's short name")})
		}
		locHint := i18n.T("A repository to clone, or a directory on this machine. A path that does not exist yet gets a starter layer.toml.")
		if base {
			locHint = i18n.T("A repository to clone, or a directory on this machine (a folder your cloud drive syncs works too). A path that does not exist yet gets a starter layer.toml.")
		}
		fields = append(fields, uiField{id: "loc", kind: fieldText, label: i18n.T("Address"), value: l.loc, hint: locHint})
		if isRemoteURL(l.loc) {
			fields = append(fields, uiField{id: "access", kind: fieldChoice, label: i18n.T("Access"), value: accessName(l, l.access), hint: accessHint(l.access)})
		}
		switch {
		case isNewOverlayDir(l):
			starter := i18n.T("Goes into the starter layer.toml of this overlay.")
			fields = append(fields,
				uiField{id: "author", kind: fieldText, label: i18n.T("Author"), value: l.author, hint: starter},
				uiField{id: "email", kind: fieldText, label: i18n.T("Email"), value: l.email, hint: starter},
				uiField{id: "remote", kind: fieldText, label: i18n.T("Match"), value: l.remote, hint: i18n.T("host/owner/*, for example github.com/acme-inc/*")},
			)
		case !base:
			if lt := m.layerInfo(l); lt != nil {
				if id := lt.Identity; id != nil {
					fields = append(fields, uiField{id: "info", kind: fieldInfo, label: i18n.T("Identity"), value: id.Name + " <" + id.Email + ">"})
				}
				var match []string
				for _, p := range lt.Match {
					match = append(match, p.Remote)
				}
				fields = append(fields, uiField{id: "info", kind: fieldInfo, label: i18n.T("Match"), value: strings.Join(match, ", ")})
			}
		}
		if !base && !l.saved {
			fields = append(fields, uiField{id: "delete", kind: fieldButton, label: i18n.T("Remove this overlay")})
		}
		note = m.layerNote(l)
	case "add":
		title = i18n.T("Add an overlay")
		desc = i18n.T("Add a private layer for a team you work with. Press Enter, then give it a name and an address.")
	case "roots":
		title = i18n.T("Project dirs")
		desc = i18n.T("cclayer looks for git repositories in these directories and gives each one the overlay that matches it.")
		fields = []uiField{{id: "roots", kind: fieldText, label: i18n.T("Directories"), value: m.dr.roots, hint: i18n.T("comma separated; cclayer scans them for repositories")}}
	case "identity":
		title = i18n.T("Default identity")
		desc = i18n.T("The git identity in repositories no overlay matches. None makes git refuse to commit there, so nothing goes out under the wrong name.")
		v := m.dr.identity
		if v == "" {
			v = i18n.T("none")
		}
		fields = []uiField{{id: "identity", kind: fieldChoice, label: i18n.T("Identity"), value: v}}
	case "autopull":
		title = i18n.T("Auto pull")
		desc = i18n.T("Pull the layers and apply them each time a Claude Code session starts. Off means you run cclayer apply yourself.")
		fields = []uiField{{id: "autopull", kind: fieldChoice, label: i18n.T("Auto pull"), value: onOff(m.dr.autoPull)}}
	case "profiles":
		title = i18n.T("Profiles")
		desc = i18n.T("One Claude Code config directory per overlay: separate logins, sessions and prompt history. A project picks its own with cclayer env or cclayer run.")
		fields = []uiField{{id: "profiles", kind: fieldChoice, label: i18n.T("Profiles"), value: onOff(m.dr.profiles)}}
	case "trust":
		title = i18n.T("Trusted layers")
		desc = i18n.T("Layers allowed to set git keys that make git run programs: hooks, credential helpers, shell aliases. Trust only layers you control.")
		for _, n := range m.dr.names() {
			if n == "" {
				continue
			}
			fields = append(fields, uiField{id: "trust:" + n, kind: fieldChoice, label: n, value: onOff(slices.Contains(m.dr.trust, n))})
		}
	case "lang":
		title = i18n.T("Language")
		desc = i18n.T("The language of cclayer's messages. Errors stay in English.")
		fields = []uiField{{id: "lang", kind: fieldChoice, label: i18n.T("Language"), value: languageName(m.dr.lang)}}
	}
	return title, desc, fields, note
}

// layerNote says what saving does with a layer's address.
func (m *setupUI) layerNote(l *layerDraft) string {
	note := ""
	if k, err := classifyLocation(l.loc); err == nil && l.loc != "" {
		switch {
		case l.loc == l.orig && (k == locURL || k == locGitDir):
			note = fmt.Sprintf(i18n.T("Cloned at %s."), m.w.e.tilde(m.d.ClonePath(l.name)))
		case k == locURL || k == locGitDir:
			note = fmt.Sprintf(i18n.T("Saving clones it to %s."), "~/.local/share/cclayer/"+cmpOr(l.name, "…"))
		case k == locNew:
			note = fmt.Sprintf(i18n.T("Saving writes a starter layer.toml to %s."), l.loc)
		default:
			note = i18n.T("Used in place: cclayer reads and writes this directory.")
		}
	}
	if l.name != "base" && l.saved {
		// leave also purges what apply put into the overlay's projects
		note = strings.TrimSpace(note + " " + fmt.Sprintf(i18n.T("To drop this overlay, run cclayer leave %s after setup."), l.name))
	}
	return note
}
