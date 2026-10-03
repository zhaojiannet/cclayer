package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/claudecli"
	"github.com/zhaojiannet/cclayer/internal/fsx"
	"github.com/zhaojiannet/cclayer/internal/gitconf"
	"github.com/zhaojiannet/cclayer/internal/i18n"
	"github.com/zhaojiannet/cclayer/internal/inject"
	"github.com/zhaojiannet/cclayer/internal/mirror"
	"github.com/zhaojiannet/cclayer/internal/project"
	"github.com/zhaojiannet/cclayer/internal/settings"
	"github.com/zhaojiannet/cclayer/internal/state"
)

const keepBackups = 5

func runApply(e *Env, args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	pull := fs.Bool("pull", false, "fast-forward the layer clones first")
	hook := fs.Bool("hook", false, "non-interactive mode for the SessionStart hook")
	mcpForce := fs.Bool("mcp-force", false, "remove and re-add MCP servers that already exist")
	replay := fs.Bool("replay-plugins", false, "run plugin and marketplace commands again even if this device ran them before")
	if err := fs.Parse(args); err != nil {
		return usagef("apply: %v", err)
	}
	if *hook {
		return applyHook(e, *mcpForce)
	}
	return apply(e, applyOpts{pull: *pull, mcpForce: *mcpForce, replay: *replay})
}

type applyOpts struct {
	pull     bool
	mcpForce bool
	replay   bool
}

// applyHook wraps apply for the SessionStart hook: silent when auto_pull is
// off, never asks, never fails the session.
func applyHook(e *Env, mcpForce bool) error {
	e.Interactive = false
	loaded, err := e.Load()
	if err != nil {
		e.errorf("cclayer hook: %v\n", err)
		return nil
	}
	if !loaded.Device.AutoPull {
		return nil
	}
	if err := apply(e, applyOpts{pull: true, mcpForce: mcpForce}); err != nil {
		if errors.Is(err, state.ErrLocked) {
			return nil
		}
		e.errorf("cclayer hook: %v\n", err)
	}
	return nil
}

// report collects what apply did for the one-line or full summary.
type report struct {
	written   []string
	skipped   []string
	conflicts []string
	notes     []string
}

func (r *report) add(list *[]string, format string, args ...any) {
	*list = append(*list, fmt.Sprintf(i18n.T(format), args...))
}

func apply(e *Env, opt applyOpts) (err error) {
	release, err := e.State.Lock()
	if err != nil {
		return err
	}
	defer release()

	loaded, err := e.Load()
	if err != nil {
		return err
	}
	var rep report
	if opt.pull {
		for _, l := range loaded.Layers {
			msg, err := pullFastForward(loaded.Device, l.Name, l.Dir)
			if err != nil {
				return err
			}
			rep.add(&rep.notes, "pull %s: %s", l.Name, msg)
		}
		// manifests may have changed
		if loaded, err = e.Load(); err != nil {
			return err
		}
	}

	// a pulled layer may carry what check refuses; never spread it
	findings, err := checkLayers(e, loaded)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		for _, f := range findings {
			e.errorf("%s\n", f)
		}
		return fmt.Errorf("check refused %d item(s) in the layers; nothing applied", len(findings))
	}

	applied, err := e.State.Load()
	if err != nil {
		return err
	}
	// whatever happens below, remember what was written so far
	defer func() {
		if saveErr := e.State.Save(applied); saveErr != nil && err == nil {
			err = saveErr
		}
	}()
	backup := e.State.NewBackup()
	base := loaded.Base()

	ctx := &applyCtx{applied: applied, replay: opt.replay, hookDecisions: map[string]bool{}}
	if base.Manifest.Claude != nil {
		ctx.settingsKeys = base.Manifest.Claude.SettingsKeys
	}
	if err := applyMirror(e, base, applied, backup, ctx, &rep); err != nil {
		return err
	}
	if err := applySettings(e, base, applied, backup, ctx, &rep); err != nil {
		return err
	}
	if err := applyGit(e, loaded, applied, backup, &rep); err != nil {
		return err
	}
	if e.Interactive {
		if err := applyBasePlugins(e, base, opt.mcpForce, ctx, &rep); err != nil {
			return err
		}
	} else {
		rep.add(&rep.skipped, "plugin and MCP steps run only in interactive apply")
	}
	if err := applyProjects(e, loaded, applied, ctx, &rep); err != nil {
		return err
	}
	if err := applyProfiles(e, loaded, applied, backup, ctx, &rep); err != nil {
		return err
	}
	if err := e.State.Prune(keepBackups); err != nil {
		return err
	}
	printReport(e, &rep, backup.Count())
	return nil
}

