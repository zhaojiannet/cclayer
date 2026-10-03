// Package mirror makes directories under ~/.claude match the base layer
// without overwriting local edits silently, and copies device changes back
// into the layer for capture.
package mirror

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhaojiannet/cclayer/internal/fsx"
	"github.com/zhaojiannet/cclayer/internal/state"
)

// Action is one planned file operation.
type Action struct {
	Rel      string // path relative to the mirrored root
	Target   string // absolute path under ~/.claude
	Source   string // absolute path in the layer, "" when the layer lacks the file
	Root     string // the mirrored target root; writes never follow symlinks below it
	Decision state.Decision
	Delete   bool // layer lacks the file and the device has it
}

// Plan compares a layer directory with its target. ignore lists relative
// paths (files or directories ending in "/") that apply never deletes and
// capture never adds. lastApplied maps absolute target paths to hashes.
func Plan(layerDir, targetDir string, ignore []string, lastApplied map[string]string) ([]Action, error) {
	layerFiles, err := listFiles(layerDir)
	if err != nil {
		return nil, err
	}
	targetFiles, err := listFiles(targetDir)
	if err != nil {
		return nil, err
	}
	var actions []Action
	for rel, src := range layerFiles {
		if ignored(rel, ignore) {
			continue
		}
		target := filepath.Join(targetDir, rel)
		wantHash, err := state.HashFile(src)
		if err != nil {
			return nil, err
		}
		curHash, err := state.HashFile(target)
		if err != nil {
			return nil, err
		}
		d := state.Decide(curHash, lastApplied[target], wantHash)
		if d == state.Unchanged {
			continue
		}
		actions = append(actions, Action{Rel: rel, Target: target, Source: src, Root: targetDir, Decision: d})
	}
	for rel := range targetFiles {
		if _, inLayer := layerFiles[rel]; inLayer || ignored(rel, ignore) {
			continue
		}
		target := filepath.Join(targetDir, rel)
		curHash, err := state.HashFile(target)
		if err != nil {
			return nil, err
		}
		// A file we wrote earlier that the layer dropped is deleted; a file we
		// never wrote belongs to the device and is a capture candidate.
		if last, ok := lastApplied[target]; ok && last == curHash {
			actions = append(actions, Action{Rel: rel, Target: target, Root: targetDir, Decision: state.Write, Delete: true})
		} else {
			actions = append(actions, Action{Rel: rel, Target: target, Root: targetDir, Decision: state.LocalOnly, Delete: true})
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Rel < actions[j].Rel })
	return actions, nil
}

// Apply executes the Write actions, backing targets up first and recording
// the new hashes. Conflicts and LocalOnly actions are returned untouched for
// the caller to report or resolve.
func Apply(actions []Action, backup *state.Backup, applied *state.Applied) (written []Action, skipped []Action, err error) {
	for _, a := range actions {
		if a.Decision != state.Write {
			skipped = append(skipped, a)
			continue
		}
		if err := backup.Save(a.Target); err != nil {
			return written, skipped, err
		}
		root := a.Root
		if root == "" {
			root = filepath.Dir(a.Target)
		}
		if a.Delete {
			if err := fsx.Remove(root, a.Target); err != nil {
				return written, skipped, err
			}
			delete(applied.Hashes, a.Target)
			written = append(written, a)
			continue
		}
		if err := copyFile(a.Source, a.Target, root); err != nil {
			return written, skipped, err
		}
		h, err := state.HashFile(a.Target)
		if err != nil {
			return written, skipped, err
		}
		applied.Hashes[a.Target] = h
		written = append(written, a)
	}
	return written, skipped, nil
}

// Force writes one conflicting action anyway; interactive apply calls it
// after the user chose the layer's version.
func Force(a Action, backup *state.Backup, applied *state.Applied) error {
	a.Decision = state.Write
	_, _, err := Apply([]Action{a}, backup, applied)
	return err
}

