// +build !windows,!plan9

package container

import (
	"os"
	"syscall"

	"github.com/creack/pty"
)

func getSysProcAttr(cmdLine string, tty bool) *syscall.SysProcAttr {
	if tty {
		return &syscall.SysProcAttr{
			Setsid:  true,
			Setctty: true,
		}
	}
	return &syscall.SysProcAttr{}
}

func openPty() (*os.File, *os.File, error) {
	return pty.Open()
}
