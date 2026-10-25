// Package proc starts the external tools (ffmpeg, ffprobe, vipsthumbnail) the way each OS needs.
package proc

import "os/exec"

// Hide keeps cmd from opening a window of its own: on Windows a program without a console (the installer's
// Vitrine.exe, in the notification area) would otherwise flash a console window for every tool it runs. Elsewhere
// it does nothing. It returns cmd.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	hide(cmd)
	return cmd
}
