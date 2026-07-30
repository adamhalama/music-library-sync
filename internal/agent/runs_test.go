package agent

import (
	"context"
	"sync"
	"testing"
)

func TestRunRegistryCancelAnswersUIBeforeContext(t *testing.T) {
	registry := NewRunRegistry()
	runID, ctx, err := registry.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	var mu sync.Mutex
	if err := registry.SetPendingUI(runID, "ui-1", func() {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			t.Error("run context canceled before pending UI reply")
		}
		order = append(order, "ui")
	}); err != nil {
		t.Fatal(err)
	}
	if !registry.Cancel(runID) {
		t.Fatal("expected run cancellation")
	}
	<-ctx.Done()
	mu.Lock()
	order = append(order, "context")
	mu.Unlock()
	if len(order) != 2 || order[0] != "ui" || order[1] != "context" {
		t.Fatalf("unexpected cancellation order: %v", order)
	}
	if !registry.Cancel(runID) {
		t.Fatal("cancellation should be idempotent while the run is registered")
	}
	if !registry.Finish(runID) || registry.Finish(runID) {
		t.Fatal("finish must succeed exactly once")
	}
}

func TestRunRegistryConcurrentLifecycle(t *testing.T) {
	registry := NewRunRegistry()
	const count = 100
	ids := make([]string, count)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			id, _, err := registry.Register(context.Background())
			if err != nil {
				t.Errorf("register: %v", err)
				return
			}
			ids[index] = id
		}(i)
	}
	wg.Wait()
	if registry.Len() != count {
		t.Fatalf("got %d registered runs, want %d", registry.Len(), count)
	}
	for _, id := range ids {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			registry.Cancel(runID)
			registry.Finish(runID)
		}(id)
	}
	wg.Wait()
	if registry.Len() != 0 {
		t.Fatalf("registry leaked %d runs", registry.Len())
	}
}

func TestRunRegistryRejectsOverlappingUIRequests(t *testing.T) {
	registry := NewRunRegistry()
	runID, _, err := registry.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetPendingUI(runID, "one", func() {}); err != nil {
		t.Fatal(err)
	}
	err = registry.SetPendingUI(runID, "two", func() {})
	rpcErr, ok := err.(*RPCError)
	if !ok || rpcErr.Code != CodeRunConflict {
		t.Fatalf("expected run conflict, got %T %v", err, err)
	}
	registry.ClearPendingUI(runID, "one")
	if err := registry.SetPendingUI(runID, "two", func() {}); err != nil {
		t.Fatalf("expected cleared pending request: %v", err)
	}
}
