// +build !windows

package container

import "syscall"

func getSysProcAttr(cmdLine string, tty bool) *syscall.SysProcAttr {
	// if tty {
	// 	return &syscall.SysProcAttr{
	// 		Setsid:  true,
	// 		Setctty: true,
	// 	}
	// }
	return &syscall.SysProcAttr{}
}
