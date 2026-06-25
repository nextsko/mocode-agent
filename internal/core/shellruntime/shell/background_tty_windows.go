//go:build windows

package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	conptyCols = 120
	conptyRows = 30
)

// conptyProcess is a Windows background process attached to a ConPTY, so
// interactive commands (prompts, full-screen TUIs) behave as in a real
// terminal. It satisfies backgroundRunner (Wait/Terminate/WriteStdin).
//
// The ConPTY owns the child's console: bytes written to input reach the
// process's stdin; bytes the process writes to stdout/stderr are emitted on
// output and forwarded to the caller.
type conptyProcess struct {
	hpc     windows.Handle // ConPTY handle
	input   *os.File       // write end: bytes we send to the process
	output  *os.File       // read end: bytes the process emits
	pi      windows.ProcessInformation
	exit    chan struct{}
	closeIO sync.Once
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

	// Two pipes: ConPTY reads from pipeIn (what we write) and writes to pipeOut
	// (what we read and forward to stdout).
	pipeInRead, pipeInWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create conpty input pipe: %w", err)
	}
	pipeOutRead, pipeOutWrite, err := os.Pipe()
	if err != nil {
		_ = pipeInRead.Close()
		_ = pipeInWrite.Close()
		return nil, fmt.Errorf("create conpty output pipe: %w", err)
	}

	size := windows.Coord{X: conptyCols, Y: conptyRows}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(size, windows.Handle(pipeInRead.Fd()), windows.Handle(pipeOutWrite.Fd()), 0, &hpc); err != nil {
		_ = pipeInRead.Close()
		_ = pipeInWrite.Close()
		_ = pipeOutRead.Close()
		_ = pipeOutWrite.Close()
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	// The ConPTY now owns the read/write ends it was given; close our copies so
	// EOF propagates when the pipes drain.
	_ = pipeInRead.Close()
	_ = pipeOutWrite.Close()

	pi, cleanup, err := startConPTYChild(hpc, command, workingDir, env)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = pipeInWrite.Close()
		_ = pipeOutRead.Close()
		return nil, fmt.Errorf("start conpty child: %w", err)
	}
	cleanup() // attribute list is only needed during CreateProcess

	proc := &conptyProcess{
		hpc:    hpc,
		input:  pipeInWrite,
		output: pipeOutRead,
		pi:     pi,
		exit:   make(chan struct{}),
	}

	// Forward ConPTY output to the caller's stdout writer.
	go func() {
		_, _ = io.Copy(stdout, pipeOutRead)
	}()

	// Cancel on context cancellation.
	go func() {
		<-ctx.Done()
		_ = proc.Terminate(false)
	}()

	return proc, nil
}

// startConPTYChild launches the command under cmd.exe, attached to the
// ConPTY via a PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE startup attribute. The
// returned cleanup frees the attribute list (which is only needed by
// CreateProcess and can be released immediately after).
func startConPTYChild(hpc windows.Handle, command, workingDir string, env []string) (windows.ProcessInformation, func(), error) {
	noop := func() {}
	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return windows.ProcessInformation{}, noop, fmt.Errorf("allocate proc thread attribute list: %w", err)
	}
	if err := attrList.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		unsafe.Pointer(&hpc),
		unsafe.Sizeof(hpc),
	); err != nil {
		attrList.Delete()
		return windows.ProcessInformation{}, noop, fmt.Errorf("set pseudoconsole attribute: %w", err)
	}

	si := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
		},
		ProcThreadAttributeList: attrList.List(),
	}

	// Build the environment block (UTF-16, double-NUL-terminated) from the
	// supplied env. We construct it ourselves rather than via the token-based
	// CreateEnvironmentBlock, so the child gets exactly this environment.
	var envBlock *uint16
	if len(env) > 0 {
		envBlock, err = newEnvBlockUTF16(env)
		if err != nil {
			attrList.Delete()
			return windows.ProcessInformation{}, noop, fmt.Errorf("create environment block: %w", err)
		}
	}

	cmdLine, err := windows.UTF16PtrFromString("cmd.exe /c " + command)
	if err != nil {
		if envBlock != nil {
			freeEnvBlock(envBlock)
		}
		attrList.Delete()
		return windows.ProcessInformation{}, noop, err
	}
	var cwdPtr *uint16
	if workingDir != "" {
		cwdPtr, err = windows.UTF16PtrFromString(workingDir)
		if err != nil {
			if envBlock != nil {
				freeEnvBlock(envBlock)
			}
			attrList.Delete()
			return windows.ProcessInformation{}, noop, err
		}
	}

	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT) | windows.EXTENDED_STARTUPINFO_PRESENT
	var pi windows.ProcessInformation
	err = windows.CreateProcess(
		nil, cmdLine, nil, nil, false,
		flags, envBlock, cwdPtr, &si.StartupInfo, &pi,
	)
	if envBlock != nil {
		freeEnvBlock(envBlock)
	}
	if err != nil {
		attrList.Delete()
		return windows.ProcessInformation{}, noop, fmt.Errorf("create process: %w", err)
	}

	return pi, attrList.Delete, nil
}

func (p *conptyProcess) Wait() error {
	// Block until the process exits and the output copier has flushed.
	<-p.exit
	return nil
}

func (p *conptyProcess) Terminate(force bool) error {
	// ConPTY does not kill the child on close, so terminate explicitly.
	_ = windows.TerminateProcess(p.pi.Process, 1)
	p.closeIOOnce()
	return nil
}

func (p *conptyProcess) WriteStdin(b []byte) (int, error) {
	if p.input == nil {
		return 0, fmt.Errorf("conpty is closed")
	}
	return p.input.Write(b)
}

// closeIOOnce closes the ConPTY and its pipes exactly once, then signals exit.
func (p *conptyProcess) closeIOOnce() {
	p.closeIO.Do(func() {
		if p.hpc != 0 {
			windows.ClosePseudoConsole(p.hpc)
			p.hpc = 0
		}
		if p.input != nil {
			_ = p.input.Close()
		}
		if p.output != nil {
			_ = p.output.Close()
		}
		// Reap the child handle and signal completion.
		_, _ = windows.WaitForSingleObject(p.pi.Process, windows.INFINITE)
		_ = windows.CloseHandle(p.pi.Process)
		_ = windows.CloseHandle(p.pi.Thread)
		close(p.exit)
	})
}

// newEnvBlockUTF16 builds a Windows-style environment block from a KEY=VALUE list: each entry is UTF-16, NUL-terminated, followed by a final NUL.
func newEnvBlockUTF16(env []string) (*uint16, error) {
	var u16 []uint16
	for _, e := range env {
		s, err := windows.UTF16FromString(e)
		if err != nil {
			return nil, err
		}
		u16 = append(u16, s...)
	}
	u16 = append(u16, 0)
	return &u16[0], nil
}

func freeEnvBlock(_ *uint16) {}
