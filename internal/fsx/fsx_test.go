package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileRefusesSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// final component is a symlink
	os.Symlink(filepath.Join(outside, "target"), filepath.Join(root, "link"))
	if err := WriteFile(root, filepath.Join(root, "link"), []byte("x"), 0o644); err == nil {
		t.Fatal("write through a symlinked file must fail")
	}
	if _, err := os.Stat(filepath.Join(outside, "target")); err == nil {
		t.Fatal("outside file was created")
	}
	// a parent directory is a symlink
	os.Symlink(outside, filepath.Join(root, "dir"))
	if err := WriteFile(root, filepath.Join(root, "dir", "f"), []byte("x"), 0o644); err == nil {
		t.Fatal("write through a symlinked directory must fail")
	}
	// normal nested write works
	if err := WriteFile(root, filepath.Join(root, "a", "b", "f"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	// outside root is refused
	if err := WriteFile(root, filepath.Join(outside, "g"), []byte("x"), 0o644); err == nil {
		t.Fatal("write outside root must fail")
	}
	if err := Remove(root, filepath.Join(root, "link")); err == nil {
		t.Fatal("removing through a symlink path must be refused")
	}
}
