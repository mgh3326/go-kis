package ws_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/internal/testutil"
	"github.com/mgh3326/go-kis/kis/ws"
)

// wireRequest is the subscribe/unsubscribe frame as it appears on the wire.
type wireRequest struct {
	Header struct {
		ApprovalKey string `json:"approval_key"`
		CustType    string `json:"custtype"`
		TRType      string `json:"tr_type"`
		ContentType string `json:"content-type"`
	} `json:"header"`
	Body struct {
		Input struct {
			TRID  string `json:"tr_id"`
			TRKey string `json:"tr_key"`
		} `json:"input"`
	} `json:"body"`
}

type control struct {
	kind int
	data []byte
}

// fakeTransport is an in-memory socket. Frames written by the connection are
// recorded and handed to the server script; frames the script pushes are
// returned by Read in push order.
type fakeTransport struct {
	server *scriptedServer

	in     chan []byte
	closed chan struct{}
	once   sync.Once

	mu       sync.Mutex
	writes   [][]byte
	controls []control
}

func (t *fakeTransport) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closed:
		return nil, io.EOF
	case data := <-t.in:
		return data, nil
	}
}

func (t *fakeTransport) Write(ctx context.Context, data []byte) error {
	select {
	case <-t.closed:
		return io.ErrClosedPipe
	default:
	}
	t.mu.Lock()
	t.writes = append(t.writes, slices.Clone(data))
	t.mu.Unlock()
	t.server.serve(t, data)
	return nil
}

func (t *fakeTransport) WriteControl(ctx context.Context, kind int, data []byte) error {
	select {
	case <-t.closed:
		return io.ErrClosedPipe
	default:
	}
	t.mu.Lock()
	t.controls = append(t.controls, control{kind: kind, data: slices.Clone(data)})
	t.mu.Unlock()
	return nil
}

func (t *fakeTransport) Close() error {
	t.once.Do(func() { close(t.closed) })
	return nil
}

// push queues a frame for the connection to read.
func (t *fakeTransport) push(frame string) {
	select {
	case t.in <- []byte(frame):
	case <-t.closed:
	}
}

// drop simulates the server dropping the socket.
func (t *fakeTransport) drop() { _ = t.Close() }

func (t *fakeTransport) written() []wireRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	requests := make([]wireRequest, 0, len(t.writes))
	for _, raw := range t.writes {
		var request wireRequest
		if json.Unmarshal(raw, &request) == nil {
			requests = append(requests, request)
		}
	}
	return requests
}

func (t *fakeTransport) rawWrites() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.writes)
}

func (t *fakeTransport) sentControls() []control {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.controls)
}

func (t *fakeTransport) queued() int { return len(t.in) }

// scriptedServer is a Dialer plus the server behaviour behind it.
type scriptedServer struct {
	mu     sync.Mutex
	dials  int
	conns  []*fakeTransport
	dialed []string

	// reply decides what the server sends back for one client frame. The
	// default acknowledges every request successfully.
	reply func(conn *fakeTransport, request wireRequest, raw []byte)
}

func newServer() *scriptedServer { return &scriptedServer{} }

func (s *scriptedServer) Dial(ctx context.Context, endpoint string) (ws.Transport, error) {
	conn := &fakeTransport{server: s, in: make(chan []byte, 64), closed: make(chan struct{})}
	s.mu.Lock()
	s.dials++
	s.dialed = append(s.dialed, endpoint)
	s.conns = append(s.conns, conn)
	s.mu.Unlock()
	return conn, nil
}

func (s *scriptedServer) serve(conn *fakeTransport, raw []byte) {
	var request wireRequest
	if json.Unmarshal(raw, &request) != nil {
		return
	}
	s.mu.Lock()
	reply := s.reply
	s.mu.Unlock()
	if reply == nil {
		conn.push(okAck(request.Body.Input.TRID, ""))
		return
	}
	reply(conn, request, raw)
}

func (s *scriptedServer) dialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dials
}

func (s *scriptedServer) endpoints() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.dialed)
}

// conn returns the nth dialled transport, waiting briefly for it to appear.
func (s *scriptedServer) conn(t *testing.T, n int) *fakeTransport {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.conns) > n {
			conn := s.conns[n]
			s.mu.Unlock()
			return conn
		}
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("transport %d was never dialled (dials=%d)", n, s.dialCount())
	return nil
}

func (s *scriptedServer) setReply(reply func(conn *fakeTransport, request wireRequest, raw []byte)) {
	s.mu.Lock()
	s.reply = reply
	s.mu.Unlock()
}

func (s *scriptedServer) latestConn() *fakeTransport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) == 0 {
		return nil
	}
	return s.conns[len(s.conns)-1]
}

// okAck builds a successful acknowledgement, optionally carrying material.
func okAck(tr, output string) string {
	if output == "" {
		output = `{}`
	}
	return `{"header":{"tr_id":"` + tr + `"},"body":{"rt_cd":"0","msg_cd":"MCA00000","msg1":"SUBSCRIBE SUCCESS","output":` + output + `}}`
}

// staticProvider hands out a fixed approval key and counts reissues.
type staticProvider struct {
	mu       sync.Mutex
	key      string
	next     string
	reissues int
}

func (p *staticProvider) ApprovalKey(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.key, nil
}

func (p *staticProvider) Reissue(context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reissues++
	if p.next != "" {
		p.key = p.next
	}
	return p.key, nil
}

func (p *staticProvider) reissueCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reissues
}

// testClock fires every timer immediately and records the requested delay, so
// backoff is deterministic and tests do not actually wait.
type testClock struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (c *testClock) Now() time.Time { return time.Now() }

func (c *testClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.delays = append(c.delays, d)
	c.mu.Unlock()
	fired := make(chan time.Time, 1)
	fired <- time.Now()
	return fired
}

func (c *testClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.delays)
}

// fixture loads a recorded frame body from the shared fixture directory.
func fixture(t *testing.T, name string) string {
	t.Helper()
	exchange, err := testutil.LoadFixture(filepath.Join("..", "..", "internal", "testutil", "fixtures", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return exchange.RawBody
}

// waitEvent returns the next event or fails the test.
func waitEvent(t *testing.T, conn *ws.Conn) ws.Event {
	t.Helper()
	select {
	case event, ok := <-conn.Events():
		if !ok {
			t.Fatal("Events closed while waiting for an event")
		}
		return event
	case <-time.After(3 * time.Second):
		t.Fatalf("no event arrived; stats=%+v", conn.Stats())
	}
	return ws.Event{}
}

// testBackoff removes both growth and jitter so reconnect timing is exact.
func testBackoff() ws.BackoffConfig {
	return ws.BackoffConfig{Min: time.Millisecond, Max: time.Millisecond, Factor: 1, Jitter: -1}
}
