//go:build unix

package diskfree

import "golang.org/x/sys/unix"

// Of returns the usage of the disk holding path.
func Of(path string) (Usage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	bs := uint64(st.Bsize)
	return Usage{Total: uint64(st.Blocks) * bs, Free: uint64(st.Bavail) * bs}, nil
}
