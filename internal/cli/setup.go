package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/keys"
	"github.com/zhaojiannet/cclayer/internal/manifest"
)

// runSetup shows every setting of the device on one screen and writes
// nothing until Save: the full-screen editor on a terminal, an overview of
// line prompts otherwise. Credentials, cloning, apply and doctor follow the
// save.
func runSetup(e *Env, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	accessible := fs.Bool("accessible", false, "plain prompts instead of the full-screen form (also used when stdin is not a terminal)")
	if err := fs.Parse(args); err != nil {
		return usagef("setup: %v", err)
	}
	if !e.Interactive {
		return fmt.Errorf("setup needs a terminal; use cclayer init with a prepared device manifest instead")
	}
	w := &wizard{e: e, accessible: *accessible || !stdinIsTerminal(e)}

	d, err := manifest.LoadDevice(e.DevicePath)
	existing := err == nil
	switch {
	case existing:
	case os.IsNotExist(underlying(err)):
		d = &manifest.Device{}
	default:
		// an existing but invalid manifest is fixed by hand, never overwritten
		return err
	}
	d.EnsureMaps()

	var dr *draft
	applyNow := false
	if w.accessible {
		dr, err = w.overview(d, existing)
	} else {
		dr, applyNow, err = w.screen(d, existing)
	}
	if err != nil || dr == nil {
		if err == nil {
			e.printf("nothing saved\n")
		}
		return err
	}
	if err := w.commit(d, dr); err != nil {
		return err
	}
	if err := d.Validate(); err != nil {
		return err
	}
	if err := d.Save(e.DevicePath); err != nil {
		return err
	}
	e.printf("wrote %s\n", e.tilde(e.DevicePath))

	if err := w.credentials(d, dr); err != nil {
		return err
	}
	if err := cloneLayers(e, d); err != nil {
		return err
	}
	loaded, err := e.Load()
	if err != nil {
		return err
	}
	if added := fillBlocklist(loaded); len(added) > 0 {
		e.printf("blocklist += %s\n", strings.Join(added, ", "))
	}
	d.Blocklist = loaded.Device.Blocklist
	if err := d.Save(e.DevicePath); err != nil {
		return err
	}

	// doctor only reports, so it follows apply without a question of its own
	if w.accessible {
		applyNow = true
		if err := w.form(huh.NewConfirm().Title(i18n.T("Apply the layers to this device now?")).Value(&applyNow)); err != nil {
			return err
		}
	}
	if !applyNow {
		e.printf("run cclayer apply when you are ready\n")
		return nil
	}
	if err := apply(e, applyOpts{}); err != nil {
		return err
	}
	return runDoctor(e, nil)
}

type wizard struct {
	e          *Env
	accessible bool
	in         io.Reader
	ended      *bool
}

// lineReader hands out one line per Read call. huh's accessible prompts
// each wrap the input in their own buffered reader; with a plain pipe the
// first prompt would swallow every following answer.
type lineReader struct {
	r     io.Reader
	ended bool
}

func (l *lineReader) Read(p []byte) (int, error) {
	n := 0
	var one [1]byte
	for n < len(p) {
		k, err := l.r.Read(one[:])
		if k == 1 {
			p[n] = one[0]
			n++
			if one[0] == '\n' {
				return n, nil
			}
		}
		if err != nil {
			if n == 0 && errors.Is(err, io.EOF) {
				l.ended = true
			}
			return n, err
		}
	}
	return n, nil
}

// ttyInput is a terminal for accessible prompts: it keeps Fd, which huh's
// password prompt needs, and notices Ctrl-D.
type ttyInput struct {
	*os.File
	ended bool
}

func (t *ttyInput) Read(p []byte) (int, error) {
	n, err := t.File.Read(p)
	if n == 0 && errors.Is(err, io.EOF) {
		t.ended = true
	}
	return n, err
}

// header names the program on the overview, with the version so a
// screenshot in an issue says which build it came from.
func header() string {
	return "cclayer " + Version + " · " + i18n.T("setup")
}

// errBack is what a form returns when Esc leaves it without its changes.
var errBack = errors.New("back")

