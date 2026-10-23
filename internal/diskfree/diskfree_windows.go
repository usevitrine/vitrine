package diskfree

import "golang.org/x/sys/windows"

// Of returns the usage of the disk holding path.
func Of(path string) (Usage, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Usage{}, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return Usage{}, err
	}
	return Usage{Total: total, Free: free}, nil
}
