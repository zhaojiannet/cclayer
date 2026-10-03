// Package fsx writes files without following symbolic links, so a symlink
// planted in a layer clone or a project working tree cannot redirect a write
// to somewhere else on the device.
package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteFile writes data to path, creating parent directories under root.
// Every path component from root down is checked with Lstat; a symbolic
// link anywhere on the way is an error. root itself may be a symlink (for
// example a home directory on a mounted volume); only what lies below it is
// checked.
func WriteFile(root, path string, data []byte, perm os.FileMode) error {
	if err := refuseSymlinks(root, path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// MkdirAll may have created new components; re-check the final path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link; refusing to write through it", path)
	}
	return os.WriteFile(path, data, perm)
}

// WriteUserFile writes one of the user's own dotfiles (~/.gitconfig,
// ~/.ssh/config, the git excludes file). These are often symlinks kept by a
// dotfiles manager, so a symlink at the final path is followed to its
// target; the target must be an absolute path inside the user's home.
func WriteUserFile(home, path string, data []byte, perm os.FileMode) error {
	target := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		realHome := home
		if h, err := filepath.EvalSymlinks(home); err == nil {
			realHome = h
		}
		if rel, err := filepath.Rel(realHome, resolved); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%s links to %s, outside %s; refusing to write there", path, resolved, home)
		}
		target = resolved
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, perm)
}

// Remove deletes path unless a symlink sits on the way below root.
func Remove(root, path string) error {
	if err := refuseSymlinks(root, path); err != nil {
		return err
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// refuseSymlinks walks from root to path and fails on the first symlink.
func refuseSymlinks(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is not under %s", path, root)
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil // nothing below here exists yet
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link; refusing to write through it", cur)
		}
	}
	return nil
}