// input wires the Env's stdin once. Accessible prompts answer every
// question with its default once input has ended, so the end is recorded
// and turned into an error instead of an endless overview loop.
func (w *wizard) input() io.Reader {
	if w.in == nil {
		switch f, isFile := w.e.Stdin.(*os.File); {
		case !w.accessible:
			w.in = w.e.Stdin
			w.ended = new(bool)
		case stdinIsTerminal(w.e) && isFile:
			t := &ttyInput{File: f}
			w.in, w.ended = t, &t.ended
		default:
			l := &lineReader{r: w.e.Stdin}
			w.in, w.ended = l, &l.ended
		}
	}
	return w.in
}

// page is one group of a form; hide, when set, is asked after the pages
// before it are answered.
type page struct {
	fields []huh.Field
	hide   func() bool
}

// run runs pages as one form, so Shift+Tab walks back between them. With
// back set, Esc leaves the form and errBack discards its answers.
func (w *wizard) run(back bool, pages ...page) error {
	in := w.input()
	if w.accessible {
		// huh's accessible runner asks every field of every group and
		// ignores WithHideFunc, so each page runs on its own
		for _, p := range pages {
			if p.hide != nil && p.hide() {
				continue
			}
			if err := w.runForm(huh.NewForm(huh.NewGroup(p.fields...)), in, back); err != nil {
				return err
			}
		}
		return nil
	}
	var groups []*huh.Group
	for _, p := range pages {
		g := huh.NewGroup(p.fields...)
		if p.hide != nil {
			g = g.WithHideFunc(p.hide)
		}
		groups = append(groups, g)
	}
	return w.runForm(huh.NewForm(groups...), in, back)
}

func (w *wizard) runForm(f *huh.Form, in io.Reader, back bool) error {
	f = f.WithInput(in).WithOutput(w.e.Stdout).WithAccessible(w.accessible)
	if back {
		km := huh.NewDefaultKeyMap()
		km.Quit.SetKeys("esc", "ctrl+c")
		f = f.WithKeyMap(km)
	}
	err := f.Run()
	switch {
	case *w.ended:
		return fmt.Errorf("setup cancelled: input ended")
	case errors.Is(err, huh.ErrUserAborted) && back:
		return errBack
	case errors.Is(err, huh.ErrUserAborted):
		return fmt.Errorf("setup cancelled")
	}
	return err
}

// form runs fields as one group that cannot be left with Esc.
func (w *wizard) form(fields ...huh.Field) error {
	return w.run(false, page{fields: fields})
}

const (
	accessKeep   = "keep"
	accessNone   = "none"
	accessKey    = "deploy-key"
	accessKey443 = "deploy-key-443"
	accessToken  = "token"
)

// layerDraft is one layer as the overview shows it, before anything is
// written.
type layerDraft struct {
	id   int
	name string
	loc  string
	// orig and auth are the location and credential method in the saved
	// manifest; empty for a layer added in this run
	orig, auth string
	// saved is true for a layer the manifest already lists. A saved
	// overlay leaves only through `cclayer leave`, which also cleans up
	// what apply put into its projects.
	saved  bool
	access string
	// the starter layer.toml of a new overlay directory
	author, email, remote string
}

type draft struct {
	layers   []*layerDraft
	roots    string
	identity string
	autoPull bool
	profiles bool
	trust    []string
	lang     string
	cloneDir string
}

func newDraft(w *wizard, d *manifest.Device) *draft {
	dr := &draft{
		roots:    strings.Join(d.Roots, ", "),
		identity: d.DefaultIdentity,
		autoPull: d.AutoPull,
		profiles: d.Profiles,
		trust:    slices.Clone(d.TrustExec),
		lang:     d.Lang,
		cloneDir: d.CloneRoot(),
	}
	if dr.roots == "" {
		dr.roots = "~/Projects"
	}
	names := d.Layers
	if len(names) == 0 {
		names = []string{"base"}
	}
	for i, n := range names {
		loc := w.location(d, n)
		l := &layerDraft{id: i, name: n, loc: loc, orig: loc, auth: d.Auth[n], access: accessNone, saved: len(d.Layers) > 0}
		if l.auth != "" {
			l.access = accessKeep
		}
		dr.layers = append(dr.layers, l)
	}
	return dr
}

// newOverlay is a blank overlay with an id no other layer of the draft
// has; it joins the draft when it is appended.
func (dr *draft) newOverlay() *layerDraft {
	id := 0
	for _, l := range dr.layers {
		id = max(id, l.id+1)
	}
	return &layerDraft{id: id, access: accessNone}
}

