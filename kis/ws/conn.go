package ws

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/mgh3326/go-kis/kis"
)

// tr_type values in a subscribe header.
const (
	trTypeSubscribe   = "1"
	trTypeUnsubscribe = "2"
)

// defaultEventBuffer is the capacity of Events when Config.EventBuffer is not
// set. It absorbs a short consumer stall without stalling the socket.
const defaultEventBuffer = 256

// closeTimeout bounds the unsubscribe frames Close sends, so a dead socket
// cannot make Close hang.
const closeTimeout = 5 * time.Second

var (
	errEndpointRequired = errors.New("ws: endpoint is required")
	errDialerRequired   = errors.New("ws: dialer is required")
	errApprovalRequired = errors.New("ws: approval provider or client is required")
	errConnClosed       = errors.New("ws: connection is closed")
)

// Config configures Dial.
type Config struct {
	// Endpoint must be EndpointLive or EndpointVTS. Any other value is
	// rejected by the KIS allowlist.
	Endpoint string
	// Approval supplies the approval key. When set it is used verbatim, which
	// is how a cached key is handed between processes with no REST call.
	Approval ApprovalKeyProvider
	// Client backs the default approval provider when Approval is nil.
	Client *kis.Client
	// Dialer opens the socket. Required; NewDialer returns the default.
	Dialer Dialer
	// EventBuffer is the capacity of the Events channel. Defaults to 256.
	EventBuffer int
	// Backoff paces reconnects. Zero members take documented defaults.
	Backoff BackoffConfig
	// OnReconnect, when set, is called after a reconnect has resubscribed
	// every active subscription, and once more if the loop stops for good. It
	// runs on an internal goroutine and must not block for long.
	OnReconnect func(ReconnectInfo)
	// Clock supplies time. Defaults to the real clock; inject one to make
	// backoff deterministic in tests.
	Clock kis.Clock
}

// ReconnectInfo describes one reconnect outcome.
type ReconnectInfo struct {
	// Attempt counts reconnect attempts since the connection opened.
	Attempt int
	// Subscriptions is the number of subscriptions restored on the new socket.
	Subscriptions int
	// Stopped reports that the reconnect loop has given up; Events is closed
	// and no further reconnect will occur.
	Stopped bool
	// Err is the reason the loop stopped, and is nil on a successful
	// reconnect. It is ErrSessionOccupied when another holder has the app key.
	Err error
	// At is the time the outcome was observed.
	At time.Time
}

// Event is one realtime message.
type Event struct {
	// TR is the transaction ID the frame arrived under.
	TR string
	// Key is the leading field of the record, which for the quote streams is
	// the issue code. It is "" for a frame with no fields.
	Key string
	// Fields are the record's fields in wire order, after decryption.
	Fields []string
	// Execution is set only for TRExecutionLive and TRExecutionVTS.
	Execution *Execution
	// ExecutionErr explains why an execution-notice record was not interpreted.
	// The event and its wire fields are still published when this is non-nil.
	ExecutionErr error
	// Raw is the frame exactly as received, before decryption. It is always
	// populated, so a caller can re-parse or archive anything this package
	// did not model.
	Raw []byte
	// ReceivedAt is when the reader read the frame.
	ReceivedAt time.Time
}

// Stats reports frames the reader discarded instead of publishing. A frame is
// dropped, rather than the connection broken, when it cannot be decrypted.
type Stats struct {
	// DroppedFrames counts frames discarded since the connection opened.
	DroppedFrames int
	// LastDropTR is the transaction ID of the most recently dropped frame.
	LastDropTR string
	// LastDropReason describes why, and never contains key material.
	LastDropReason string
}

// subscription is one active stream, kept in the order it was subscribed so a
// reconnect restores the same order.
type subscription struct{ tr, key string }

// pendingAck is one outstanding request waiting for its ACK.
type pendingAck struct {
	tr string
	ch chan error
}

// Conn is a KIS realtime WebSocket connection. It reconnects and resubscribes
// on its own; see the package documentation for ordering and backpressure.
type Conn struct {
	endpoint    string
	dialer      Dialer
	approval    ApprovalKeyProvider
	backoff     BackoffConfig
	onReconnect func(ReconnectInfo)
	clock       kis.Clock

	events  chan Event
	done    chan struct{}
	stopped chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc

	writeMu sync.Mutex

	mu         sync.Mutex
	transport  Transport
	key        string
	generation int
	subs       []subscription
	material   map[string]aesMaterial
	pending    []*pendingAck
	closed     bool
	stats      Stats
	// reissuedFor is the rejection code the current approval key was already
	// reissued for. It is cleared by the next accepted request, so a rejection
	// after a healthy spell is treated as new rather than as a repeat.
	reissuedFor string

	closeOnce sync.Once
}

