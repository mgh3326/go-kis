package ws

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// ackMessage is the JSON system message KIS sends for subscribe, unsubscribe,
// and PINGPONG.
type ackMessage struct {
	Header struct {
		TRID string `json:"tr_id"`
	} `json:"header"`
	Body struct {
		RTCD   string `json:"rt_cd"`
		MsgCD  string `json:"msg_cd"`
		Msg1   string `json:"msg1"`
		Output struct {
			IV  string `json:"iv"`
			Key string `json:"key"`
		} `json:"output"`
	} `json:"body"`
}

const trPingPong = "PINGPONG"

// run owns the socket for the life of the connection: it reads until the
// socket fails, then decides whether to reconnect, and closes Events when it
// finally gives up.
func (c *Conn) run() {
	defer close(c.stopped)
	defer close(c.events)

	attempt := 0
	for {
		err := c.readLoop()
		if c.isClosed() {
			return
		}
		if errors.Is(err, ErrSessionOccupied) {
			// Another live session holds this app key. Neither a retry nor a
			// new key can take it from them, so the loop ends here and the
			// caller is told why.
			c.dropTransport()
			c.notify(ReconnectInfo{Attempt: attempt, Stopped: true, Err: ErrSessionOccupied, At: c.clock.Now()})
			return
		}
		c.dropTransport()

		attempt++
		if errors.Is(err, ErrApprovalRejected) && c.markRejection(rejectionCode(err)) {
			// Reissue once per rejection. Repeating it while the server keeps
			// returning the same code only churns keys for no gain.
			c.reissue()
		}

		if !c.sleep(c.backoff.delay(attempt)) {
			return
		}
		generation, err := c.redial()
		if err != nil {
			continue
		}
		// Resubscription waits for ACKs, which only the reader below can
		// deliver, so it runs alongside the reader rather than before it.
		go c.resubscribe(generation, attempt)
	}
}

// readLoop reads one socket to exhaustion. It returns the transport error that
// ended it, or the protocol verdict that made the socket unusable.
func (c *Conn) readLoop() error {
	transport := c.currentTransport()
	if transport == nil {
		return errConnClosed
	}
	for {
		raw, err := transport.Read(c.ctx)
		if err != nil {
			c.failPending(err)
			return err
		}
		if fatal := c.handle(raw); fatal != nil {
			c.failPending(fatal)
			return fatal
		}
	}
}

// handle processes one frame and returns a non-nil error only when the frame
// makes the socket unusable.
func (c *Conn) handle(raw []byte) error {
	if frame, ok := parseRealtime(raw); ok {
		c.publish(frame, raw)
		return nil
	}
	var message ackMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		c.countDrop("", "unrecognised frame")
		return nil
	}
	if message.Header.TRID == trPingPong {
		// Answer liveness immediately and never surface it as an event.
		return c.pong(raw)
	}
	return c.applyAck(message)
}

// pong echoes the PINGPONG payload back. A failure to answer means the socket
// is gone, which is reported so the reconnect path takes over.
func (c *Conn) pong(raw []byte) error {
	transport := c.currentTransport()
	if transport == nil {
		return errConnClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return transport.WriteControl(c.ctx, PongMessage, raw)
}

// applyAck records an ACK's side effects and hands its verdict to whoever is
// waiting for it.
func (c *Conn) applyAck(message ackMessage) error {
	tr := message.Header.TRID
	waiter := c.takePending(tr)
	if tr == "" && waiter != nil {
		// No tr_id on the ACK: it belongs to the oldest outstanding request.
		tr = waiter.tr
	}
	verdict := classifyAck(tr, message.Body.RTCD, message.Body.MsgCD, message.Body.Msg1)
	if verdict == nil {
		c.clearRejection()
		c.storeMaterial(tr, message.Body.Output.Key, message.Body.Output.IV)
	}
	if waiter != nil {
		c.resolve(waiter, verdict)
	}
	// Only a verdict that invalidates the whole session ends the read loop; a
	// refused subscription leaves the socket perfectly usable.
	if errors.Is(verdict, ErrSessionOccupied) || errors.Is(verdict, ErrApprovalRejected) {
		return verdict
	}
	return nil
}

// classifyAck maps a KIS response envelope onto this package's closed error
// vocabulary.
//
// The reissuable set is the single authority on whether a fresh approval key
// could help, so it is consulted before any other verdict. That ordering is
// exactly why msgCodeSessionOccupied must never be added to that set: doing so
// would turn an occupied session into a key-reissue loop.
func classifyAck(tr, rtCD, msgCD, msg1 string) error {
	switch {
	case reissuable(msgCD):
		return approvalRejection(msgCD)
	case msgCD == msgCodeSessionOccupied:
		return ErrSessionOccupied
	case rtCD != "0":
		return &SubscribeError{TR: tr, RTCD: rtCD, MsgCD: msgCD, Msg1: msg1}
	default:
		return nil
	}
}

// approvalRejection carries which code caused a rejection so the loop can tell
// a repeat of the same refusal from a new one.
type approvalRejection string

func (r approvalRejection) Error() string {
	return "ws: approval key rejected (msg_cd=" + string(r) + ")"
}
func (r approvalRejection) Unwrap() error { return ErrApprovalRejected }

func rejectionCode(err error) string {
	var rejection approvalRejection
	if errors.As(err, &rejection) {
		return string(rejection)
	}
	return ""
}

// publish decrypts if needed and hands the event to the consumer, preserving
// arrival order and blocking when the buffer is full.
func (c *Conn) publish(frame realtimeFrame, raw []byte) {
	payload := frame.Payload
	if frame.Encrypted {
		material, ok := c.materialFor(frame.TR)
		if !ok {
			// Without this stream's ACK material there is nothing to try; a
			// guess would only produce plausible-looking garbage.
			c.countDrop(frame.TR, "no decryption material")
			return
		}
		plain, err := material.decrypt(payload)
		if err != nil {
			c.countDrop(frame.TR, err.Error())
			return
		}
		payload = plain
	}
	fields := splitFields(payload)
	event := Event{
		TR:         frame.TR,
		Key:        field(fields, 0),
		Fields:     fields,
		Raw:        slices.Clone(raw),
		ReceivedAt: c.clock.Now(),
	}
	if IsExecution(frame.TR) {
		event.Execution, event.ExecutionErr = parseExecution(fields)
	}
	select {
	case c.events <- event:
	case <-c.done:
	}
}

func (c *Conn) storeMaterial(tr, rawKey, rawIV string) {
	if tr == "" || strings.TrimSpace(rawKey) == "" || strings.TrimSpace(rawIV) == "" {
		return
	}
	material, err := newMaterial(rawKey, rawIV)
	if err != nil {
		c.countDrop(tr, "unusable decryption material")
		return
	}
	c.mu.Lock()
	c.material[tr] = material
	c.mu.Unlock()
}

func (c *Conn) materialFor(tr string) (aesMaterial, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	material, ok := c.material[tr]
	return material, ok
}

func (c *Conn) countDrop(tr, reason string) {
	c.mu.Lock()
	c.stats.DroppedFrames++
	c.stats.LastDropTR = tr
	c.stats.LastDropReason = reason
	c.mu.Unlock()
}