func (dr *draft) names() []string {
	var out []string
	for _, l := range dr.layers {
		out = append(out, l.name)
	}
	return out
}

func (dr *draft) find(name string) *layerDraft {
	for _, l := range dr.layers {
		if l.name == name {
			return l
		}
	}
	return nil
}

// remove drops an overlay added in this run, with what referred to it.
func (dr *draft) remove(gone *layerDraft) {
	dr.layers = slices.DeleteFunc(dr.layers, func(l *layerDraft) bool { return l == gone })
	if gone.name == "" {
		return
	}
	dr.trust = slices.DeleteFunc(dr.trust, func(t string) bool { return t == gone.name })
	if dr.identity == gone.name {
		dr.identity = ""
	}
}

// gap is one thing Save still needs: which layer (nil for the device
// settings), which field, and how to say it.
type gap struct {
	layer *layerDraft
	field string
	what  string
}

// gaps lists what Save still needs, in screen order.
func (dr *draft) gaps() []gap {
	var out []gap
	for _, l := range dr.layers {
		switch {
		case l.name == "base" && l.loc == "":
			out = append(out, gap{l, "loc", i18n.T("base layer address")})
		case l.name == "":
			out = append(out, gap{l, "name", i18n.T("name of the new overlay")})
		case l.loc == "":
			out = append(out, gap{l, "loc", fmt.Sprintf(i18n.T("address of overlay %s"), l.name)})
		case l.name != "base" && l.loc != l.orig && isNewLocation(l.loc) && (l.author == "" || l.email == "" || l.remote == ""):
			out = append(out, gap{l, "author", fmt.Sprintf(i18n.T("identity of overlay %s"), l.name)})
		}
	}
	if len(splitRoots(dr.roots)) == 0 {
		out = append(out, gap{nil, "roots", i18n.T("project dirs")})
	}
	return out
}

// problem is why the draft cannot be saved yet, or "".
func (dr *draft) problem() string {
	g := dr.gaps()
	if len(g) == 0 {
		return ""
	}
	return fmt.Sprintf(i18n.T("Still needed: %s"), g[0].what)
}

