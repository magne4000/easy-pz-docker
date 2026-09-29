//go:build !linux

package sys

import "golang.org/x/sys/unix"

// blockSize is the unit of Statfs block counts; outside Linux it is Bsize.
func blockSize(st *unix.Statfs_t) uint64 { return uint64(st.Bsize) }
