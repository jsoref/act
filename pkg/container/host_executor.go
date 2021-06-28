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
	Path string
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
				os.MkdirAll(filepath.Base(fdestpath), 0777)
				os.Symlink(lnk, fdestpath)
			} else if fi.Mode().IsRegular() {
				relpath, _ := filepath.Rel(srcPath, file)
				f, _ := os.Open(file)
				defer f.Close()
				fdestpath := filepath.Join(destPath, relpath)
				os.MkdirAll(filepath.Base(fdestpath), 0777)
				df, _ := os.OpenFile(fdestpath, os.O_CREATE|os.O_WRONLY, fi.Mode())
				defer df.Close()
				io.Copy(df, f)
			}
			return nil
		})
	}
}

func (e *HostExecutor) GetContainerArchive(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	filepath.Walk(srcPath, func(file string, fi os.FileInfo, err error) error {
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
			f, _ := os.Open(file)
			defer f.Close()
			io.Copy(tw, f)
		}
		return nil
	})
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

func (e *HostExecutor) Exec(command []string, env map[string]string, user string) common.Executor {
	return func(ctx context.Context) error {
		rawLogger := common.Logger(ctx).WithField("raw_output", true)
		logWriter := common.NewLineWriter(func(s string) bool {
			rawLogger.Infof("%s", s)
			return true
		})
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

		cmd := &exec.Cmd{
			Path:   f,
			Args:   command,
			Stdin:  nil,
			Stdout: logWriter,
			Env:    envList,
			Stderr: logWriter,
			Dir:    e.Path,
		}
		return cmd.Run()
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
		return os.RemoveAll(e.Path)
	}
}
