//go:build windows

package state

import "os"

// Windows has no signal 0; FindProcess succeeding is the best cheap answer.
func processAlive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
