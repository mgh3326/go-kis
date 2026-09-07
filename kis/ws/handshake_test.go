package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/mgh3326/go-kis/kis/ws"
)

// kisEcho is a minimal KIS-shaped server: it acknowledges every subscribe and
// then pushes one realtime frame. It exists only so the default dialer has a
// real WebSocket handshake to complete.
func kisEcho(t *testing.T, push string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer socket.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		for {
			_, raw, err := socket.Read(ctx)
			if err != nil {
				return
			}
			var request wireRequest
			if json.Unmarshal(raw, &request) != nil {
				continue
			}
			if err := socket.Write(ctx, websocket.MessageText, []byte(okAck(request.Body.Input.TRID, ""))); err != nil {
				return
			}
			if push != "" && request.Header.TRType == "1" {
				if err := socket.Write(ctx, websocket.MessageText, []byte(push)); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func wsAddr(server *httptest.Server) string {
	return "ws://" + strings.TrimPrefix(server.URL, "http://") + "/tryitout"
}

// Test 0: the bundled default dialer completes a real WebSocket handshake
// against the same library's server side and exchanges one frame round trip.
func TestDefaultDialerRealHandshake(t *testing.T) {
	server := kisEcho(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transport, err := ws.NewDialer().Dial(ctx, wsAddr(server))
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	defer transport.Close()

	request := `{"header":{"approval_key":"handshake-key","custtype":"P","tr_type":"1","content-type":"utf-8"},"body":{"input":{"tr_id":"H0STCNT0","tr_key":"005930"}}}`
	if err := transport.Write(ctx, []byte(request)); err != nil {
		t.Fatalf("Write over the real socket: %v", err)
	}
	raw, err := transport.Read(ctx)
	if err != nil {
		t.Fatalf("Read over the real socket: %v", err)
	}
	if want := okAck("H0STCNT0", ""); string(raw) != want {
		t.Fatalf("round trip:\n got=%s\nwant=%s", raw, want)
	}

	// The PINGPONG reply path also has to survive the real socket.
	if err := transport.WriteControl(ctx, ws.PongMessage, []byte(`{"header":{"tr_id":"PINGPONG"}}`)); err != nil {
		t.Fatalf("WriteControl over the real socket: %v", err)
	}
	if err := transport.WriteControl(ctx, 9, nil); err == nil {
		t.Fatal("WriteControl accepted an opcode the adapter cannot emit")
	}
}

// Test 0b: the whole Conn runs over the real transport. The endpoint stays
// allowlisted; only the Dialer is redirected, which is the supported seam.
func TestConnOverRealHandshake(t *testing.T) {
	const frame = "0|H0STCNT0|001|005930^091000^71000^001"
	server := kisEcho(t, frame)

	redirected := ws.DialerFunc(func(ctx context.Context, endpoint string) (ws.Transport, error) {
		// Dial has already applied the KIS allowlist to endpoint; the test
		// substitutes the socket, not the endpoint policy.
		if endpoint != ws.EndpointVTS {
			t.Errorf("dialer received endpoint %q", endpoint)
		}
		return ws.NewDialer().Dial(ctx, wsAddr(server))
	})

	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "handshake-key"}, Dialer: redirected})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe over the real socket: %v", err)
	}
	event := waitEvent(t, conn)
	if event.TR != ws.TRQuotePrice || string(event.Raw) != frame {
		t.Fatalf("event = %+v, want the pushed frame verbatim", event)
	}
}