// applyMirror syncs the base layer's claude/ paths into ~/.claude.
func applyMirror(e *Env, base Layer, applied *state.Applied, backup *state.Backup, ctx *applyCtx, rep *report) error {
	return mirrorInto(e, base, e.ClaudeDir, false, applied, backup, ctx, rep)
}

// mirrorInto syncs the base layer's claude/ paths into one target root:
// ~/.claude or a profile directory. skipClaudeMD leaves CLAUDE.md out, for
// profiles, where ~/.claude/CLAUDE.md is loaded anyway (claude-code #88528).
func mirrorInto(e *Env, base Layer, root string, skipClaudeMD bool, applied *state.Applied, backup *state.Backup, ctx *applyCtx, rep *report) error {
	if base.Manifest.Claude == nil {
		return nil
	}
	for _, p := range base.Manifest.Claude.Paths {
		if skipClaudeMD && p == "CLAUDE.md" {
			continue
		}
		src, err := layerPath(base.Dir, "claude/"+p)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, p)
		var actions []mirror.Action
		if strings.HasSuffix(p, "/") {
			actions, err = mirror.Plan(src, dst, scopedIgnore(base.Manifest.Claude.Ignore, p), applied.Hashes)
		} else {
			actions, err = planFile(src, dst, applied.Hashes)
		}
		if err != nil {
			return err
		}
		actions, held := holdCode(e, p, actions, ctx, rep)
		written, skipped, err := mirror.Apply(actions, backup, applied)
		if err != nil {
			return err
		}
		for _, a := range written {
			rep.add(&rep.written, "%s", e.tilde(a.Target))
		}
		skipped = append(skipped, held...)
		for _, a := range skipped {
			switch a.Decision {
			case state.Conflict:
				if content, _ := os.ReadFile(a.Source); e.Interactive && mayRun(layerRel(p, a.Rel), a.Source, content) {
					// the overwrite puts code in place, so it is shown like any other change Claude Code may run
					e.printf("The layer's version of %s, a file Claude Code may run:\n%s\n", e.tilde(a.Target), indent(strings.TrimRight(string(content), "\n")))
				}
				if e.Interactive && e.confirm("%s was edited here and the layer has a new version. Overwrite with the layer's version?", e.tilde(a.Target)) {
					if err := mirror.Force(a, backup, applied); err != nil {
						return err
					}
					rep.add(&rep.written, "%s (conflict resolved)", e.tilde(a.Target))
				} else {
					rep.add(&rep.conflicts, "%s", e.tilde(a.Target))
				}
			case state.LocalOnly:
				if a.Delete {
					rep.add(&rep.skipped, "%s (only on this device, run capture --add to keep it in the layer)", e.tilde(a.Target))
				} else {
					rep.add(&rep.skipped, "%s (local change, run capture)", e.tilde(a.Target))
				}
			}
		}
	}
	return nil
}

// holdCode holds every file Claude Code may run until it is confirmed: a
// file under hooks/, skills/, commands/ or agents/ (skills and commands run
// inline shell, skills and agents declare hooks in their frontmatter), any
// file that is not markdown (hook and status line commands call scripts in
// any language from anywhere under ~/.claude), and any executable or shebang
// file. Plain markdown elsewhere is instructions, not code, and is mirrored
// as it is. Interactive apply lists the files of one path with their content
// and asks once; --hook leaves them alone. A file confirmed on this device
// with the same path and content is not asked about again, so a new profile
// gets the skills ~/.claude has without a second question.
func holdCode(e *Env, p string, actions []mirror.Action, ctx *applyCtx, rep *report) (keep, held []mirror.Action) {
	var gated []mirror.Action
	var contents [][]byte
	for _, a := range actions {
		if a.Decision != state.Write || a.Delete {
			keep = append(keep, a)
			continue
		}
		content, _ := os.ReadFile(a.Source)
		if !mayRun(layerRel(p, a.Rel), a.Source, content) || ctx.applied.ApprovedCode[approvalKey(p, a.Rel, content)] {
			keep = append(keep, a)
			continue
		}
		gated = append(gated, a)
		contents = append(contents, content)
	}
	if len(gated) == 0 {
		return keep, nil
	}
	var sum strings.Builder
	for i, a := range gated {
		sum.WriteString(a.Rel + "\x00" + state.Hash(contents[i]) + "\x00")
	}
	key := state.Hash([]byte(sum.String()))
	ok, seen := ctx.hookDecisions[key]
	if !seen {
		if !e.Interactive {
			for _, a := range gated {
				rep.add(&rep.skipped, "%s: Claude Code may run it, run cclayer apply to review", e.tilde(a.Target))
			}
			ctx.hookDecisions[key] = false
			return keep, gated
		}
		e.printf("The base layer changes files Claude Code may run:\n")
		for i, a := range gated {
			e.printf("--- %s\n%s\n", e.tilde(a.Target), indent(strings.TrimRight(string(contents[i]), "\n")))
		}
		ok = e.confirm("Apply these %d file(s)?", len(gated))
		ctx.hookDecisions[key] = ok
		if ok {
			for i, a := range gated {
				ctx.applied.ApprovedCode[approvalKey(p, a.Rel, contents[i])] = true
			}
		} else {
			for _, a := range gated {
				rep.add(&rep.skipped, "%s: declined", e.tilde(a.Target))
			}
		}
	}
	if ok {
		return append(keep, gated...), nil
	}
	return keep, gated
}

