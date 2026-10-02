package supervisor

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestChildProcess(t *testing.T) {
	if os.Getenv("SUPERVISOR_TEST_CHILD") != "1" {
		return
	}
	args := os.Args[len(os.Args)-2:]
	ready, mode := args[0], args[1]
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
	if mode == "exit" {
		os.Exit(7)
	}
	for {
		<-ch
		if mode != "ignore" {
			_ = os.WriteFile(ready+".stopped", []byte("stopped"), 0600)
			os.Exit(0)
		}
	}
}

func child(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestChildProcess$", "--", ready, mode)
	cmd.Env = append(os.Environ(), "SUPERVISOR_TEST_CHILD=1")
	return cmd, ready
}

func waitReady(t *testing.T, ready string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not start")
}

func TestCancellationStopsAndReapsAllChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	one, first := child(t, "wait")
	two, second := child(t, "wait")
	done := make(chan error, 1)
	go func() { done <- Run(ctx, time.Second, one, two) }()
	waitReady(t, first)
	waitReady(t, second)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, ready := range []string{first, second} {
		if _, err := os.Stat(ready + ".stopped"); err != nil {
			t.Fatal("SIGTERM not delivered", err)
		}
	}
	if one.ProcessState == nil || two.ProcessState == nil {
		t.Fatal("children not reaped")
	}
}

func TestChildFailureStopsSibling(t *testing.T) {
	one, _ := child(t, "exit")
	two, _ := child(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, time.Second, one, two) }()
	if err := <-done; err == nil {
		t.Fatal("child failure hidden")
	}
	if two.ProcessState == nil {
		t.Fatal("sibling not reaped")
	}
}

func TestStartFailureCleansUpStartedChildren(t *testing.T) {
	one, _ := child(t, "wait")
	err := Run(context.Background(), time.Second, one, exec.Command("/no-such-supervisor-binary"))
	if err == nil {
		t.Fatal("start failure hidden")
	}
	if one.ProcessState == nil {
		t.Fatal("started child not reaped")
	}
}

func TestShutdownKillsUnresponsiveChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	one, ready := child(t, "ignore")
	done := make(chan error, 1)
	go func() { done <- Run(ctx, 50*time.Millisecond, one) }()
	waitReady(t, ready)
	cancel()
	select {
	case <-done:
		if one.ProcessState == nil || one.ProcessState.Success() {
			t.Fatal("unresponsive child not killed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown exceeded deadline")
	}
}
