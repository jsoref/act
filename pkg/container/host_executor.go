package container

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/nektos/act/pkg/common"
	"github.com/pkg/errors"
)

type HostExecutor struct {
	Path    string
	CleanUp func()
	StdOut  io.Writer
}

func (e *HostExecutor) Create(capAdd []string, capDrop []string) common.Executor {
	return func(ctx context.Context) error {
		return nil
	}
}

func (e *HostExecutor) Copy(destPath string, files ...*FileEntry) common.Executor {
	return func(ctx context.Context) error {
		for _, f := range files {
			os.MkdirAll(filepath.Dir(filepath.Join(destPath, f.Name)), 0777)
			os.WriteFile(filepath.Join(destPath, f.Name), []byte(f.Body), fs.FileMode(f.Mode))
		}
		return nil
	}
}

func (e *HostExecutor) CopyDir(destPath string, srcPath string, useGitIgnore bool) common.Executor {
	return func(ctx context.Context) error {
		return filepath.Walk(srcPath, func(file string, fi os.FileInfo, err error) error {
			if fi.Mode()&os.ModeSymlink != 0 {
				lnk, _ := os.Readlink(file)
				relpath, _ := filepath.Rel(srcPath, file)
				fdestpath := filepath.Join(destPath, relpath)
				os.MkdirAll(filepath.Dir(fdestpath), 0777)
				os.Symlink(lnk, fdestpath)
			} else if fi.Mode().IsRegular() {
				relpath, _ := filepath.Rel(srcPath, file)
				f, _ := os.Open(file)
				defer f.Close()
				fdestpath := filepath.Join(destPath, relpath)
				os.MkdirAll(filepath.Dir(fdestpath), 0777)
				df, _ := os.OpenFile(fdestpath, os.O_CREATE|os.O_WRONLY, fi.Mode())
				defer df.Close()
				io.Copy(df, f)
			}
			return nil
		})
	}
}

func (e *HostExecutor) GetContainerArchive(ctx context.Context, srcPath string) (rc io.ReadCloser, err error) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	defer func() {
		if err == nil {
			err = tw.Close()
		}
	}()
	srcPath = filepath.Clean(srcPath)
	filecbk := func(file string, fi os.FileInfo, err error) error {
		if fi.Mode()&os.ModeSymlink != 0 {
			lnk, _ := os.Readlink(file)
			fih, _ := tar.FileInfoHeader(fi, lnk)
			fih.Name, _ = filepath.Rel(srcPath, file)
			if string(filepath.Separator) != "/" {
				fih.Name = strings.ReplaceAll(fih.Name, string(filepath.Separator), "/")
			}
			tw.WriteHeader(fih)
		} else if fi.Mode().IsRegular() {
			fih, _ := tar.FileInfoHeader(fi, "")
			fih.Name, _ = filepath.Rel(srcPath, file)
			if string(filepath.Separator) != "/" {
				fih.Name = strings.ReplaceAll(fih.Name, string(filepath.Separator), "/")
			}
			tw.WriteHeader(fih)
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()
			io.Copy(tw, f)
		}
		return nil
	}
	fi, err := os.Lstat(srcPath)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		filepath.Walk(srcPath, filecbk)
	} else {
		file := srcPath
		srcPath = filepath.Dir(srcPath)
		if err := filecbk(file, fi, nil); err != nil {
			return nil, err
		}
	}
	return io.NopCloser(buf), nil
}

func (e *HostExecutor) Pull(forcePull bool) common.Executor {
	return func(ctx context.Context) error {
		return nil
	}
}

func (e *HostExecutor) Start(attach bool) common.Executor {
	return func(ctx context.Context) error {
		return nil
	}
}

