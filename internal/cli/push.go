package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// runPush is the upload half of a sync: capture, then commit and push every
// layer repository that has something to send. apply --pull is the other
// half. Directory layers are synced by whatever syncs their folder and are
// left out.
func runPush(e *Env, args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	var add multiFlag
	fs.Var(&add, "add", "admit a device-only file or directory, as with capture; repeatable")
	message := fs.String("m", "", "commit message (default: cclayer: capture from <host>)")
	yes := fs.Bool("yes", false, "commit and push without asking")
	if err := fs.Parse(args); err != nil {
		return usagef("push: %v", err)
	}
	if fs.NArg() > 0 {
		return usagef("usage: cclayer push [--add <path>]... [-m <message>] [--yes]")
	}
	var captureArgs []string
	for _, a := range add {
		captureArgs = append(captureArgs, "--add", a)
	}
	if err := capture(e, captureArgs, false); err != nil {
		return err
	}

	loaded, err := e.Load()
	if err != nil {
		return err
	}
	// the clone may hold edits made by hand, which capture never checked
	findings, err := checkLayers(e, loaded)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		for _, f := range findings {
			e.errorf("%s\n", f)
		}
		return fmt.Errorf("check refused %d item(s); nothing was committed", len(findings))
	}

	type pending struct {
		name, dir string
		dirty     bool
	}
	var todo []pending
	for _, l := range loaded.Layers {
		if !cloned(loaded.Device, l.Name, l.Dir) {
			continue
		}
		clean, err := isClean(l.Dir)
		if err != nil {
			return err
		}
		ahead, _, aerr := aheadBehind(l.Dir)
		if clean && (aerr != nil || ahead == 0) {
			continue
		}
		todo = append(todo, pending{l.Name, l.Dir, !clean})
	}
	if len(todo) == 0 {
		e.printf("push: nothing to push\n")
		return nil
	}

	e.printf("\nTo commit and push:\n")
	for _, p := range todo {
		if !p.dirty {
			e.printf("%s (%s): commits not pushed yet\n", p.name, e.tilde(p.dir))
			continue
		}
		out, err := git(p.dir, "status", "--short")
		if err != nil {
			return err
		}
		e.printf("%s (%s):\n%s\n", p.name, e.tilde(p.dir), indent(out))
	}
	if !*yes && !e.confirm("Commit and push these layers?") {
		return fmt.Errorf("push cancelled; the changes stay in the layer clones")
	}

	msg := *message
	if msg == "" {
		host, _ := os.Hostname()
		msg = "cclayer: capture from " + host
	}
	failed := 0
	var pulled []string
	for _, p := range todo {
		if p.dirty {
			if _, err := git(p.dir, "add", "-A"); err != nil {
				return err
			}
			if _, err := git(p.dir, "commit", "--quiet", "-m", msg); err != nil {
				e.errorf("%s: %v\n", p.name, err)
				failed++
				continue
			}
		}
		// another device may have pushed in the meantime: put this commit on
		// top of theirs, or stop with the clone as it was before the pull
		if _, err := git(p.dir, "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
			before, _ := git(p.dir, "rev-parse", "@{u}")
			// fetch on its own, so a network or credential failure is not
			// reported as a conflict
			if _, err := git(p.dir, "fetch", "--quiet"); err != nil {
				e.errorf("%s: %v\n", p.name, err)
				failed++
				continue
			}
			if _, err := git(p.dir, "rebase", "--quiet", "@{u}"); err != nil {
				if _, aerr := git(p.dir, "rebase", "--abort"); aerr != nil {
					e.errorf("%s: %v\n", p.name, aerr)
				}
				e.printf("%s: the remote has changes that conflict with these; merge them by hand in %s, then run cclayer push again\n", p.name, e.tilde(p.dir))
				failed++
				continue
			}
			if after, _ := git(p.dir, "rev-parse", "@{u}"); after != before {
				pulled = append(pulled, p.name)
			}
		}
		if _, err := git(p.dir, "push", "--quiet"); err != nil {
			if rejected(err) {
				e.printf("%s: the remote moved on while pushing; run cclayer push again\n", p.name)
			} else {
				e.errorf("%s: %v\n", p.name, err)
			}
			failed++
			continue
		}
		e.printf("%s: pushed\n", p.name)
	}
	if len(pulled) > 0 {
		e.printf("newer commits from the remote came along in %s; run cclayer apply to bring them to this device\n", strings.Join(pulled, ", "))
	}
	if failed > 0 {
		return fmt.Errorf("%d layer(s) not pushed", failed)
	}
	return nil
}

// rejected reports whether git refused a push because the remote moved on,
// which a pull fixes, as opposed to a network or permission failure.
func rejected(err error) bool {
	s := err.Error()
	return strings.Contains(s, "[rejected]") || strings.Contains(s, "non-fast-forward") || strings.Contains(s, "fetch first")
}