func splitRoots(s string) []string {
	var out []string
	for _, r := range strings.Split(s, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func onOff(b bool) string {
	if b {
		return i18n.T("on")
	}
	return i18n.T("off")
}

func isNewLocation(loc string) bool {
	k, err := classifyLocation(loc)
	return err == nil && k == locNew
}

// summary is a layer's value on the overview.
func (l *layerDraft) summary() string {
	if l.loc == "" {
		return i18n.T("not set")
	}
	s := l.loc
	if l.loc != l.orig && isNewLocation(l.loc) {
		s += i18n.T(" (new; a starter layer.toml is written)")
	}
	method := l.access
	if method == accessKeep {
		method = l.auth
	}
	switch method {
	case accessKey:
		s += i18n.T(", deploy key")
	case accessKey443:
		s += i18n.T(", deploy key over port 443")
	case accessToken:
		s += i18n.T(", token")
	}
	return s
}

func (dr *draft) rows() []huh.Option[string] {
	type line struct{ label, value, key string }
	var lines []line
	for _, l := range dr.layers {
		label := i18n.T("Base layer")
		if l.name != "base" {
			label = fmt.Sprintf(i18n.T("Overlay %s"), l.name)
		}
		lines = append(lines, line{label, l.summary(), "layer:" + l.name})
	}
	lines = append(lines, line{i18n.T("Add an overlay"), i18n.T("a private layer for one team"), "add"})
	lines = append(lines, line{i18n.T("Project dirs"), dr.roots, "roots"})
	lines = append(lines, line{i18n.T("Local clones"), dr.cloneDir, "clonedir"})
	if len(dr.layers) > 1 {
		id := dr.identity
		if id == "" {
			id = i18n.T("none (git refuses to commit there)")
		}
		lines = append(lines, line{i18n.T("Default identity"), id, "identity"})
	}
	trust := strings.Join(dr.trust, ", ")
	if trust == "" {
		trust = i18n.T("none")
	}
	lines = append(lines,
		line{i18n.T("Auto pull"), fmt.Sprintf(i18n.T("%s (update when Claude Code starts)"), onOff(dr.autoPull)), "autopull"},
		line{i18n.T("Profiles"), fmt.Sprintf(i18n.T("%s (separate login per overlay)"), onOff(dr.profiles)), "profiles"},
		line{i18n.T("Trusted layers"), fmt.Sprintf(i18n.T("%s (may set git hooks and helpers)"), trust), "trust"},
		line{i18n.T("Language"), languageName(dr.lang), "lang"},
	)
	// labels are padded by display width: a CJK character takes two cells
	width := 0
	for _, l := range lines {
		width = max(width, lipgloss.Width(l.label))
	}
	var opts []huh.Option[string]
	for _, l := range lines {
		opts = append(opts, huh.NewOption(l.label+strings.Repeat(" ", width-lipgloss.Width(l.label)+2)+l.value, l.key))
	}
	return append(opts,
		huh.NewOption(i18n.T("Save and continue"), "save"),
		huh.NewOption(i18n.T("Quit without saving"), "quit"),
	)
}

// nativeNames are shown in their own language, so a wrong choice can be
// undone without reading the current one.
var nativeNames = map[string]string{"en": "English", "zh": "简体中文", "ja": "日本語"}

func languageName(lang string) string {
	if lang == "" {
		return fmt.Sprintf(i18n.T("follow the system (%s)"), nativeNames[i18n.Lang()])
	}
	return nativeNames[lang]
}

func (w *wizard) editLanguage(dr *draft) error {
	opts := []huh.Option[string]{huh.NewOption(fmt.Sprintf(i18n.T("Follow the system (%s)"), nativeNames[i18n.Detect(w.e.getenv, "")]), "")}
	for _, l := range i18n.Languages {
		opts = append(opts, huh.NewOption(nativeNames[l], l))
	}
	v := dr.lang
	if err := w.run(true, page{fields: []huh.Field{huh.NewSelect[string]().Title(i18n.T("Language of cclayer's messages")).Options(opts...).Value(&v)}}); err != nil {
		return err
	}
	dr.lang = v
	i18n.Set(i18n.Detect(w.e.getenv, v))
	return nil
}

// overview runs the settings page until Save or Quit. It returns nil for
// Quit.
func (w *wizard) overview(d *manifest.Device, existing bool) (*draft, error) {
	dr := newDraft(w, d)
	intro := func() string {
		s := i18n.T("Pick a row to change it, Esc to come back. Nothing is written until Save.")
		if existing {
			s = fmt.Sprintf(i18n.T("Editing %s."), w.e.tilde(w.e.DevicePath)) + " " + s
		}
		return s
	}
	cursor := dr.next()
	note := ""
	for {
		desc := intro()
		if note != "" {
			desc = note
		}
		opts := dr.rows()
		choice := cursor
		if err := w.form(
			huh.NewNote().Title(header()).Description(desc),
			huh.NewSelect[string]().Title(i18n.T("Settings")).Options(opts...).Value(&choice).Height(len(opts)+1),
		); err != nil {
			return nil, err
		}
		note = ""
		var err error
		switch {
		case choice == "save":
			if note = dr.problem(); note == "" {
				return dr, nil
			}
		case choice == "quit":
			return nil, nil
		case choice == "autopull":
			dr.autoPull = !dr.autoPull
		case choice == "profiles":
			dr.profiles = !dr.profiles
		case choice == "add":
			err = w.editLayer(dr, nil)
		case strings.HasPrefix(choice, "layer:"):
			err = w.pickLayer(dr, dr.find(strings.TrimPrefix(choice, "layer:")))
		case choice == "roots":
			err = w.editRoots(dr)
		case choice == "clonedir":
			err = w.editCloneDir(dr)
		case choice == "identity":
			err = w.editIdentity(dr)
		case choice == "trust":
			err = w.editTrust(dr)
		case choice == "lang":
			err = w.editLanguage(dr)
		}
		switch {
		case errors.Is(err, errBack):
			cursor = choice
		case err != nil:
			return nil, err
		case choice == "autopull" || choice == "profiles":
			// a toggle stays under the cursor so the new value is seen
			cursor = choice
		default:
			cursor = dr.next()
		}
	}
}

// next is where the cursor waits: the first thing Save still needs, else
// Save itself.
func (dr *draft) next() string {
	switch {
	case dr.layers[0].loc == "":
		return "layer:base"
	case len(splitRoots(dr.roots)) == 0:
		return "roots"
	}
	return "save"
}

// pickLayer edits a layer; an overlay added in this run can also be
// removed again. A saved overlay leaves with `cclayer leave`, which also
// cleans up what apply put on the device.
func (w *wizard) pickLayer(dr *draft, l *layerDraft) error {
	if l.name == "base" || l.saved {
		return w.editLayer(dr, l)
	}
	what := "edit"
	if err := w.run(true, page{fields: []huh.Field{huh.NewSelect[string]().Title(fmt.Sprintf(i18n.T("Overlay %s"), l.name)).
		Options(huh.NewOption(i18n.T("Change it"), "edit"), huh.NewOption(i18n.T("Remove it"), "remove")).Value(&what)}}); err != nil {
		return err
	}
	if what == "remove" {
		dr.remove(l)
		return nil
	}
	return w.editLayer(dr, l)
}

// accessOptions are the ways to reach a layer repository; keeping the saved
// credential is offered while the location is the saved one.
func accessOptions(l *layerDraft, loc string) []huh.Option[string] {
	var opts []huh.Option[string]
	if l.auth != "" && loc == l.orig {
		keep := i18n.T("Keep the deploy key this device already has")
		if l.auth == accessToken {
			keep = i18n.T("Keep the token this device already has")
		}
		opts = append(opts, huh.NewOption(keep, accessKeep))
	}
	return append(opts,
		huh.NewOption(i18n.T("Nothing: git already has access (public repository, or credentials you manage)"), accessNone),
		huh.NewOption(i18n.T("Deploy key: an SSH key for this one repository, created here and attached with gh"), accessKey),
		huh.NewOption(i18n.T("Deploy key over port 443, for networks that block SSH's port 22"), accessKey443),
		huh.NewOption(i18n.T("HTTPS token: you create a fine-grained token on GitHub, stored in the credential helper"), accessToken),
	)
}

// editLayer edits a layer, or adds an overlay when l is nil. The pages
// after the location appear only when they apply: credentials for a
// remote repository, the starter questions for a new overlay directory.
func (w *wizard) editLayer(dr *draft, l *layerDraft) error {
	adding := l == nil
	if adding {
		l = dr.newOverlay()
	}
	base := l.name == "base"
	name, loc, access := l.name, l.loc, l.access
	author, email, remote := l.author, l.email, l.remote

	var first []huh.Field
	if !base && !l.saved {
		others := slices.DeleteFunc(dr.names(), func(n string) bool { return n == l.name })
		first = append(first, huh.NewInput().Title(i18n.T("Overlay name")).
			Description(i18n.T("lower-case letters, digits, hyphens; the team's short name")).
			Value(&name).Validate(validLayerName(others)))
	}
	title, desc := i18n.T("Git URL or directory of the overlay"), i18n.T("A repository to clone, or a directory on this machine. A path that does not exist yet gets a starter layer.toml.")
	if base {
		title = i18n.T("Git URL or directory of the base layer")
		desc = i18n.T("A repository to clone, or a directory on this machine (a folder your cloud drive syncs works too). A path that does not exist yet gets a starter layer.toml.")
	} else if l.saved {
		desc += " " + fmt.Sprintf(i18n.T("To drop this overlay, run cclayer leave %s after setup."), l.name)
	}
	first = append(first, huh.NewInput().Title(title).Description(desc).
		Placeholder("https://github.com/you/cclayer-"+cmpOr(l.name, "acme")+".git").
		Value(&loc).Validate(validLocation))

	required := func(check func(string) error) func(string) error {
		return func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("required")
			}
			if check == nil {
				return nil
			}
			return check(s)
		}
	}
	err := w.run(true,
		page{fields: first},
		page{fields: []huh.Field{huh.NewSelect[string]().Title(i18n.T("How should this device reach the repository?")).
			OptionsFunc(func() []huh.Option[string] { return accessOptions(l, strings.TrimSpace(loc)) }, &loc).
			Value(&access)},
			hide: func() bool { return !isRemoteURL(loc) }},
		page{fields: []huh.Field{
			huh.NewNote().Title(i18n.T("New overlay")).DescriptionFunc(func() string {
				return fmt.Sprintf(i18n.T("Nothing at %s yet; these go into its starter layer.toml."), strings.TrimSpace(loc))
			}, &loc),
			huh.NewInput().Title(i18n.T("Commit author name")).Value(&author).Validate(required(manifest.CheckIdentityName)),
			huh.NewInput().Title(i18n.T("Commit email")).Value(&email).Validate(required(manifest.CheckIdentityEmail)),
			huh.NewInput().Title(i18n.T("Remote URL pattern of this team's repositories")).
				Description(i18n.T("host/owner/*, for example github.com/acme-inc/*")).Value(&remote).Validate(required(nil)),
		}, hide: func() bool { return base || !isNewLocation(loc) }},
	)
	if err != nil {
		return err
	}
	loc = strings.TrimSpace(loc)
	if !isRemoteURL(loc) || access == accessKeep && loc != l.orig {
		access = accessNone
	}
	l.loc, l.access = loc, access
	l.author, l.email, l.remote = strings.TrimSpace(author), strings.TrimSpace(email), strings.TrimSpace(remote)
	if adding {
		l.name = name
		dr.layers = append(dr.layers, l)
	} else if l.name != name {
		dr.renameNew(l, name)
	}
	return nil
}

