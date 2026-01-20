package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

type ExecSpec struct {
	Command string
	Args    []string
	Env     []string
	WorkDir string
}

type Process struct {
	cmd    *exec.Cmd
	pty    *os.File
	stdout io.Reader
	done   chan error
	mu     sync.Mutex
}

func (p *Process) Wait() error {
	return <-p.done
}

func (p *Process) OutputReader() io.Reader {
	return p.stdout
}

func (p *Process) Write(input []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pty == nil {
		return 0, fmt.Errorf("pty not available")
	}
	return p.pty.Write(input)
}

func (p *Process) Resize(cols int, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid terminal size")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pty == nil {
		return fmt.Errorf("pty not available")
	}
	return pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *Process) Interrupt(ctx context.Context) error {
	if p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		return err
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		return ctx.Err()
	}
}

func StartProcess(spec ExecSpec) (*Process, error) {
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Env = spec.Env
	cmd.Dir = spec.WorkDir
	ptyFile, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	_ = pty.Setsize(ptyFile, &pty.Winsize{Cols: 120, Rows: 40})
	proc := &Process{
		cmd:    cmd,
		pty:    ptyFile,
		stdout: ptyFile,
		done:   make(chan error, 1),
	}
	go func() {
		err := cmd.Wait()
		proc.done <- err
		close(proc.done)
		_ = ptyFile.Close()
	}()
	return proc, nil
}

func ExitCode(err error) *int {
	if err == nil {
		code := 0
		return &code
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			code := status.ExitStatus()
			return &code
		}
	}
	return nil
}

func BuildEnv(defaults, overrides map[string]string) []string {
	env := map[string]string{}
	for key, value := range defaults {
		env[key] = value
	}
	for key, value := range overrides {
		env[key] = value
	}
	if _, ok := env["PATH"]; !ok {
		env["PATH"] = "/usr/local/bin:/usr/bin:/bin"
	}
	if _, ok := env["TERM"]; !ok {
		env["TERM"] = "xterm-256color"
	}
	result := make([]string, 0, len(env))
	for key, value := range env {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}
