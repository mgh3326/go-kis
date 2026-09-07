package ws_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/kis"
	"github.com/mgh3326/go-kis/kis/ws"
)

func dialTest(t *testing.T, cfg ws.Config) *ws.Conn {
	t.Helper()
	if cfg.Endpoint == "" {
		cfg.Endpoint = ws.EndpointVTS
	}
	if cfg.Backoff == (ws.BackoffConfig{}) {
		cfg.Backoff = testBackoff()
	}
	conn, err := ws.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// Test 1: the subscribe frame is byte-exact, carries the injected approval
// key, and the acknowledgement is accepted.
func TestSubscribeFrameBytesAndAck(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{
		Approval: &staticProvider{key: "approval-injected"},
		Dialer:   server,
	})

	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	writes := server.conn(t, 0).rawWrites()
	if len(writes) != 1 {
		t.Fatalf("frames written = %d, want 1: %q", len(writes), writes)
	}
	want := `{"header":{"approval_key":"approval-injected","custtype":"P","tr_type":"1","content-type":"utf-8"},"body":{"input":{"tr_id":"H0STCNT0","tr_key":"005930"}}}`
	if got := string(writes[0]); got != want {
		t.Fatalf("subscribe frame:\n got=%s\nwant=%s", got, want)
	}
}

// Test 1b: a rejected subscribe surfaces the envelope and matches the
// ErrSubscribeFailed sentinel without breaking the connection.
func TestSubscribeRejectionCarriesEnvelope(t *testing.T) {
	server := newServer()
	server.setReply(func(conn *fakeTransport, request wireRequest, _ []byte) {
		conn.push(`{"header":{"tr_id":"` + request.Body.Input.TRID + `"},"body":{"rt_cd":"1","msg_cd":"MCA00101","msg1":"INVALID TR KEY","output":{}}}`)
	})
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})

	err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930")
	if !errors.Is(err, ws.ErrSubscribeFailed) {
		t.Fatalf("err = %v, want ErrSubscribeFailed", err)
	}
	var rejected *ws.SubscribeError
	if !errors.As(err, &rejected) {
		t.Fatalf("err = %v, want *SubscribeError", err)
	}
	if rejected.TR != ws.TRQuotePrice || rejected.RTCD != "1" || rejected.MsgCD != "MCA00101" || rejected.Msg1 != "INVALID TR KEY" {
		t.Fatalf("envelope = %+v", rejected)
	}
	if server.dialCount() != 1 {
		t.Fatalf("dials = %d, want 1: a refused subscription must not drop the socket", server.dialCount())
	}
}

// Test 3: PINGPONG is answered with an identical pong control frame and is
// never published as an event.
func TestPingPongAnsweredAndNeverPublished(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})

	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	socket := server.conn(t, 0)
	ping := fixture(t, "ws-pingpong.json")
	socket.push(ping)
	// A data frame pushed after the ping proves the reader got that far.
	socket.push(fixture(t, "ws-market-data.json"))

	event := waitEvent(t, conn)
	if event.TR != ws.TRQuotePrice {
		t.Fatalf("first event TR = %q, want the market frame, not PINGPONG", event.TR)
	}

	controls := socket.sentControls()
	if len(controls) != 1 {
		t.Fatalf("control frames = %d, want exactly 1 pong", len(controls))
	}
	if controls[0].kind != ws.PongMessage {
		t.Fatalf("control opcode = %d, want %d", controls[0].kind, ws.PongMessage)
	}
	if string(controls[0].data) != ping {
		t.Fatalf("pong payload:\n got=%s\nwant=%s", controls[0].data, ping)
	}
}

