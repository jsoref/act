package container

import "syscall"

func getSysProcAttr(cmdLine string, tty bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CmdLine: cmdLine}
}
