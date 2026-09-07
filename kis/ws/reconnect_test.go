package ws_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/kis/ws"
)

// reconnectRecorder collects OnReconnect callbacks.
type reconnectRecorder struct {
	mu   sync.Mutex
	seen []ws.ReconnectInfo
}

func (r *reconnectRecorder) record(info ws.ReconnectInfo) {
	r.mu.Lock()
	r.seen = append(r.seen, info)
	r.mu.Unlock()
}

func (r *reconnectRecorder) snapshot() []ws.ReconnectInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ws.ReconnectInfo(nil), r.seen...)
}

// Test 4: a dropped socket is redialled, every active subscription is restored
// in its original subscribe order, and only then is OnReconnect called.
func TestReconnectResubscribesInOrder(t *testing.T) {
	symbols := []string{"005930", "000660", "035720"}
	server := newServer()
	hook := &reconnectRecorder{}
	clock := &testClock{}
	hookFrameCount := make(chan int, 1)
	conn := dialTest(t, ws.Config{
		Approval: &staticProvider{key: "k"},
		Dialer:   server,
		OnReconnect: func(info ws.ReconnectInfo) {
			hook.record(info)
			if restored := server.latestConn(); restored != nil {
				hookFrameCount <- len(restored.written())
			}
		},
		Clock:   clock,
		Backoff: ws.BackoffConfig{Min: 7 * time.Millisecond, Max: time.Second, Factor: 2, Jitter: -1},
	})
	for _, symbol := range symbols {
		if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, symbol); err != nil {
			t.Fatalf("Subscribe %s: %v", symbol, err)
		}
	}

	server.conn(t, 0).drop()

	restored := server.conn(t, 1)
	waitFor(t, func() bool { return len(hook.snapshot()) > 0 })
	select {
	case count := <-hookFrameCount:
		if count != len(symbols) {
			t.Fatalf("frames visible inside OnReconnect = %d, want %d: hook ran before resubscriptions completed", count, len(symbols))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnReconnect did not capture restored frames")
	}

	frames := restored.written()
	if len(frames) != len(symbols) {
		t.Fatalf("restored frames = %d, want %d: %+v", len(frames), len(symbols), frames)
	}
	for i, symbol := range symbols {
		if frames[i].Body.Input.TRKey != symbol {
			t.Fatalf("restored frame %d = %q, want %q (subscribe order was not preserved)", i, frames[i].Body.Input.TRKey, symbol)
		}
		if frames[i].Header.TRType != "1" {
			t.Fatalf("restored frame %d tr_type = %q, want \"1\"", i, frames[i].Header.TRType)
		}
	}

	seen := hook.snapshot()
	info := seen[len(seen)-1]
	if info.Stopped || info.Err != nil {
		t.Fatalf("OnReconnect = %+v, want a successful reconnect", info)
	}
	if info.Subscriptions != len(symbols) {
		t.Fatalf("OnReconnect.Subscriptions = %d, want %d", info.Subscriptions, len(symbols))
	}
	if info.Attempt != 1 {
		t.Fatalf("OnReconnect.Attempt = %d, want 1", info.Attempt)
	}
	if delays := clock.recorded(); len(delays) == 0 || delays[0] != 7*time.Millisecond {
		t.Fatalf("backoff delays = %v, want the configured minimum first", delays)
	}

	// The restored socket is live: a frame on it reaches the consumer.
	restored.push("0|H0STCNT0|001|005930^091000^71000^001")
	if event := waitEvent(t, conn); event.TR != ws.TRQuotePrice {
		t.Fatalf("event after reconnect = %+v", event)
	}
}

