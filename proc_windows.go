//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow keeps helper commands (wsl.exe, netstat…) from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// windowsBuild is the Windows build number: xterm.js adapts to ConPTY's behaviour with it.
func windowsBuild() int {
	_, _, build := windows.RtlGetNtVersionNumbers()
	return int(build)
}