func approvalKey(p, rel string, content []byte) string {
	return strings.ToLower(layerRel(p, rel)) + "\x00" + state.Hash(content)
}

// layerRel is a mirrored file's path relative to ~/.claude.
func layerRel(p, rel string) string {
	if strings.HasSuffix(p, "/") {
		return p + filepath.ToSlash(rel)
	}
	return p
}

// mayRun reports whether a mirrored file can be run by Claude Code or a
// shell. The first directory compares the way case-insensitive file systems
// fold names (Unicode folding: "ſkills" is "skills" there), which ToLower
// does not follow.
func mayRun(rel, src string, content []byte) bool {
	s := filepath.ToSlash(rel)
	first, _, nested := strings.Cut(s, "/")
	if nested {
		for _, d := range []string{"hooks", "skills", "commands", "agents"} {
			if strings.EqualFold(first, d) {
				return true
			}
		}
	}
	return !strings.HasSuffix(strings.ToLower(s), ".md") || looksExecutable(src, content)
}

// scopedIgnore keeps the ignore entries under one mirrored path and makes
// them relative to it, which is what mirror.Plan compares against.
func scopedIgnore(ignore []string, p string) []string {
	var out []string
	for _, ig := range ignore {
		if rest, ok := strings.CutPrefix(filepath.ToSlash(ig), filepath.ToSlash(p)); ok && rest != "" {
			out = append(out, rest)
		}
	}
	return out
}

// planFile is mirror.Plan for a single file path such as CLAUDE.md.
func planFile(src, dst string, lastApplied map[string]string) ([]mirror.Action, error) {
	want, err := state.HashFile(src)
	if err != nil {
		return nil, err
	}
	cur, err := state.HashFile(dst)
	if err != nil {
		return nil, err
	}
	if want == "" {
		return nil, nil // layer lacks the file; nothing to do
	}
	d := state.Decide(cur, lastApplied[dst], want)
	if d == state.Unchanged {
		return nil, nil
	}
	return []mirror.Action{{Rel: filepath.Base(dst), Target: dst, Source: src, Decision: d}}, nil
}

// applySettings merges the base-owned keys into ~/.claude/settings.json and
// records in ctx which keys were allowed through, so profiles apply the
// same set.
func applySettings(e *Env, base Layer, applied *state.Applied, backup *state.Backup, ctx *applyCtx, rep *report) error {
	if base.Manifest.Claude == nil || len(base.Manifest.Claude.SettingsKeys) == 0 {
		return nil
	}
	// A layer without claude/settings.json has not taken in any settings yet,
	// as right after setup wrote a starter. Merging would read every owned
	// key as removed and delete the device's values; capture fills the file
	// from the device instead.
	if p, err := layerPath(base.Dir, "claude/settings.json"); err != nil {
		return err
	} else if !exists(p) {
		ctx.settingsKeys = nil // profiles merge the same set, so none there either
		return nil
	}
	src, err := baseSettings(base)
	if err != nil {
		return err
	}
	target := filepath.Join(e.ClaudeDir, "settings.json")
	dst, err := settings.Load(target)
	if err != nil {
		return err
	}
	keys := base.Manifest.Claude.SettingsKeys
	for _, x := range gatedKeysChanged(dst, src, keys) {
		cur, _ := dst.Get(x)
		want, _ := src.Get(x)
		if !e.Interactive {
			rep.add(&rep.skipped, "%s: %s changed in the base layer, run cclayer apply to review them", e.tilde(target), x)
			keys = without(keys, x)
			continue
		}
		e.printf("The base layer changes %s, %s:\n  current: %s\n  layer:   %s\n", x, gateReason(x), compact(cur), compact(want))
		if !e.confirm("Apply the new %s?", x) {
			rep.add(&rep.skipped, "%s: %s change declined", e.tilde(target), x)
			keys = without(keys, x)
		}
	}
	ctx.settingsKeys = keys
	changed := settings.Merge(dst, src, keys)
	if len(changed) == 0 {
		return nil
	}
	b, err := dst.Marshal()
	if err != nil {
		return err
	}
	if err := backup.Save(target); err != nil {
		return err
	}
	if err := fsx.WriteFile(e.ClaudeDir, target, b, 0o644); err != nil {
		return err
	}
	applied.Hashes[target] = state.Hash(b)
	rep.add(&rep.written, "%s (%s)", e.tilde(target), strings.Join(changed, ", "))
	return nil
}

