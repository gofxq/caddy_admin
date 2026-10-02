// Package supervisor manages the processes sharing the application container.
package supervisor

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// Run starts all commands. Any exit stops the other processes and returns an
// error so the container's restart policy can recover the whole application.
// Context cancellation gracefully stops all process groups before the deadline.
func Run(ctx context.Context, grace time.Duration, commands ...*exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(commands) == 0 {
		return fmt.Errorf("no processes configured")
	}
	type result struct {
		command *exec.Cmd
		err     error
	}
	done := make(chan result, len(commands))
	active := make(map[*exec.Cmd]bool)
	var cause error
	for _, cmd := range commands {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			cause = fmt.Errorf("start %s: %w", cmd.Path, err)
			break
		}
		active[cmd] = true
		go func() { done <- result{cmd, cmd.Wait()} }()
	}
	if cause == nil {
		select {
		case <-ctx.Done():
		case result := <-done:
			delete(active, result.command)
			// Kill remaining descendants even if their parent exited first.
			_ = syscall.Kill(-result.command.Process.Pid, syscall.SIGTERM)
			cause = fmt.Errorf("%s exited unexpectedly: %v", result.command.Path, result.err)
		}
	}
	for cmd := range active {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	for len(active) > 0 {
		select {
		case result := <-done:
			delete(active, result.command)
		case <-timer.C:
			for cmd := range active {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	}
	return cause
}
