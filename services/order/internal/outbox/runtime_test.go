package outbox

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeShutdownCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt := &Runtime{
		Brokers: []string{"127.0.0.1:1"}, // invalid — will retry
		Metrics: &Metrics{},
	}
	done := make(chan struct{})
	go func() {
		rt.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not stop")
	}
	if rt.Ready() {
		t.Fatal("should not be ready on bad broker")
	}
}