// execSettingsKeys make Claude Code run programs: hook and status line
// commands, the credential and header helper commands, the commands behind
// file suggestions and the subagent display, the process wrapper, and the
// marketplaces Claude Code registers on its own at startup. enabledPlugins
// is confirmed too, as a key cclayer does not count as harmless: it installs
// nothing by itself, but it switches on a plugin from a marketplace this
// device already knows, and that plugin's hooks run from then on.
var execSettingsKeys = []string{
	"hooks", "statusLine", "subagentStatusLine", "fileSuggestion", "spellcheck", "processWrapper", "policyHelper",
	"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "gcpAuthRefresh", "otelHeadersHelper", "proxyAuthHelper",
	"extraKnownMarketplaces",
}

// harmlessSettingsKeys are merged without a question: appearance, models,
// context and workflow preferences, attribution, and the settings that only
// restrict what Claude Code may do. Everything else is confirmed like the
// exec keys, because a layer is pulled from a repository and Claude Code
// adds settings that widen access or run programs faster than any denylist
// follows. A key is harmless when it is one of these or sits below one.
var harmlessSettingsKeys = []string{
	"theme", "tui", "viewMode", "verbose", "editorMode", "language", "timeFormat", "timeZone", "prefersReducedMotion",
	"syntaxHighlightingDisabled", "maxProseWidth", "showTurnDuration", "showThinkingSummaries", "showClearContextOnPlanAccept",
	"spinnerTipsEnabled", "spinnerTipsOverride", "spinnerVerbs", "promptSuggestionEnabled", "emojiCompletionEnabled",
	"autoScrollEnabled", "wheelScrollAccelerationEnabled", "terminalProgressBarEnabled", "terminalTitleFromRename",
	"axScreenReader", "awaySummaryEnabled", "teammateMode", "voice", "voiceEnabled", "defaultShell", "respondToBashCommands",
	"model", "fallbackModel", "availableModels", "modelPicker", "modelSettings", "switchModelsOnFlag",
	"effortLevel", "maxEffortLevel", "alwaysThinkingEnabled", "fastMode", "fastModePerSessionOptIn",
	"autoCompactEnabled", "autoCompactWindow", "promptCacheTtl", "subagentPromptCacheTtl", "bashOutputMaxChars",
	"fileCheckpointingEnabled", "includeGitInstructions", "includeCoAuthoredBy", "attribution",
	"claudeMdExcludes", "skillOverrides", "skillListingBudgetFraction", "skillListingMaxDescChars",
	"outputStyle", "enableWorkflows", "disableWorkflows", "ultracode", "workflowKeywordTriggerEnabled",
	"workflowSizeGuideline", "autoUpdatesChannel", "minimumVersion", "feedbackSurveyRate",
	"agentPushNotifEnabled", "inputNeededNotifEnabled", "preferredNotifChannel", "companyAnnouncements", "footerLinksRegexes",
	"permissions.deny", "permissions.disableBypassPermissionsMode", "permissions.blockReadsOutsideWorkingDirectories",
	"deniedMcpServers", "disabledMcpjsonServers",
	"disableAllHooks", "disableSkillShellExecution", "disableBundledSkills", "disableClaudeAiConnectors",
	"disableRemoteControl", "disableAutoMode", "disableAgentView", "enableArtifact", "disableArtifact",
}

