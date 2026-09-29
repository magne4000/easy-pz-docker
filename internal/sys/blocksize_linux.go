package sys

import "golang.org/x/sys/unix"

// blockSize is the unit of Statfs block counts. On Linux that is the fragment
// size; Bsize is only the preferred I/O size and differs on some filesystems
// (Docker Desktop's host mounts report 1 MiB against a 4 KiB fragment).
func blockSize(st *unix.Statfs_t) uint64 {
	if st.Frsize > 0 {
		return uint64(st.Frsize)
	}
	return uint64(st.Bsize)
}
