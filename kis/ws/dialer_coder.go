package ws

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// This file is the only production file in this package that imports a
// WebSocket implementation. Everything else in kis/ws is written against
// Transport and Dialer, so replacing the default transport is a one-file
// concern for this package and a one-field concern for callers.

// readLimit bounds a single realtime message. KIS realtime frames are short
// pipe-delimited records; the library default of 32 KiB is generous already,
// and this keeps a misbehaving peer from growing an unbounded buffer.
const readLimit = 1 << 20

// NewDialer returns the default Dialer, built on github.com/coder/websocket.
//
// It is a convenience, not a requirement: Config.Dialer accepts any Dialer.
// The returned dialer does not itself apply the KIS endpoint allowlist, since
// Dial has already applied it; calling it directly with an arbitrary address
// is the caller's own decision.
func NewDialer() Dialer { return coderDialer{} }

type coderDialer struct{}

func (coderDialer) Dial(ctx context.Context, endpoint string) (Transport, error) {
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		// The library error can embed the dialled URL but never credentials;
		// the endpoint is allowlisted and carries no query string.
		return nil, fmt.Errorf("ws: dial failed: %w", err)
	}
	conn.SetReadLimit(readLimit)
	return &coderTransport{conn: conn}, nil
}

type coderTransport struct{ conn *websocket.Conn }

func (t *coderTransport) Read(ctx context.Context) ([]byte, error) {
	_, data, err := t.conn.Read(ctx)
	return data, err
}

func (t *coderTransport) Write(ctx context.Context, data []byte) error {
	return t.conn.Write(ctx, websocket.MessageText, data)
}

// WriteControl answers a KIS PINGPONG.
//
// KIS PINGPONG is an application-level exchange carried in a text frame, and
// the server accepts the identical payload echoed back. The underlying library
// deliberately exposes no way to emit an arbitrary control frame — it owns the
// ping/pong state machine — so this adapter echoes the payload as a text
// message. Transport implementations that can emit a true pong control frame
// are free to do so; the parameter is kept so the protocol intent stays
// expressible across transports.
func (t *coderTransport) WriteControl(ctx context.Context, kind int, data []byte) error {
	if kind != PongMessage {
		return errors.New("ws: unsupported control frame")
	}
	return t.conn.Write(ctx, websocket.MessageText, data)
}

func (t *coderTransport) Close() error {
	return t.conn.Close(websocket.StatusNormalClosure, "")
}