// gatedKeysChanged returns the owned settings keys whose value differs
// between the file on disk and the layer and that need a confirmation: the
// exec keys, and anything not known to be harmless. An exec key is reported
// under its own name even when the layer owns a path below it.
func gatedKeysChanged(dst, src settings.Doc, keys []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range keys {
		if len(settings.Diff(dst, src, []string{k})) == 0 {
			continue
		}
		name := k
		if x := execKeyOf(k); x != "" {
			name = x
		} else if isHarmlessKey(k) && !widensRestriction(dst, src, k) {
			continue
		} else if keepsOff(dst, src, k) {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// execKeyOf returns the exec key that k names or sits below or above, or "".
func execKeyOf(k string) string {
	for _, x := range execSettingsKeys {
		if k == x || strings.HasPrefix(k, x+".") || strings.HasPrefix(x, k+".") {
			return x
		}
	}
	return ""
}

// restrictiveKeys only ever limit what Claude Code may do. The layer may add
// to them freely; removing or loosening one of the user's limits is a change
// like any other gated key, since a layer that claims the key and leaves it
// out would otherwise delete the limit without a word.
var restrictiveKeys = []string{
	"permissions.deny", "permissions.disableBypassPermissionsMode", "permissions.blockReadsOutsideWorkingDirectories",
	"deniedMcpServers", "disabledMcpjsonServers", "disableAllHooks", "disableSkillShellExecution", "disableBundledSkills",
	"disableClaudeAiConnectors", "disableRemoteControl", "disableAutoMode", "disableAgentView", "enableArtifact", "disableArtifact",
}

// widensRestriction reports whether writing the layer's value of an owned
// key would drop or loosen something the device has under a restrictive key.
// An owned key above a restrictive one (permissions) compares each of them.
func widensRestriction(dst, src settings.Doc, k string) bool {
	for _, r := range restrictiveKeys {
		if r != k && !strings.HasPrefix(r, k+".") && !strings.HasPrefix(k, r+".") {
			continue
		}
		path := r
		if strings.HasPrefix(k, r+".") {
			path = k
		}
		cur, has := dst.Get(path)
		if !has {
			continue
		}
		want, ok := src.Get(path)
		if !ok || !covers(want, cur) {
			return true
		}
	}
	return false
}

// covers reports whether want keeps everything cur has: every element of a
// list, every key of an object, or the same value.
func covers(want, cur any) bool {
	switch c := cur.(type) {
	case []any:
		w, ok := want.([]any)
		if !ok {
			return false
		}
		for _, x := range c {
			found := false
			for _, y := range w {
				if compact(x) == compact(y) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case map[string]any:
		w, ok := want.(map[string]any)
		if !ok {
			return false
		}
		for key, v := range c {
			if !covers(w[key], v) {
				return false
			}
		}
		return true
	default:
		return compact(want) == compact(cur)
	}
}

// keepsOff reports whether the layer only writes false where the device has
// false or nothing: a switch that stays off changes nothing worth a question.
// Not for keys where false is the loosening direction: disable* switches,
// anything under permissions or sandbox, and the two defaults-on guards
// useAutoModeDuringPlan and respectGitignore.
func keepsOff(dst, src settings.Doc, k string) bool {
	last := k[strings.LastIndex(k, ".")+1:]
	if strings.HasPrefix(last, "disable") || strings.HasPrefix(k, "permissions") || strings.HasPrefix(k, "sandbox") ||
		k == "useAutoModeDuringPlan" || k == "respectGitignore" {
		return false
	}
	if want, ok := src.Get(k); !ok || want != false {
		return false
	}
	cur, has := dst.Get(k)
	return !has || cur == false
}

func isHarmlessKey(k string) bool {
	for _, h := range harmlessSettingsKeys {
		if k == h || strings.HasPrefix(k, h+".") {
			return true
		}
	}
	return false
}

// gateReason explains in the prompt why a key is held.
func gateReason(k string) string {
	if execKeyOf(k) != "" {
		return i18n.T("which makes Claude Code run programs")
	}
	return i18n.T("which cclayer does not know to be harmless")
}

// looksExecutable reports whether a mirrored file is something Claude Code or
// a shell would run: an execute bit, or a shebang line.
func looksExecutable(path string, content []byte) bool {
	if bytes.HasPrefix(content, []byte("#!")) {
		return true
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().Perm()&0o111 != 0
}

func without(keys []string, drop string) []string {
	var out []string
	for _, k := range keys {
		if k != drop && !strings.HasPrefix(k, drop+".") {
			out = append(out, k)
		}
	}
	return out
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// applyGit regenerates ~/.gitconfig.cclayer and keeps the include block.
func applyGit(e *Env, loaded *Loaded, applied *state.Applied, backup *state.Backup, rep *report) error {
	in := gitconf.Input{Dir: "~/.gitconfig.cclayer.d", TokenRepos: tokenRepos(loaded.Device)}
	var err error
	if in.BaseFragment, err = fragment(loaded.Base()); err != nil {
		return err
	}
	if err := gitconf.CheckFragment(in.BaseFragment, loaded.Device.Trusted("base")); err != nil {
		return fmt.Errorf("base layer: %w", err)
	}
	if in.BaseFragment, err = gitconf.RenderFragment(in.BaseFragment); err != nil {
		return fmt.Errorf("base layer: %w", err)
	}
	for _, o := range loaded.Overlays() {
		frag, err := fragment(o)
		if err != nil {
			return err
		}
		if err := gitconf.CheckFragment(frag, loaded.Device.Trusted(o.Name)); err != nil {
			return fmt.Errorf("layer %s: %w", o.Name, err)
		}
		if frag, err = gitconf.RenderFragment(frag); err != nil {
			return fmt.Errorf("layer %s: %w", o.Name, err)
		}
		ov := gitconf.Overlay{Name: o.Name, UserName: o.Manifest.Identity.Name, Email: o.Manifest.Identity.Email, Patterns: patterns(o), Fragment: frag}
		in.Overlays = append(in.Overlays, ov)
		if o.Name == loaded.Device.DefaultIdentity {
			def := ov
			in.DefaultIdentity = &def
		}
	}
	out := gitconf.Generate(in)

	mainPath := filepath.Join(e.Home, ".gitconfig.cclayer")
	dirPath := filepath.Join(e.Home, ".gitconfig.cclayer.d")
	if err := writeManaged(e, mainPath, []byte(out.Main), applied, backup, rep); err != nil {
		return err
	}
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return err
	}
	wanted := map[string]bool{}
	for name, content := range out.Files {
		p := filepath.Join(dirPath, name)
		wanted[p] = true
		if err := writeManaged(e, p, []byte(content), applied, backup, rep); err != nil {
			return err
		}
	}
	// drop identity files of layers no longer enabled
	entries, _ := os.ReadDir(dirPath)
	for _, en := range entries {
		p := filepath.Join(dirPath, en.Name())
		if !wanted[p] {
			if err := backup.Save(p); err != nil {
				return err
			}
			os.Remove(p)
			delete(applied.Hashes, p)
			rep.add(&rep.written, "%s (removed)", e.tilde(p))
		}
	}

	cur, err := gitconf.ReadFile(e.GitConfig)
	if err != nil {
		return err
	}
	next, changed := gitconf.EnsureIncludeBlock(cur)
	if changed {
		if err := backup.Save(e.GitConfig); err != nil {
			return err
		}
		if err := fsx.WriteUserFile(e.Home, e.GitConfig, []byte(next), 0o644); err != nil {
			return err
		}
		rep.add(&rep.written, "%s (include block)", e.tilde(e.GitConfig))
	}
	return nil
}

// writeManaged writes a file cclayer fully owns, with backup and hash record.
func writeManaged(e *Env, path string, content []byte, applied *state.Applied, backup *state.Backup, rep *report) error {
	cur, err := state.HashFile(path)
	if err != nil {
		return err
	}
	want := state.Hash(content)
	if cur == want {
		applied.Hashes[path] = want
		return nil
	}
	if err := backup.Save(path); err != nil {
		return err
	}
	if err := fsx.WriteFile(filepath.Dir(path), path, content, 0o644); err != nil {
		return err
	}
	applied.Hashes[path] = want
	rep.add(&rep.written, "%s", e.tilde(path))
	return nil
}

// applyCtx carries what the claude-command steps need to stay idempotent.
type applyCtx struct {
	applied       *state.Applied
	replay        bool
	settingsKeys  []string        // owned keys that passed the hooks gate this run
	hookDecisions map[string]bool // hash of a set of held files -> apply or hold
}

// applyBasePlugins plans plugin and MCP commands for the base and runs them
// after one confirmation.
func applyBasePlugins(e *Env, base Layer, mcpForce bool, ctx *applyCtx, rep *report) error {
	doc, err := baseSettings(base)
	if err != nil {
		return err
	}
	markets, plugins := pluginsFrom(doc)
	servers, err := mcpServers(base)
	if err != nil {
		return err
	}
	// checked before anything runs: `claude mcp get` is called while planning
	if err := claudecli.CheckArgs(markets, plugins, servers); err != nil {
		return fmt.Errorf("base layer: %w", err)
	}
	cmds := claudecli.PlanPlugins("", markets, plugins, "user")
	if claudeAvailable() {
		if present, ok := claudecli.ListPresent(e.Runner); ok {
			cmds = claudecli.SkipPresent(cmds, present)
		}
	}
	if len(servers) > 0 {
		if !claudeAvailable() {
			rep.add(&rep.skipped, "MCP servers: claude not on PATH")
		} else {
			mcpCmds, err := claudecli.PlanMCP(e.Runner, servers, mcpForce)
			if err != nil {
				return err
			}
			cmds = append(cmds, mcpCmds...)
		}
	}
	return runPlanned(e, cmds, i18n.T("base layer"), ctx, rep)
}

// runPlanned shows planned claude commands and runs them after one yes.
// Commands this device already ran are skipped unless --replay-plugins;
// MCP commands are never remembered because `claude mcp get` is the truth.
func runPlanned(e *Env, cmds []claudecli.Command, what string, ctx *applyCtx, rep *report) error {
	if ctx != nil && !ctx.replay {
		var fresh []claudecli.Command
		for _, c := range cmds {
			if c.Args[0] == "plugin" && ctx.applied.Commands[c.String()] {
				continue
			}
			fresh = append(fresh, c)
		}
		cmds = fresh
	}
	if len(cmds) == 0 {
		return nil
	}
	if !claudeAvailable() {
		rep.add(&rep.skipped, "%s: %d claude commands (claude not on PATH)", what, len(cmds))
		return nil
	}
	if !e.Interactive {
		rep.add(&rep.skipped, "%s: %d claude commands need confirmation, run cclayer apply", what, len(cmds))
		return nil
	}
	e.printf("Commands to run for the %s:\n", what)
	for _, c := range cmds {
		e.printf("  %s\n", c)
	}
	if !e.confirm("Run them now?") {
		rep.add(&rep.skipped, "%s: %d claude commands declined", what, len(cmds))
		return nil
	}
	ran := 0
	for _, c := range cmds {
		if err := claudecli.Execute(e.Runner, []claudecli.Command{c}); err != nil {
			rep.add(&rep.skipped, "%s: `%s` failed, the rest were not run: %s", what, c, firstLine(err.Error()))
			break
		}
		ran++
		if ctx != nil && c.Args[0] == "plugin" {
			ctx.applied.Commands[c.String()] = true
		}
	}
	if ran > 0 {
		rep.add(&rep.written, "%s: %d claude commands run", what, ran)
	}
	return nil
}

// applyProjects discovers repositories, assigns them to overlays and writes
// the per-project files.
func applyProjects(e *Env, loaded *Loaded, applied *state.Applied, ctx *applyCtx, rep *report) error {
	overlays := loaded.Overlays()
	if len(overlays) == 0 {
		return nil
	}
	var roots []string
	for _, r := range loaded.Device.Roots {
		roots = append(roots, expand(e, r))
	}
	projects, err := project.Discover(roots)
	if err != nil {
		return err
	}
	var pov []project.Overlay
	for _, o := range overlays {
		pov = append(pov, project.Overlay{Name: o.Name, Patterns: patterns(o)})
	}
	assignments := project.Assign(projects, pov)
	seen := map[string]bool{}
	anyAssigned := false
	for _, a := range assignments {
		seen[a.Project.Dir] = true
		if len(a.Conflict) > 0 {
			rep.add(&rep.conflicts, "%s matches %s; fix the [[match]] patterns", e.tilde(a.Project.Dir), describeOverlays(loaded, a.Conflict))
			continue
		}
		if a.Layer == "" {
			if old, ok := applied.Projects[a.Project.Dir]; ok {
				// no longer matches: take the old overlay's files out
				if o, found := loaded.Find(old); found {
					if err := removeInjected(o, a.Project.Dir); err != nil {
						return err
					}
					rep.add(&rep.written, "%s (no longer matches %s, injected files removed)", e.tilde(a.Project.Dir), old)
				}
				delete(applied.Projects, a.Project.Dir)
			}
			continue
		}
		anyAssigned = true
		if old, ok := applied.Projects[a.Project.Dir]; ok && old != a.Layer {
			if o, found := loaded.Find(old); found {
				if err := removeInjected(o, a.Project.Dir); err != nil {
					return err
				}
			}
		}
		applied.Projects[a.Project.Dir] = a.Layer
		o, _ := loaded.Find(a.Layer)
		if _, err := injectInto(e, o, a.Project.Dir, ctx, rep); err != nil {
			// one project's problem (a symlinked file, a half-written manifest)
			// must not stop the others
			rep.add(&rep.conflicts, "%s: %s", e.tilde(a.Project.Dir), firstLine(err.Error()))
			continue
		}
	}
	// projects remembered from earlier runs whose directory is gone stay in
	// the map so leave can still purge their state
	for dir := range applied.Projects {
		if !seen[dir] && exists(dir) {
			delete(applied.Projects, dir)
		}
	}
	if anyAssigned || len(applied.Projects) > 0 {
		path, err := inject.ExcludesFile()
		if err != nil {
			return err
		}
		added, err := inject.EnsureExcludes(path)
		if err != nil {
			return err
		}
		if len(added) > 0 {
			rep.add(&rep.written, "%s (+%s)", e.tilde(path), strings.Join(added, ", "))
		}
	}
	return nil
}

// describeOverlays names overlays together with their match patterns.
func describeOverlays(loaded *Loaded, names []string) string {
	var parts []string
	for _, n := range names {
		if o, ok := loaded.Find(n); ok {
			parts = append(parts, fmt.Sprintf("%s (%s)", n, strings.Join(patterns(o), ", ")))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " and ")
}

// removeInjected takes an overlay's keys and block out of a project.
func removeInjected(o Layer, dir string) error {
	if err := inject.RemoveSettingsLocal(dir, injectKeys(o)); err != nil {
		return err
	}
	return inject.RemoveClaudeLocal(dir)
}

// injectInto writes one overlay's files into one project and plans its plugins.
func injectInto(e *Env, o Layer, dir string, ctx *applyCtx, rep *report) (changed bool, err error) {
	keys := injectKeys(o)
	if len(keys) > 0 {
		tmpl, err := overlayTemplate(o)
		if err != nil {
			return false, err
		}
		cur, err := settings.Load(filepath.Join(dir, ".claude", "settings.local.json"))
		if err != nil {
			return false, err
		}
		for _, x := range gatedKeysChanged(cur, tmpl, keys) {
			want, _ := tmpl.Get(x)
			if !e.Interactive {
				rep.add(&rep.skipped, "%s: %s from overlay %s need confirmation, run cclayer apply", e.tilde(dir), x, o.Name)
				keys = without(keys, x)
				continue
			}
			e.printf("Overlay %s sets %s in %s, %s:\n  %s\n", o.Name, x, e.tilde(dir), gateReason(x), compact(want))
			if !e.confirm("Write the %s into this project?", x) {
				rep.add(&rep.skipped, "%s: %s from overlay %s declined", e.tilde(dir), x, o.Name)
				keys = without(keys, x)
			}
		}
		c, err := inject.SettingsLocal(dir, tmpl, keys)
		if err != nil {
			return false, err
		}
		if c {
			changed = true
			rep.add(&rep.written, "%s/.claude/settings.local.json (%s)", e.tilde(dir), o.Name)
		}
		markets, plugins := pluginsFrom(settings.Extract(tmpl, keys))
		if err := claudecli.CheckArgs(markets, plugins, nil); err != nil {
			return changed, fmt.Errorf("layer %s: %w", o.Name, err)
		}
		if cmds := claudecli.PlanPlugins(dir, markets, plugins, "local"); len(cmds) > 0 && e.Interactive {
			if err := runPlanned(e, cmds, fmt.Sprintf(i18n.T("project %s"), e.tilde(dir)), ctx, rep); err != nil {
				return changed, err
			}
		}
	}
	body, err := overlayClaudeLocal(o)
	if err != nil {
		return changed, err
	}
	if body != "" {
		c, err := inject.ClaudeLocal(dir, body)
		if err != nil {
			return changed, err
		}
		if c {
			changed = true
			rep.add(&rep.written, "%s/CLAUDE.local.md (%s)", e.tilde(dir), o.Name)
		}
	}
	return changed, nil
}

func expand(e *Env, p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		return filepath.Join(e.Home, strings.TrimPrefix(p, "~"))
	}
	return p
}

func printReport(e *Env, rep *report, backups int) {
	if !e.Interactive {
		parts := []string{fmt.Sprintf(i18n.T("%d written"), len(rep.written))}
		if len(rep.conflicts) > 0 {
			parts = append(parts, fmt.Sprintf(i18n.T("%d conflicts: %s"), len(rep.conflicts), strings.Join(rep.conflicts, "; ")))
		}
		if len(rep.skipped) > 0 {
			parts = append(parts, fmt.Sprintf(i18n.T("%d skipped: %s"), len(rep.skipped), strings.Join(rep.skipped, "; ")))
		}
		e.printf("cclayer: %s\n", strings.Join(parts, ", "))
		return
	}
	for _, n := range rep.notes {
		e.printf("%s\n", n)
	}
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		e.printf("%s\n", title)
		for _, it := range items {
			e.printf("  %s\n", it)
		}
	}
	section(i18n.T("Written:"), rep.written)
	section(i18n.T("Left alone (edit here, layer unchanged):"), rep.skipped)
	section(i18n.T("Conflicts (edited here and in the layer):"), rep.conflicts)
	if len(rep.written) == 0 && len(rep.skipped) == 0 && len(rep.conflicts) == 0 {
		e.printf("Nothing to do.\n")
	}
	if backups > 0 {
		e.printf("%d file(s) backed up under %s\n", backups, e.tilde(filepath.Join(e.State.Path, "backups")))
	}
}
