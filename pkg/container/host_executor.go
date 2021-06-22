package container

import (
	"archive/tar"
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/docker/docker/pkg/archive"
	"github.com/nektos/act/pkg/common"
	"github.com/pkg/errors"
)

type HostExecutor struct {
	Path string
}

func (e *HostExecutor) Create(capAdd []string, capDrop []string) common.Executor {
	return func(ctx context.Context) error {
		os.MkdirAll(e.Path, 0777)
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
		tarArchive, err := archive.TarWithOptions(srcPath, &archive.TarOptions{
			Compression: archive.Uncompressed,
		})
		if err != nil {
			return err
		}
		os.MkdirAll(destPath, 0777)
		return archive.UntarUncompressed(tarArchive, destPath, &archive.TarOptions{})
	}
}

func (e *HostExecutor) GetContainerArchive(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	return archive.TarWithOptions(srcPath, &archive.TarOptions{
		Compression: archive.Uncompressed,
	})
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
		for k, v := range env {
			envList = append(envList, fmt.Sprintf("%s=%s", k, v))
		}
		oldpath, _ := os.LookupEnv("PATH")
		os.Setenv("PATH", env["PATH"])
		f, _ := exec.LookPath(command[0])
		os.Setenv("PATH", oldpath)

		cmd := &exec.Cmd{
			Path:   f,
			Args:   command,
			Stdin:  nil,
			Stdout: logWriter,
			Env:    envList,
			Stderr: logWriter,
		}
		return cmd.Run()
	}
}

func (e *HostExecutor) UpdateFromEnv(srcPath string, env *map[string]string) common.Executor {
	if singleLineEnvPattern == nil {
		singleLineEnvPattern = regexp.MustCompile("^([^=]+)=([^=]+)$")
		mulitiLineEnvPattern = regexp.MustCompile(`^([^<]+)<<(\w+)$`)
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
			if singleLineEnv := singleLineEnvPattern.FindStringSubmatch(line); singleLineEnv != nil {
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
			if mulitiLineEnvStart := mulitiLineEnvPattern.FindStringSubmatch(line); mulitiLineEnvStart != nil {
				multiLineEnvKey = mulitiLineEnvStart[1]
				multiLineEnvDelimiter = mulitiLineEnvStart[2]
			}
		}
		env = &localEnv
		return nil
	}
}

func (e *HostExecutor) UpdateFromPath(env *map[string]string) common.Executor {
	// return func(ctx context.Context) error {
	// 	return nil
	// }
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
			localEnv["PATH"] = fmt.Sprintf("%s:%s", line, localEnv["PATH"])
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
