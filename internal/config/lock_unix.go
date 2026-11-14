//go:build unix

package config

import (
	"os"
	"syscall"
)

// lockDir takes an advisory exclusive lock on a directory for as long as the
// returned function has not been called, so that two `regulus config set` at
// the same time do not both read the old file and lose one edit. The
// directory is locked (not a file in it, which the package would own) and a
// failure to lock is not an error: the edit goes on without it.
func lockDir(dir string) (unlock func()) {
	d, err := os.Open(dir)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(d.Fd()), syscall.LOCK_EX); err != nil {
		_ = d.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(d.Fd()), syscall.LOCK_UN)
		_ = d.Close()
	}
}

// ownerOf returns the owner and group of a file, to give them to its
// replacement.
func ownerOf(info os.FileInfo) (uid, gid int, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