// Test 7: an injected approval provider means no REST approval request is
// issued at all.
func TestInjectedProviderIssuesNoApprovalRequest(t *testing.T) {
	var approvalCalls atomic.Int64
	client, err := kis.NewClient(kis.Config{
		Host:           kis.HostVTS,
		AppKey:         "test-app-key",
		AppSecret:      "test-app-secret",
		RequestTimeout: time.Second,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/oauth2/Approval" {
				approvalCalls.Add(1)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: newBody(`{"approval_key":"issued-over-rest"}`)}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	server := newServer()
	conn := dialTest(t, ws.Config{
		// Both are supplied: the injected provider must win outright.
		Approval: &staticProvider{key: "handed-over-key"},
		Client:   client,
		Dialer:   server,
	})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if calls := approvalCalls.Load(); calls != 0 {
		t.Fatalf("approval REST calls = %d, want 0", calls)
	}
	frames := server.conn(t, 0).written()
	if len(frames) != 1 || frames[0].Header.ApprovalKey != "handed-over-key" {
		t.Fatalf("frames = %+v, want the injected key on the wire", frames)
	}
}

// Test 7b: without an injected provider the default one uses the client's REST
// approval endpoint, and caches it across reconnects.
func TestDefaultProviderUsesClientApproval(t *testing.T) {
	var approvalCalls atomic.Int64
	client, err := kis.NewClient(kis.Config{
		Host:           kis.HostVTS,
		AppKey:         "test-app-key",
		AppSecret:      "test-app-secret",
		RequestTimeout: time.Second,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/oauth2/Approval" {
				t.Errorf("unexpected REST path %q", r.URL.Path)
			}
			approvalCalls.Add(1)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: newBody(`{"approval_key":"issued-over-rest"}`)}, nil
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	server := newServer()
	conn := dialTest(t, ws.Config{Client: client, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if calls := approvalCalls.Load(); calls != 1 {
		t.Fatalf("approval REST calls = %d, want 1", calls)
	}
	frames := server.conn(t, 0).written()
	if len(frames) != 1 || frames[0].Header.ApprovalKey != "issued-over-rest" {
		t.Fatalf("frames = %+v", frames)
	}
}

// A configured one-slot event buffer must backpressure the reader. The
// unconsumed socket input makes the configured capacity observable without a
// timing guess.
func TestEventBufferCapacityIsApplied(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{
		Approval:    &staticProvider{key: "k"},
		Dialer:      server,
		EventBuffer: 1,
	})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	socket := server.conn(t, 0)
	for i := 0; i < 5; i++ {
		socket.push(fmt.Sprintf("0|H0STCNT0|001|005930^091000^%d^001", 71000+i))
	}
	waitFor(t, func() bool { return len(conn.Events()) == 1 && socket.queued() > 0 }, "EventBuffer=1 did not apply: reader drained the fake socket input")
	if got := len(conn.Events()); got != 1 {
		t.Fatalf("Events capacity = %d, want 1", got)
	}
	if queued := socket.queued(); queued == 0 {
		t.Fatal("reader drained all five frames despite EventBuffer=1")
	}
}

// Provider failures fail Dial and expose only the package-level sanitized
// error, not the request or its synthetic credential detail.
func TestApprovalProviderErrorsAreSanitized(t *testing.T) {
	const upstream = "upstream request secret=synthetic-credential"
	server := newServer()
	_, err := ws.Dial(context.Background(), ws.Config{
		Endpoint: ws.EndpointVTS,
		Approval: approvalErrorProvider{err: errors.New(upstream)},
		Dialer:   server,
	})
	if err == nil || strings.Contains(err.Error(), upstream) {
		t.Fatalf("provider error = %v, want a sanitized Dial failure", err)
	}

	client, err := kis.NewClient(kis.Config{
		Host:           kis.HostVTS,
		AppKey:         "test-app-key",
		AppSecret:      "test-app-secret",
		RequestTimeout: time.Second,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New(upstream)
		})},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = ws.Dial(context.Background(), ws.Config{Endpoint: ws.EndpointVTS, Client: client, Dialer: server})
	if err == nil || strings.Contains(err.Error(), upstream) {
		t.Fatalf("default provider error = %v, want a sanitized Dial failure", err)
	}
}

// Test 8: events reach the consumer in exactly the order the socket produced
// them, including when the bounded buffer is smaller than the burst.
func TestEventOrderPreserved(t *testing.T) {
	const burst = 32
	server := newServer()
	conn := dialTest(t, ws.Config{
		Approval:    &staticProvider{key: "k"},
		Dialer:      server,
		EventBuffer: 1, // force the reader to block on backpressure
	})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	socket := server.conn(t, 0)
	sent := make([]string, 0, burst)
	go func() {
		for i := range burst {
			price := fmt.Sprintf("%05d", 70000+i)
			socket.push("0|H0STCNT0|001|005930^091000^" + price + "^001")
		}
	}()
	for i := range burst {
		sent = append(sent, fmt.Sprintf("%05d", 70000+i))
		event := waitEvent(t, conn)
		if len(event.Fields) < 3 {
			t.Fatalf("event %d fields = %v", i, event.Fields)
		}
		if event.Fields[2] != sent[i] {
			t.Fatalf("event %d price = %q, want %q (order was not preserved; got sequence out of step)", i, event.Fields[2], sent[i])
		}
	}
}

// Test 9: Close unsubscribes every active stream with a tr_type "2" frame
// before dropping the socket, and closes Events.
func TestCloseUnsubscribesActiveStreams(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "handover-key"}, Dialer: server})
	for _, symbol := range []string{"005930", "000660"} {
		if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, symbol); err != nil {
			t.Fatalf("Subscribe %s: %v", symbol, err)
		}
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	frames := server.conn(t, 0).written()
	if len(frames) != 4 {
		t.Fatalf("frames = %d, want 2 subscribes then 2 unsubscribes: %+v", len(frames), frames)
	}
	for i, want := range []struct{ trType, key string }{
		{"1", "005930"}, {"1", "000660"}, {"2", "005930"}, {"2", "000660"},
	} {
		got := frames[i]
		if got.Header.TRType != want.trType || got.Body.Input.TRKey != want.key {
			t.Fatalf("frame %d = tr_type %q key %q, want tr_type %q key %q", i, got.Header.TRType, got.Body.Input.TRKey, want.trType, want.key)
		}
		if got.Header.ApprovalKey != "handover-key" {
			t.Fatalf("frame %d approval key = %q", i, got.Header.ApprovalKey)
		}
	}

	select {
	case _, ok := <-conn.Events():
		if ok {
			t.Fatal("Events delivered after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Events was not closed by Close")
	}

	// Close is idempotent.
	if err := conn.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// Unsubscribe removes a stream so a later reconnect does not restore it.
func TestUnsubscribeStopsRestoration(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server, Clock: &testClock{}})
	for _, symbol := range []string{"005930", "000660"} {
		if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, symbol); err != nil {
			t.Fatalf("Subscribe %s: %v", symbol, err)
		}
	}
	if err := conn.Unsubscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	server.conn(t, 0).drop()
	restored := server.conn(t, 1)
	waitFor(t, func() bool { return len(restored.written()) == 1 })
	frames := restored.written()
	if frames[0].Body.Input.TRKey != "000660" || frames[0].Header.TRType != "1" {
		t.Fatalf("restored = %+v, want only 000660 resubscribed", frames)
	}
}

