//go:build linux

package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

type ttyBackgroundProcess struct {
	cmd *exec.Cmd
	ptm *os.File

	copyDone chan struct{}
	waitOnce sync.Once
	waitErr  error
}

func startTTYBackgroundProcess(
	ctx context.Context,
	workingDir string,
	env []string,
	blockFuncs []BlockFunc,
	command string,
	stdout io.Writer,
) (backgroundRunner, error) {
	if err := validateCommandAgainstBlockers(command, env, blockFuncs); err != nil {
		return nil, err
	}

	ptm, pts, err := openBackgroundPTY()
	if err != nil {
		return nil, err
	}

	shellPath, err := exec.LookPath("sh")
	if err != nil {
		_ = ptm.Close()
		_ = pts.Close()
		return nil, fmt.Errorf("find shell: %w", err)
	}

	cmd := exec.CommandContext(ctx, shellPath, "-lc", command)
	cmd.Dir = workingDir
	cmd.Env = env
	cmd.Stdin = pts
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	if err := cmd.Start(); err != nil {
		_ = ptm.Close()
		_ = pts.Close()
		return nil, fmt.Errorf("start tty background process: %w", err)
	}

	_ = pts.Close()

	proc := &ttyBackgroundProcess{
		cmd:      cmd,
		ptm:      ptm,
		copyDone: make(chan struct{}),
	}

	go func() {
		defer close(proc.copyDone)
		_, _ = io.Copy(stdout, ptm)
	}()

	go func() {
		<-ctx.Done()
		_ = proc.Terminate(false)
	}()

	return proc, nil
}

func (p *ttyBackgroundProcess) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		_ = p.ptm.Close()
		<-p.copyDone
	})
	return p.waitErr
}

func (p *ttyBackgroundProcess) Terminate(force bool) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}

	return syscall.Kill(-p.cmd.Process.Pid, sig)
}

func openBackgroundPTY() (*os.File, *os.File, error) {
	ptm, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	if err := unlockPTY(ptm); err != nil {
		_ = ptm.Close()
		return nil, nil, err
	}

	ptsName, err := ptsName(ptm)
	if err != nil {
		_ = ptm.Close()
		return nil, nil, err
	}

	pts, err := os.OpenFile(ptsName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = ptm.Close()
		return nil, nil, fmt.Errorf("open %s: %w", ptsName, err)
	}
	return ptm, pts, nil
}

func ptsName(f *os.File) (string, error) {
	var n uintptr
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), unix.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if errno != 0 {
		return "", fmt.Errorf("lookup pts name: %w", errno)
	}
	return fmt.Sprintf("/dev/pts/%d", n), nil
}

func unlockPTY(f *os.File) error {
	var unlock uintptr
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		return fmt.Errorf("unlock pty: %w", errno)
	}
	return nil
}
