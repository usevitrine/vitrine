//go:build !windows

package main

import (
	"fmt"
	"os"
)

// runTray: the notification-area program is Windows only (the installer's Vitrine.exe).
func runTray([]string) int {
	fmt.Fprintln(os.Stderr, "vitrine: this build is for Windows' notification area; use the vitrine command here")
	return 2
}
