package ws

import "context"

// Endpoints. Live and mock are separate constants so the two can never be
// selected by an off-by-one edit of a single shared string.
const (
	// EndpointLive is the KIS production realtime WebSocket endpoint.
	EndpointLive = "ws://ops.koreainvestment.com:21000/tryitout"
	// EndpointVTS is the KIS mock-trading realtime WebSocket endpoint.
	EndpointVTS = "ws://ops.koreainvestment.com:31000/tryitout"
)

// PongMessage is the RFC 6455 pong opcode passed to Transport.WriteControl
// when a KIS PINGPONG frame is answered.
const PongMessage = 10

// Transport is one live WebSocket connection, reduced to what this package
// needs. Read and Write may be called concurrently with each other, but this
// package serialises all writes itself, so an implementation only has to be
// safe for one concurrent reader plus one concurrent writer.
type Transport interface {
	// Read returns the next complete message payload from the socket.
	Read(ctx context.Context) ([]byte, error)
	// Write sends one text message.
	Write(ctx context.Context, data []byte) error
	// WriteControl sends a control frame of the given RFC 6455 opcode.
	WriteControl(ctx context.Context, kind int, data []byte) error
	// Close releases the connection. It must be safe to call more than once.
	Close() error
}

// Dialer opens a Transport. Implementations receive an endpoint that Dial has
// already checked against the KIS allowlist; a Dialer used on its own, outside
// Dial, performs no such check.
type Dialer interface {
	Dial(ctx context.Context, endpoint string) (Transport, error)
}

// DialerFunc adapts a function to Dialer.
type DialerFunc func(ctx context.Context, endpoint string) (Transport, error)

// Dial calls f.
func (f DialerFunc) Dial(ctx context.Context, endpoint string) (Transport, error) {
	return f(ctx, endpoint)
}
