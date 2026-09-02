package setup

import (
	"context"
	"testing"
)

func TestTurnCancelRegistryStopAfterDetach(t *testing.T) {
	var reg turnCancelRegistry
	ctx, cancel := context.WithCancel(context.Background())
	h := &turnCancel{cancel: cancel}
	key := turnCancelKey("u1", "agt", "s1")
	reg.register(key, h)

	if !reg.cancel(key) {
		t.Fatal("expected an in-flight turn to be stoppable")
	}
	if ctx.Err() == nil {
		t.Fatal("cancel should stop the detached agent context")
	}

	reg.unregister(key, h)
	if reg.cancel(key) {
		t.Fatal("unregister should drop the stop handle")
	}
}

func TestTurnCancelRegistryUnregisterIgnoresReplacement(t *testing.T) {
	var reg turnCancelRegistry
	key := turnCancelKey("u1", "agt", "s1")
	old := &turnCancel{cancel: func() {}}
	newer, newerCancel := context.WithCancel(context.Background())
	next := &turnCancel{cancel: newerCancel}
	reg.register(key, old)
	reg.register(key, next)
	reg.unregister(key, old)
	if !reg.cancel(key) {
		t.Fatal("old unregister must not drop the replacement handle")
	}
	if newer.Err() == nil {
		t.Fatal("replacement handle should still cancel")
	}
}
