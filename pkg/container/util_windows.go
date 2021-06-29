package container

import "syscall"

func getSysProcAttr(cmdLine string) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CmdLine: cmdLine}
}