// Dial opens a realtime connection and starts reading it.
//
// The returned Conn owns its socket for the rest of its life: ctx bounds only
// the initial approval request and handshake, not the connection.
func Dial(ctx context.Context, cfg Config) (*Conn, error) {
	endpoint, err := kis.ValidateWSURL(cfg.Endpoint)
	if err != nil {
		if cfg.Endpoint == "" {
			return nil, errEndpointRequired
		}
		return nil, err
	}
	if cfg.Dialer == nil {
		return nil, errDialerRequired
	}
	approval := cfg.Approval
	if approval == nil {
		if cfg.Client == nil {
			return nil, errApprovalRequired
		}
		approval = NewClientApprovalProvider(cfg.Client)
	}
	buffer := cfg.EventBuffer
	if buffer <= 0 {
		buffer = defaultEventBuffer
	}
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}

	key, err := approval.ApprovalKey(ctx)
	if err != nil || key == "" {
		return nil, errApprovalUnavailable
	}
	transport, err := cfg.Dialer.Dial(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	c := &Conn{
		endpoint:    endpoint,
		dialer:      cfg.Dialer,
		approval:    approval,
		backoff:     cfg.Backoff.normalized(),
		onReconnect: cfg.OnReconnect,
		clock:       clock,
		events:      make(chan Event, buffer),
		done:        make(chan struct{}),
		stopped:     make(chan struct{}),
		transport:   transport,
		key:         key,
		material:    map[string]aesMaterial{},
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	go c.run()
	return c, nil
}

// Events returns the event stream. It is closed once the connection stops for
// good, whether through Close or through a fatal protocol verdict such as
// ErrSessionOccupied.
//
// The channel is bounded and the reader blocks when it is full, so a consumer
// that stops draining stalls the socket rather than losing events.
func (c *Conn) Events() <-chan Event { return c.events }

// Stats returns a snapshot of the discarded-frame counters.
func (c *Conn) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Subscribe registers tr/key and waits for the server's acknowledgement.
//
// The subscription is remembered and automatically restored, in subscribe
// order, after every reconnect. A rejection whose remedy is a reconnect
// (ErrApprovalRejected) keeps the subscription registered so the reconnect
// restores it; a rejection of the subscription itself (SubscribeError) drops
// it again.
func (c *Conn) Subscribe(ctx context.Context, tr, key string) error {
	return c.request(ctx, trTypeSubscribe, tr, key)
}

// Unsubscribe cancels tr/key with a tr_type "2" frame and stops it from being
// restored on reconnect.
func (c *Conn) Unsubscribe(ctx context.Context, tr, key string) error {
	return c.request(ctx, trTypeUnsubscribe, tr, key)
}

func (c *Conn) request(ctx context.Context, trType, tr, key string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errConnClosed
	}
	transport, approvalKey := c.transport, c.key
	if trType == trTypeSubscribe {
		c.addSubscription(tr, key)
	} else {
		c.removeSubscription(tr, key)
	}
	wait := &pendingAck{tr: tr, ch: make(chan error, 1)}
	c.pending = append(c.pending, wait)
	c.mu.Unlock()

	if err := c.write(ctx, transport, requestFrame(approvalKey, trType, tr, key)); err != nil {
		// The request never reached the server, so no ACK will ever match it.
		c.forget(wait)
		c.dropSubscriptionOnFailure(trType, tr, key)
		return err
	}

	select {
	case err := <-wait.ch:
		if err != nil {
			var rejected *SubscribeError
			if errors.As(err, &rejected) {
				c.dropSubscriptionOnFailure(trType, tr, key)
			}
		}
		return err
	case <-ctx.Done():
		c.forget(wait)
		return ctx.Err()
	case <-c.stopped:
		c.forget(wait)
		return errConnClosed
	}
}

// dropSubscriptionOnFailure removes a subscription the server refused, so a
// later reconnect does not keep re-asking for a stream it will not grant.
func (c *Conn) dropSubscriptionOnFailure(trType, tr, key string) {
	if trType != trTypeSubscribe {
		return
	}
	c.mu.Lock()
	c.removeSubscription(tr, key)
	c.mu.Unlock()
}

// requestFrame builds a subscribe or unsubscribe frame. The two differ only in
// tr_type, so both reuse the parent package's canonical subscribe shape.
func requestFrame(approvalKey, trType, tr, key string) []byte {
	request := kis.NewSubscribe(approvalKey, tr, key)
	request.Header.TRType = trType
	raw, err := json.Marshal(request)
	if err != nil {
		// Subscribe is a fixed struct of strings; marshalling cannot fail.
		return nil
	}
	return raw
}

// write serialises all socket writes, including PINGPONG replies.
func (c *Conn) write(ctx context.Context, transport Transport, data []byte) error {
	if transport == nil {
		return errConnClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return transport.Write(ctx, data)
}

// addSubscription runs under c.mu and keeps subscribe order.
func (c *Conn) addSubscription(tr, key string) {
	for _, s := range c.subs {
		if s.tr == tr && s.key == key {
			return
		}
	}
	c.subs = append(c.subs, subscription{tr: tr, key: key})
}

// removeSubscription runs under c.mu.
func (c *Conn) removeSubscription(tr, key string) {
	c.subs = slices.DeleteFunc(c.subs, func(s subscription) bool {
		return s.tr == tr && s.key == key
	})
}

// Close unsubscribes every active stream and shuts the connection down.
//
// It follows the break-before-make handover order: a tr_type "2" frame goes
// out for each active subscription before the socket is dropped, so the app
// key is free for the next process. Close is idempotent and, once it returns,
// Events is closed.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		subs := slices.Clone(c.subs)
		c.subs = nil
		transport, approvalKey := c.transport, c.key
		c.mu.Unlock()

		if transport != nil {
			ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
			for _, s := range subs {
				// Best effort: the socket may already be gone, and a failed
				// unsubscribe must not stop the remaining ones.
				_ = c.write(ctx, transport, requestFrame(approvalKey, trTypeUnsubscribe, s.tr, s.key))
			}
			cancel()
		}

		close(c.done)
		c.cancel()
		if transport != nil {
			_ = transport.Close()
		}
	})
	<-c.stopped
	return nil
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
