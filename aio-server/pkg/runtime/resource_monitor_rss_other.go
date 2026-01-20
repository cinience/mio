//go:build !linux && !windows
// +build !linux,!windows

package runtime

func readRSSBytes() (uint64, bool) {
	return 0, false
}
