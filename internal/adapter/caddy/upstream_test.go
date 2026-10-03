package caddy

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestTCPUpstreamConnectsAndReportsFailureWithoutRawErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			connection.Close()
		}
		close(done)
	}()
	probe := &TCPUpstreamProbe{}
	result := probe.Check(context.Background(), listener.Addr().String())
	if result.Status != "reachable" || result.DurationMS < 0 {
		t.Fatalf("reachable=%+v", result)
	}
	<-done
	listener.Close()
	result = probe.Check(context.Background(), listener.Addr().String())
	if result.Status != "unreachable" || strings.Contains(result.Message, "dial tcp") {
		t.Fatalf("failed=%+v", result)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result = probe.Check(ctx, listener.Addr().String())
	if result.Status != "unknown" {
		t.Fatalf("canceled=%+v", result)
	}
}