// CaptureOptions steer Capture.
type CaptureOptions struct {
	Ignore      []string
	Admit       []string          // device-only files admitted with --add: relative paths, "dir/" for a directory, "/" for all
	LastApplied map[string]string // absolute target path -> hash written by the last apply
	// Allow vets a file before it enters the layer; nil allows everything.
	Allow func(rel string, content []byte) error
}

// CaptureResult lists what Capture did and did not do.
type CaptureResult struct {
	Updated    []string // written into the layer
	Candidates []string // device-only files, not admitted
	Conflicts  []string // changed on the device and in the layer since the last apply
	Refused    []string // rejected by Allow, with the reason
}

// Capture copies device changes back into the layer directory. A file counts
// as changed on the device only when it differs from what the last apply
// wrote; a layer that moved on at the same time is a conflict, never
// overwritten. Device-only files are listed unless admitted.
func Capture(layerDir, targetDir string, opt CaptureOptions) (CaptureResult, error) {
	var res CaptureResult
	layerFiles, err := listFiles(layerDir)
	if err != nil {
		return res, err
	}
	targetFiles, err := listFiles(targetDir)
	if err != nil {
		return res, err
	}
	admitted := map[string]bool{}
	var admittedDirs []string // "" admits the whole directory
	for _, e := range opt.Admit {
		switch e = filepath.ToSlash(e); {
		case e == "/":
			admittedDirs = append(admittedDirs, "")
		case strings.HasSuffix(e, "/"):
			admittedDirs = append(admittedDirs, e)
		default:
			admitted[e] = true
		}
	}
	isAdmitted := func(rel string) bool {
		if admitted[rel] {
			return true
		}
		for _, d := range admittedDirs {
			if strings.HasPrefix(rel, d) {
				return true
			}
		}
		return false
	}
	for rel, target := range targetFiles {
		if ignored(rel, opt.Ignore) {
			continue
		}
		content, err := os.ReadFile(target)
		if err != nil {
			return res, err
		}
		cur := state.Hash(content)
		src, inLayer := layerFiles[rel]
		if !inLayer {
			if !isAdmitted(rel) {
				res.Candidates = append(res.Candidates, rel)
				continue
			}
		} else {
			layerHash, err := state.HashFile(src)
			if err != nil {
				return res, err
			}
			last := opt.LastApplied[target]
			if cur == layerHash || cur == last {
				continue // nothing changed on the device
			}
			if last != "" && layerHash != last {
				res.Conflicts = append(res.Conflicts, rel)
				continue
			}
		}
		if opt.Allow != nil {
			if err := opt.Allow(rel, content); err != nil {
				res.Refused = append(res.Refused, rel+": "+err.Error())
				continue
			}
		}
		if err := copyFile(target, filepath.Join(layerDir, rel), layerDir); err != nil {
			return res, err
		}
		res.Updated = append(res.Updated, rel)
	}
	sort.Strings(res.Updated)
	sort.Strings(res.Candidates)
	sort.Strings(res.Conflicts)
	sort.Strings(res.Refused)
	return res, nil
}

// listFiles maps relative slash paths to absolute paths for every regular
// file under dir. A missing dir is empty.
func listFiles(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and specials are neither mirrored nor captured
		}
		if osJunk(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = path
		return nil
	})
	return out, err
}

func ignored(rel string, ignore []string) bool {
	for _, ig := range ignore {
		ig = filepath.ToSlash(ig)
		if strings.HasSuffix(ig, "/") {
			if strings.HasPrefix(rel, ig) {
				return true
			}
			continue
		}
		if rel == ig {
			return true
		}
	}
	return false
}

// copyFile copies src to dst without following symlinks below root and
// makes the mode, including the executable bit, follow the source.
func copyFile(src, dst, root string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := fsx.WriteFile(root, dst, b, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}

// osJunk names files the file manager writes on its own. They belong to no
// layer, and listing them as device-only files on every apply is noise.
func osJunk(name string) bool {
	for _, j := range []string{".DS_Store", "Thumbs.db", "desktop.ini"} {
		if strings.EqualFold(name, j) {
			return true
		}
	}
	return false
}
