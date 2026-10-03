// Package state keeps what cclayer remembers between runs on one device:
// the hashes of files it wrote, which project belongs to which layer, a lock
// against concurrent runs, and backups of files it overwrote.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Dir is the state directory, normally ~/.local/state/cclayer.
type Dir struct{ Path string }

// Applied records what the last apply wrote: target path -> sha256 of the
// content, plus the project-to-layer map.
type Applied struct {
	Hashes   map[string]string `json:"hashes"`
	Projects map[string]string `json:"projects"` // project dir -> layer name
	// Commands records claude commands already run on this device, so plugin
	// installs are not repeated on every apply.
	Commands map[string]bool `json:"commands"`
	// ApprovedCode records files Claude Code may run that were confirmed on
	// this device: path relative to ~/.claude, NUL, sha256 of the content.
	// Only an explicit yes adds an entry, so no other written file can stand
	// in for an approval.
	ApprovedCode map[string]bool `json:"approved_code,omitempty"`
}

// Load reads the applied record; a missing file is an empty record.
func (d Dir) Load() (*Applied, error) {
	a := &Applied{Hashes: map[string]string{}, Projects: map[string]string{}, Commands: map[string]bool{}, ApprovedCode: map[string]bool{}}
	b, err := os.ReadFile(filepath.Join(d.Path, "applied.json"))
	if os.IsNotExist(err) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, a); err != nil {
		return nil, fmt.Errorf("applied.json: %w", err)
	}
	if a.Hashes == nil {
		a.Hashes = map[string]string{}
	}
	if a.Projects == nil {
		a.Projects = map[string]string{}
	}
	if a.ApprovedCode == nil {
		a.ApprovedCode = map[string]bool{}
	}
	if a.Commands == nil {
		a.Commands = map[string]bool{}
	}
	return a, nil
}

// Save writes the applied record atomically.
func (d Dir) Save(a *Applied) error {
	if err := os.MkdirAll(d.Path, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(d.Path, "applied.json.tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(d.Path, "applied.json"))
}

// Hash returns the sha256 of content as hex.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// HashFile hashes a file; a missing file hashes to "".
func HashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return Hash(b), nil
}

// Decision says what apply should do with one target.
type Decision int

const (
	// Unchanged: target already has the wanted content.
	Unchanged Decision = iota
	// Write: target equals what we wrote last time (or never existed), so the
	// layer's new content can replace it.
	Write
	// Conflict: target was edited locally since the last apply and the layer
	// also brings new content. Interactive apply asks; --hook skips.
	Conflict
	// LocalOnly: target was edited locally and the layer is unchanged; leave
	// it for capture.
	LocalOnly
)

// Decide classifies a target given the hash of the current file ("" when
// it does not exist), the hash recorded at the last apply and the hash of
// the wanted content. A file the device deleted after we wrote it counts as
// a local edit: left alone while the layer is unchanged, a conflict when the
// layer moved on.
func Decide(current, lastApplied, wanted string) Decision {
	switch {
	case current == wanted:
		return Unchanged
	case current == lastApplied: // never written, or untouched since
		return Write
	case lastApplied == wanted:
		return LocalOnly
	case lastApplied == "" && current != "":
		return Conflict // pre-existing device file we never wrote
	default:
		return Conflict
	}
}

// ErrLocked is returned when another cclayer run holds the lock.
var ErrLocked = errors.New("another cclayer run holds the lock")

// Lock takes the device lock. The pid is written to a temporary file that
// is then hard-linked into place, so the lock never exists without a pid.
// A lock whose pid no longer runs is taken over; an unreadable lock is
// treated as held.
func (d Dir) Lock() (release func(), err error) {
	if err := os.MkdirAll(d.Path, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(d.Path, "lock")
	tmp := fmt.Sprintf("%s.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	for attempt := 0; attempt < 2; attempt++ {
		if err := os.Link(tmp, path); err == nil {
			return func() { os.Remove(path) }, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, ErrLocked
		}
		pid, perr := strconv.Atoi(trim(string(b)))
		if perr != nil || processAlive(pid) {
			return nil, ErrLocked
		}
		os.Remove(path) // owner is dead
	}
	return nil, ErrLocked
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// Backup copies the current content of path into a run directory before it
// is overwritten. One run directory per cclayer invocation.
type Backup struct {
	root string
	run  string
	n    int
}

// NewBackup prepares a backup session; the run directory is created lazily
// so runs that change nothing leave no trace.
func (d Dir) NewBackup() *Backup {
	return &Backup{root: filepath.Join(d.Path, "backups"), run: time.Now().Format("20060102-150405") + "-" + strconv.Itoa(os.Getpid())}
}

// Save stores the current content of target, if the file exists.
func (b *Backup) Save(target string) error {
	content, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	dir := filepath.Join(b.root, b.run)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b.n++
	name := fmt.Sprintf("%03d-%s", b.n, filepath.Base(target))
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		return err
	}
	// record the original path so a human can put the file back
	return appendLine(filepath.Join(dir, "INDEX"), name+"\t"+target)
}

// Count reports how many files this run has backed up.
func (b *Backup) Count() int { return b.n }

// Prune keeps the newest `keep` run directories plus the oldest one ever
// taken, which is the state before cclayer first touched the device.
func (d Dir) Prune(keep int) error {
	root := filepath.Join(d.Path, "backups")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var runs []string
	for _, e := range entries {
		if e.IsDir() {
			runs = append(runs, e.Name())
		}
	}
	sort.Strings(runs)
	if len(runs) <= keep+1 {
		return nil
	}
	for _, r := range runs[1 : len(runs)-keep] {
		if err := os.RemoveAll(filepath.Join(root, r)); err != nil {
			return err
		}
	}
	return nil
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}