// AC2: only the two allowlisted endpoints are dialable, and the two constants
// are distinct so live and mock cannot be confused.
func TestEndpointAllowlist(t *testing.T) {
	if ws.EndpointLive == ws.EndpointVTS {
		t.Fatal("live and mock endpoints must be distinct constants")
	}
	for _, endpoint := range []string{ws.EndpointLive, ws.EndpointVTS} {
		if _, err := kis.ValidateWSURL(endpoint); err != nil {
			t.Fatalf("endpoint %s rejected by the shared allowlist: %v", endpoint, err)
		}
	}
	server := newServer()
	for _, endpoint := range []string{
		"",
		"ws://ops.koreainvestment.com:9999/tryitout",
		"wss://ops.koreainvestment.com:21000/tryitout",
		"ws://127.0.0.1:8080/tryitout",
		"ws://ops.koreainvestment.com:21000/tryitout?x=1",
	} {
		conn, err := ws.Dial(context.Background(), ws.Config{Endpoint: endpoint, Approval: &staticProvider{key: "k"}, Dialer: server})
		if err == nil {
			_ = conn.Close()
			t.Fatalf("Dial accepted endpoint %q", endpoint)
		}
	}
	if server.dialCount() != 0 {
		t.Fatalf("dials = %d, want 0: a rejected endpoint must never reach the dialer", server.dialCount())
	}

	conn := dialTest(t, ws.Config{Endpoint: ws.EndpointLive, Approval: &staticProvider{key: "k"}, Dialer: server})
	_ = conn
	if got := server.endpoints(); len(got) != 1 || got[0] != ws.EndpointLive {
		t.Fatalf("dialed = %v, want the live endpoint verbatim", got)
	}
}

// Dial rejects an incomplete configuration rather than guessing.
func TestDialRequiresDialerAndApproval(t *testing.T) {
	if _, err := ws.Dial(context.Background(), ws.Config{Endpoint: ws.EndpointVTS, Approval: &staticProvider{key: "k"}}); err == nil {
		t.Fatal("Dial accepted a nil dialer")
	}
	if _, err := ws.Dial(context.Background(), ws.Config{Endpoint: ws.EndpointVTS, Dialer: newServer()}); err == nil {
		t.Fatal("Dial accepted neither a provider nor a client")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newBody(s string) readCloser { return readCloser{strings.NewReader(s)} }

type readCloser struct{ *strings.Reader }

func (readCloser) Close() error { return nil }

type approvalErrorProvider struct{ err error }

func (p approvalErrorProvider) ApprovalKey(context.Context) (string, error) { return "", p.err }
func (p approvalErrorProvider) Reissue(context.Context) (string, error)     { return "", p.err }

// waitFor polls cond until it holds or the test times out.
func waitFor(t *testing.T, cond func() bool, messages ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if len(messages) > 0 {
		t.Fatal(messages[0])
		return
	}
	t.Fatal("condition was never met")
}