// renameNew renames an overlay added in this run, with what refers to it.
func (dr *draft) renameNew(l *layerDraft, name string) {
	if l.name == "" {
		// nothing refers to an overlay before it has a name, and an empty
		// default identity means none, not this overlay
		l.name = name
		return
	}
	for i, t := range dr.trust {
		if t == l.name {
			dr.trust[i] = name
		}
	}
	if dr.identity == l.name {
		dr.identity = name
	}
	l.name = name
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (w *wizard) editCloneDir(dr *draft) error {
	dir := dr.cloneDir
	if err := w.run(true, page{fields: []huh.Field{huh.NewInput().Title(i18n.T("Where this device keeps its clones of the layer repositories")).
		Description(i18n.T("One folder per layer. Saving moves the clones cclayer made into it.")).Value(&dir).Validate(validCloneDir)}}); err != nil {
		return err
	}
	dr.cloneDir = cleanCloneDir(dir)
	return nil
}

// validCloneDir rejects a clone directory the manifest would refuse, or a
// file in its place.
func validCloneDir(s string) error {
	s = cleanCloneDir(s)
	if err := manifest.CheckCloneDir(s); err != nil {
		return err
	}
	if fi, err := os.Stat(manifest.ExpandHome(s)); err == nil && !fi.IsDir() {
		return fmt.Errorf("%s is a file, not a directory", s)
	}
	return nil
}

func cleanCloneDir(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1 {
		s = strings.TrimRight(s, "/")
	}
	return s
}

func (w *wizard) editRoots(dr *draft) error {
	roots := dr.roots
	if err := w.run(true, page{fields: []huh.Field{huh.NewInput().Title(i18n.T("Directories that hold your projects")).
		Description(i18n.T("comma separated; cclayer scans them for repositories")).Value(&roots)}}); err != nil {
		return err
	}
	dr.roots = strings.Join(splitRoots(roots), ", ")
	return nil
}

func (w *wizard) editIdentity(dr *draft) error {
	opts := []huh.Option[string]{huh.NewOption(i18n.T("None: git refuses to commit where no overlay matches (safest)"), "")}
	for _, n := range dr.names()[1:] {
		opts = append(opts, huh.NewOption(n, n))
	}
	v := dr.identity
	if err := w.run(true, page{fields: []huh.Field{huh.NewSelect[string]().Title(i18n.T("Identity for repositories no overlay matches")).Options(opts...).Value(&v)}}); err != nil {
		return err
	}
	dr.identity = v
	return nil
}

func (w *wizard) editTrust(dr *draft) error {
	var opts []huh.Option[string]
	for _, n := range dr.names() {
		opts = append(opts, huh.NewOption(n, n).Selected(slices.Contains(dr.trust, n)))
	}
	trust := slices.Clone(dr.trust)
	if err := w.run(true, page{fields: []huh.Field{huh.NewMultiSelect[string]().Title(i18n.T("Layers allowed to set git keys that run programs")).
		Description(i18n.T("core.hooksPath, credential.helper, shell aliases... Only trust layers you control")).
		Options(opts...).Value(&trust)}}); err != nil {
		return err
	}
	dr.trust = trust
	return nil
}

// commit writes the draft into the manifest and does what a changed
// location needs: drop the old credential, write a starter layer.toml.
func (w *wizard) commit(d *manifest.Device, dr *draft) error {
	moves, err := planMoves(d, dr)
	if err != nil {
		return err
	}
	// every new address is checked and every starter written before any
	// clone is moved or credential dropped, so a failure leaves the device
	// as it was
	for _, l := range dr.layers {
		if l.loc == l.orig {
			continue
		}
		kind, err := classifyLocation(l.loc)
		if err != nil {
			return err
		}
		if kind == locNew {
			if err := w.writeStarter(l); err != nil {
				return err
			}
		}
	}
	if err := w.moveClones(d, moves); err != nil {
		return err
	}
	d.CloneDir = dr.cloneDir
	if d.CloneDir == manifest.DefaultCloneDir {
		d.CloneDir = ""
	}
	for _, l := range dr.layers {
		if l.loc != l.orig {
			if err := w.setLocation(d, l); err != nil {
				return err
			}
		} else if l.auth != "" && l.access == accessNone {
			if err := removeCredential(w.e, d, l.name, false); err != nil {
				return err
			}
		}
	}
	d.Layers = dr.names()
	d.Roots = splitRoots(dr.roots)
	d.DefaultIdentity = dr.identity
	d.AutoPull = dr.autoPull
	d.Profiles = dr.profiles
	d.TrustExec = dr.trust
	d.Lang = dr.lang
	return nil
}

// location is what the overview shows as the current address of a layer:
// the URL it was cloned from, or the directory it lives in. A directory
// that is missing for now, such as a cloud folder still syncing, is still
// the saved address; reading it as empty would make saving it again write
// a starter layer.toml into the folder.
func (w *wizard) location(d *manifest.Device, name string) string {
	if url := d.Repo[name]; url != "" {
		return url
	}
	return d.Clone[name]
}

// setLocation records where a layer comes from. A URL or a bare repository
// is cloned into the device's clone directory; a directory is used where it is,
// its starter layer.toml written by commit beforehand when it was new.
func (w *wizard) setLocation(d *manifest.Device, l *layerDraft) error {
	name, loc := l.name, l.loc
	kind, err := classifyLocation(loc)
	if err != nil {
		return err
	}
	switch kind {
	case locURL, locGitDir:
		if kind == locGitDir {
			// git does not expand ~ and the hook runs elsewhere: store the
			// repository path exactly as git must see it
			loc = manifest.ExpandHome(localPath(loc))
		}
		if d.Repo[name] != loc {
			if err := w.dropCredential(d, name); err != nil {
				return err
			}
			d.Clone[name] = d.NewClonePath(name)
		}
		d.Repo[name] = loc
		return nil
	}
	if err := w.dropCredential(d, name); err != nil {
		return err
	}
	d.Clone[name] = localPath(loc)
	delete(d.Repo, name)
	return nil
}

// cloneMove is one layer clone that follows a new clone directory.
type cloneMove struct {
	layer    string
	from, to string // as the manifest stores them
	// absent: not cloned yet on this device, so only the path changes
	absent bool
}

// planMoves lists the clones that follow a changed clone directory: those
// cclayer made at the old default place. A clone path written by hand
// elsewhere stays. A target that is already taken stops the save.
func planMoves(d *manifest.Device, dr *draft) ([]cloneMove, error) {
	if dr.cloneDir == d.CloneRoot() {
		return nil, nil
	}
	if err := manifest.CheckCloneDir(dr.cloneDir); err != nil {
		return nil, err
	}
	next := manifest.Device{CloneDir: dr.cloneDir}
	var moves []cloneMove
	for _, l := range dr.layers {
		if !l.saved || d.Repo[l.name] == "" || d.Clone[l.name] != d.NewClonePath(l.name) {
			continue
		}
		m := cloneMove{layer: l.name, from: d.Clone[l.name], to: next.NewClonePath(l.name)}
		if !exists(manifest.ExpandHome(m.from)) {
			m.absent = true
			moves = append(moves, m)
			continue
		}
		if exists(manifest.ExpandHome(m.to)) {
			return nil, fmt.Errorf("cannot move the %s clone to %s: something is already there", l.name, m.to)
		}
		moves = append(moves, m)
	}
	return moves, nil
}

// moveClones moves the planned clones and records their new paths. A move
// that fails puts the ones already done back.
func (w *wizard) moveClones(d *manifest.Device, moves []cloneMove) error {
	var done []cloneMove
	for _, m := range moves {
		if m.absent {
			continue
		}
		to := manifest.ExpandHome(m.to)
		err := os.MkdirAll(filepath.Dir(to), 0o755)
		if err == nil {
			err = os.Rename(manifest.ExpandHome(m.from), to)
		}
		if err != nil {
			for _, back := range done {
				os.Rename(manifest.ExpandHome(back.to), manifest.ExpandHome(back.from))
			}
			return fmt.Errorf("moving the %s clone to %s: %w (on another disk, move it by hand and edit [clone] in the device manifest)", m.layer, m.to, err)
		}
		done = append(done, m)
		w.e.printf("moved %s -> %s\n", m.from, m.to)
	}
	for _, m := range moves {
		d.Clone[m.layer] = m.to
	}
	return nil
}

// writeStarter writes the starter layer.toml of a layer whose new address
// is an empty or missing directory.
func (w *wizard) writeStarter(l *layerDraft) error {
	dir := manifest.ExpandHome(strings.TrimPrefix(l.loc, "file://"))
	var err error
	if l.name == "base" {
		err = starterBase(dir)
	} else {
		err = starterOverlay(dir, l.name, l.author, l.email, l.remote)
	}
	if err != nil {
		return err
	}
	w.e.printf("wrote %s\n", w.e.tilde(filepath.Join(dir, "layer.toml")))
	return nil
}

// dropCredential removes the key files, ssh stanza or token note of a layer
// whose source is about to change, so nothing is left behind that `keys
// remove` or `leave` would no longer know about.
func (w *wizard) dropCredential(d *manifest.Device, name string) error {
	if d.Auth[name] == "" {
		return nil
	}
	return removeCredential(w.e, d, name, false)
}

// credentials sets up the access chosen on the overview, after the
// manifest is saved: a deploy key is generated and attached, a token is
// pasted now.
func (w *wizard) credentials(d *manifest.Device, dr *draft) error {
	for _, l := range dr.layers {
		method, port443 := l.access, false
		switch method {
		case accessKey443:
			method, port443 = accessKey, true
		case accessKey, accessToken:
		default:
			continue
		}
		if !isRemoteURL(d.Repo[l.name]) {
			continue
		}
		token := ""
		if method == accessToken {
			repo, err := keys.ParseRepo(d.Repo[l.name])
			if err != nil {
				return err
			}
			note := huh.NewNote().Title(fmt.Sprintf(i18n.T("Create the token for %s"), l.name)).Description(keys.TokenInstructions(repo.Owner, []string{repo.Name}))
			if w.accessible && !stdinIsTerminal(w.e) {
				// a pipe cannot hide input; huh's password prompt needs a terminal
				if err := w.form(note); err != nil {
					return err
				}
				line, _ := bufio.NewReader(w.input()).ReadString('\n')
				token = line
			} else if err := w.form(note, huh.NewInput().Title(i18n.T("Paste the token")).EchoMode(huh.EchoModePassword).Value(&token)); err != nil {
				return err
			}
			if token = strings.TrimSpace(token); token == "" {
				return errors.New("the token is empty")
			}
		}
		res, err := setupCredential(w.e, d, l.name, d.Repo[l.name], method, port443, token)
		if err != nil {
			return err
		}
		w.e.printf("%s", res)
		if err := d.Save(w.e.DevicePath); err != nil {
			return err
		}
	}
	return nil
}

// validLayerName rejects names the device manifest would refuse.
func validLayerName(taken []string) func(string) error {
	return func(s string) error {
		if s == "" || s == "base" {
			return errors.New("give the overlay a name other than base")
		}
		if slices.Contains(taken, s) {
			return errors.New("that layer already exists")
		}
		probe := manifest.Device{Layers: []string{"base", s}, Roots: []string{"x"}, Clone: map[string]string{"base": "x", s: "y"}}
		return probe.Validate()
	}
}

func stdinIsTerminal(e *Env) bool {
	f, ok := e.Stdin.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