// Test 5: OPSP8996 means another session holds the app key. The loop stops
// without a single retry or reissue, the hook is told why, and Events closes.
func TestSessionOccupiedStopsWithoutRetryOrReissue(t *testing.T) {
	server := newServer()
	server.setReply(func(conn *fakeTransport, _ wireRequest, _ []byte) {
		conn.push(fixture(t, "ws-session-occupied.json"))
	})
	provider := &staticProvider{key: "k"}
	hook := &reconnectRecorder{}
	conn := dialTest(t, ws.Config{
		Approval:    provider,
		Dialer:      server,
		OnReconnect: hook.record,
		Clock:       &testClock{},
	})

	err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01")
	if !errors.Is(err, ws.ErrSessionOccupied) {
		t.Fatalf("Subscribe err = %v, want ErrSessionOccupied", err)
	}

	select {
	case _, ok := <-conn.Events():
		if ok {
			t.Fatal("Events delivered an event after the session was refused")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Events was not closed after ErrSessionOccupied")
	}

	if dials := server.dialCount(); dials != 1 {
		t.Fatalf("dials = %d, want 1: ErrSessionOccupied must never be retried", dials)
	}
	if reissues := provider.reissueCount(); reissues != 0 {
		t.Fatalf("Reissue calls = %d, want 0: a new key cannot free an occupied session", reissues)
	}
	seen := hook.snapshot()
	if len(seen) != 1 {
		t.Fatalf("OnReconnect calls = %d, want exactly 1 stop notification: %+v", len(seen), seen)
	}
	if !seen[0].Stopped || !errors.Is(seen[0].Err, ws.ErrSessionOccupied) {
		t.Fatalf("stop notification = %+v, want Stopped with ErrSessionOccupied", seen[0])
	}
}

// Test 6: OPSP0011 is a reissuable rejection. The key is reissued exactly once
// and the connection retries with the new key.
func TestApprovalRejectedReissuesOnceThenRetries(t *testing.T) {
	server := newServer()
	var rejectOnce sync.Once
	server.setReply(func(conn *fakeTransport, request wireRequest, _ []byte) {
		rejected := false
		rejectOnce.Do(func() {
			rejected = true
			conn.push(fixture(t, "ws-approval-rejected.json"))
		})
		if !rejected {
			conn.push(okAck(request.Body.Input.TRID, ""))
		}
	})
	provider := &staticProvider{key: "stale-key", next: "reissued-key"}
	hook := &reconnectRecorder{}
	conn := dialTest(t, ws.Config{
		Approval:    provider,
		Dialer:      server,
		OnReconnect: hook.record,
		Clock:       &testClock{},
	})

	err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01")
	if !errors.Is(err, ws.ErrApprovalRejected) {
		t.Fatalf("Subscribe err = %v, want ErrApprovalRejected", err)
	}

	waitFor(t, func() bool { return len(hook.snapshot()) > 0 })

	if reissues := provider.reissueCount(); reissues != 1 {
		t.Fatalf("Reissue calls = %d, want exactly 1", reissues)
	}
	if dials := server.dialCount(); dials != 2 {
		t.Fatalf("dials = %d, want 2: the rejection must be retried once on a fresh socket", dials)
	}
	frames := server.conn(t, 1).written()
	if len(frames) != 1 {
		t.Fatalf("retry frames = %+v, want the subscription restored once", frames)
	}
	if frames[0].Header.ApprovalKey != "reissued-key" {
		t.Fatalf("retry approval key = %q, want the reissued key", frames[0].Header.ApprovalKey)
	}
	if info := hook.snapshot()[0]; info.Stopped || info.Err != nil {
		t.Fatalf("OnReconnect = %+v, want a successful reconnect", info)
	}
}

// A server that keeps refusing with the same code must not make the client
// churn approval keys: the reissue happens once, then it is pure backoff.
func TestRepeatedApprovalRejectionDoesNotChurnKeys(t *testing.T) {
	server := newServer()
	server.setReply(func(conn *fakeTransport, _ wireRequest, _ []byte) {
		conn.push(fixture(t, "ws-approval-rejected.json"))
	})
	provider := &staticProvider{key: "stale-key"}
	conn := dialTest(t, ws.Config{Approval: provider, Dialer: server, Clock: &testClock{}})

	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); !errors.Is(err, ws.ErrApprovalRejected) {
		t.Fatalf("Subscribe err = %v", err)
	}
	// Let several reconnect rounds run; the fake clock makes them immediate.
	waitFor(t, func() bool { return server.dialCount() >= 4 })

	if reissues := provider.reissueCount(); reissues != 1 {
		t.Fatalf("Reissue calls = %d after %d dials, want exactly 1", reissues, server.dialCount())
	}
}