func (e *HostExecutor) Exec(command []string, cmdline string, env map[string]string, user string) common.Executor {
	return func(ctx context.Context) error {
		envList := make([]string, 0)
		if runtime.GOOS == "windows" && env["PATHEXT"] == "" {
			env["PATHEXT"] = ".COM;.EXE;.BAT;.CMD;.VBS;.VBE;.JS;.JSE;.WSF;.WSH;.MSC;.CPL"
		}
		for k, v := range env {
			envList = append(envList, fmt.Sprintf("%s=%s", k, v))
		}
		oldpath, _ := os.LookupEnv("PATH")
		os.Setenv("PATH", env["PATH"])
		f, _ := exec.LookPath(command[0])
		os.Setenv("PATH", oldpath)
		if len(f) == 0 {
			f, _ = exec.LookPath(command[0])
		}

		if len(f) == 0 {
			err := "Cannot find: " + fmt.Sprint(command[0]) + " in PATH\n"
			e.StdOut.Write([]byte(err))
			return errors.New(err)
		} else {
			cmd := exec.CommandContext(ctx, f)
			cmd.Path = f
			cmd.Args = command
			cmd.Stdin = nil
			cmd.Stdout = e.StdOut
			cmd.Env = envList
			cmd.Stderr = e.StdOut
			cmd.Dir = e.Path
			cmd.SysProcAttr = getSysProcAttr(cmdline, false)
			var err error
			// ttyctx, finishTty := context.WithCancel(context.Background())
			// var ppty *os.File
			{
				// var tty *os.File
				// defer func() {
				// 	if tty != nil {
				// 		tty.Close()
				// 	}
				// }()
				// if containerAllocateTerminal {
				// 	var err error
				// 	ppty, tty, err = pty.Open()
				// 	if err != nil {
				// 		finishTty()
				// 	} else {
				// 		cmd.Stdin = tty
				// 		cmd.Stdout = tty
				// 		cmd.Stderr = tty
				// 		cmd.SysProcAttr = getSysProcAttr(cmdline, true)
				// 		go func() {
				// 			defer finishTty()
				// 			io.Copy(e.StdOut, ppty)
				// 		}()
				// 	}
				// } else {
				// 	finishTty()
				// }
				err = cmd.Start()
			}
			if err == nil {
				err = cmd.Wait()
			}
			// ppty.Close()
			// <-ttyctx.Done()
			if err != nil {
				select {
				case <-ctx.Done():
					e.StdOut.Write([]byte("This step was cancelled\n"))
				default:
				}
				e.StdOut.Write([]byte(err.Error() + "\n"))
			}
			return err
		}
	}
}

var _singleLineEnvPattern *regexp.Regexp
var _mulitiLineEnvPattern *regexp.Regexp

func (e *HostExecutor) UpdateFromEnv(srcPath string, env *map[string]string) common.Executor {
	if _singleLineEnvPattern == nil {
		_singleLineEnvPattern = regexp.MustCompile("^([^=]+)=([^=]+)$")
		_mulitiLineEnvPattern = regexp.MustCompile(`^([^<]+)<<(\w+)$`)
	}

	localEnv := *env
	return func(ctx context.Context) error {
		envTar, err := e.GetContainerArchive(ctx, srcPath)
		if err != nil {
			return nil
		}
		defer envTar.Close()
		reader := tar.NewReader(envTar)
		_, err = reader.Next()
		if err != nil && err != io.EOF {
			return errors.WithStack(err)
		}
		s := bufio.NewScanner(reader)
		multiLineEnvKey := ""
		multiLineEnvDelimiter := ""
		multiLineEnvContent := ""
		for s.Scan() {
			line := s.Text()
			if singleLineEnv := _singleLineEnvPattern.FindStringSubmatch(line); singleLineEnv != nil {
				localEnv[singleLineEnv[1]] = singleLineEnv[2]
			}
			if line == multiLineEnvDelimiter {
				localEnv[multiLineEnvKey] = multiLineEnvContent
				multiLineEnvKey, multiLineEnvDelimiter, multiLineEnvContent = "", "", ""
			}
			if multiLineEnvKey != "" && multiLineEnvDelimiter != "" {
				if multiLineEnvContent != "" {
					multiLineEnvContent += "\n"
				}
				multiLineEnvContent += line
			}
			if mulitiLineEnvStart := _mulitiLineEnvPattern.FindStringSubmatch(line); mulitiLineEnvStart != nil {
				multiLineEnvKey = mulitiLineEnvStart[1]
				multiLineEnvDelimiter = mulitiLineEnvStart[2]
			}
		}
		env = &localEnv
		return nil
	}
}

func (e *HostExecutor) UpdateFromPath(env *map[string]string) common.Executor {
	localEnv := *env
	return func(ctx context.Context) error {
		pathTar, err := e.GetContainerArchive(ctx, localEnv["GITHUB_PATH"])
		if err != nil {
			return errors.WithStack(err)
		}
		defer pathTar.Close()

		reader := tar.NewReader(pathTar)
		_, err = reader.Next()
		if err != nil && err != io.EOF {
			return errors.WithStack(err)
		}
		s := bufio.NewScanner(reader)
		for s.Scan() {
			line := s.Text()
			pathSep := string(filepath.ListSeparator)
			localEnv["PATH"] = fmt.Sprintf("%s%s%s", line, pathSep, localEnv["PATH"])
		}

		env = &localEnv
		return nil
	}
}

func (e *HostExecutor) Remove() common.Executor {
	return func(ctx context.Context) error {
		if e.CleanUp != nil {
			e.CleanUp()
		}
		return os.RemoveAll(e.Path)
	}
}
