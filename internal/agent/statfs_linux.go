package agent

import "syscall"

// diskUsage returns the total and used bytes of the filesystem at path. Like
// df, used is the total minus the blocks that are free (including those
// reserved for root).
func diskUsage(path string) (total, used uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	total = uint64(st.Blocks) * bsize
	used = (uint64(st.Blocks) - uint64(st.Bfree)) * bsize
	return total, used, nil
}
