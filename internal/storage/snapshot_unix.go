//go:build unix

package storage

import (
	"os"
	"syscall"
)

// copyOwner gives f the owner and group of info, when that is allowed (root).
func copyOwner(f *os.File, info os.FileInfo) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
}

// matchOwner gives the write-ahead log and the shared-memory file of the
// database at path the owner of the database. A process running as root that
// opens a database nobody has open creates them as root, and the hub, which
// is not root, could not open its own database afterwards.
func matchOwner(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			_ = os.Lchown(path+suffix, int(st.Uid), int(st.Gid))
		}
	}
}
