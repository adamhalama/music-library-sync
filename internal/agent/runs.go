package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

type RunRegistry struct {
	mu   sync.Mutex
	runs map[string]*runState
}

type runState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	lifecycle runLifecycle
	pending   *pendingUIRequest
}

type runLifecycle string

const (
	runLifecycleActive    runLifecycle = "active"
	runLifecycleCanceling runLifecycle = "canceling"
)

type pendingUIRequest struct {
	id       string
	cancelUI func()
}

func NewRunRegistry() *RunRegistry {
	return &RunRegistry{runs: map[string]*runState{}}
}

func (r *RunRegistry) Register(parent context.Context) (string, context.Context, error) {
	if parent == nil {
		parent = context.Background()
	}
	for attempts := 0; attempts < 4; attempts++ {
		id, err := newRunID()
		if err != nil {
			return "", nil, err
		}
		ctx, cancel := context.WithCancel(parent)
		r.mu.Lock()
		if _, exists := r.runs[id]; !exists {
			r.runs[id] = &runState{ctx: ctx, cancel: cancel, lifecycle: runLifecycleActive}
			r.mu.Unlock()
			return id, ctx, nil
		}
		r.mu.Unlock()
		cancel()
	}
	return "", nil, fmt.Errorf("generate unique run id")
}

func (r *RunRegistry) Context(runID string) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return nil, false
	}
	return run.ctx, true
}

func (r *RunRegistry) SetPendingUI(runID, requestID string, cancelUI func()) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return NewRPCError(CodeRunNotFound, "run not found", map[string]string{"run_id": runID})
	}
	if run.pending != nil {
		return NewRPCError(CodeRunConflict, "run already has a pending UI request", map[string]string{"run_id": runID})
	}
	run.pending = &pendingUIRequest{id: requestID, cancelUI: cancelUI}
	return nil
}

func (r *RunRegistry) ClearPendingUI(runID, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run := r.runs[runID]; run != nil && run.pending != nil && run.pending.id == requestID {
		run.pending = nil
	}
}

// Cancel answers a pending frontend request first, then cancels the run
// context. This ordering prevents the worker from remaining blocked on an
// interaction reply channel.
func (r *RunRegistry) Cancel(runID string) bool {
	r.mu.Lock()
	run, ok := r.runs[runID]
	if !ok {
		r.mu.Unlock()
		return false
	}
	var cancelUI func()
	if run.pending != nil {
		cancelUI = run.pending.cancelUI
		run.pending = nil
	}
	cancel := run.cancel
	run.lifecycle = runLifecycleCanceling
	r.mu.Unlock()
	if cancelUI != nil {
		cancelUI()
	}
	cancel()
	return true
}

// Finish removes a run and cancels any remaining resources. It returns false
// after the first finish so callers can emit exactly one terminal notification.
func (r *RunRegistry) Finish(runID string) bool {
	r.mu.Lock()
	run, ok := r.runs[runID]
	if ok {
		delete(r.runs, runID)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	if run.pending != nil && run.pending.cancelUI != nil {
		run.pending.cancelUI()
	}
	run.cancel()
	return true
}

func (r *RunRegistry) CancelAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.runs))
	for id := range r.runs {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.Cancel(id)
	}
}

func (r *RunRegistry) CloseAll() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.runs))
	for id := range r.runs {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.Cancel(id)
		r.Finish(id)
	}
}

func (r *RunRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func newRunID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
