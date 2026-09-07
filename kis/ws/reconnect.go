package ws

import (
	"context"
	"slices"
	"time"
)

// resubscribeTimeout bounds one restored subscription's ACK wait, so a silent
// server cannot pin the resubscribe goroutine forever.
const resubscribeTimeout = 30 * time.Second

// currentTransport returns the socket in use, or nil once the connection has
// been closed.
func (c *Conn) currentTransport() Transport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transport
}

func (c *Conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// dropTransport releases the failed socket before a reconnect, so no write
// lands on a connection the server has already abandoned.
func (c *Conn) dropTransport() {
	c.mu.Lock()
	transport := c.transport
	c.transport = nil
	c.mu.Unlock()
	if transport != nil {
		_ = transport.Close()
	}
}

// sleep waits out a backoff delay, reporting false if the connection was
// closed while waiting.
func (c *Conn) sleep(d time.Duration) bool {
	select {
	case <-c.done:
		return false
	case <-c.clock.After(d):
		return !c.isClosed()
	}
}

// reissue asks the provider for a fresh approval key. A failure here is not
// fatal: the loop keeps backing off and will try again with the key it has.
func (c *Conn) reissue() {
	ctx, cancel := context.WithTimeout(c.ctx, closeTimeout)
	defer cancel()
	key, err := c.approval.Reissue(ctx)
	if err != nil || key == "" {
		return
	}
	c.mu.Lock()
	c.key = key
	c.mu.Unlock()
}

// markRejection records that code caused a rejection and reports whether a
// reissue is warranted. A repeat of the code the current key was already
// reissued for is not.
func (c *Conn) markRejection(code string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reissuedFor == code {
		return false
	}
	c.reissuedFor = code
	return true
}

// clearRejection forgets the last rejection once the server accepts a request
// again, so a later rejection of the same code counts as new.
func (c *Conn) clearRejection() {
	c.mu.Lock()
	c.reissuedFor = ""
	c.mu.Unlock()
}

// redial opens a new socket and returns the generation it was installed as.
// Decryption material is discarded, because the new session issues its own.
func (c *Conn) redial() (int, error) {
	key, err := c.approval.ApprovalKey(c.ctx)
	if err != nil || key == "" {
		return 0, errApprovalUnavailable
	}
	transport, err := c.dialer.Dial(c.ctx, c.endpoint)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = transport.Close()
		return 0, errConnClosed
	}
	c.transport = transport
	c.key = key
	c.material = map[string]aesMaterial{}
	c.generation++
	generation := c.generation
	c.mu.Unlock()
	return generation, nil
}

// resubscribe restores every active subscription on a freshly dialled socket,
// in the order it was originally subscribed, and then reports the reconnect.
//
// It aborts silently if the socket has already been replaced again, so a burst
// of reconnects cannot leave two resubscribe passes interleaving frames.
func (c *Conn) resubscribe(generation, attempt int) {
	c.mu.Lock()
	subs := slices.Clone(c.subs)
	c.mu.Unlock()

	for _, s := range subs {
		if c.generationChanged(generation) {
			return
		}
		ctx, cancel := context.WithTimeout(c.ctx, resubscribeTimeout)
		err := c.request(ctx, trTypeSubscribe, s.tr, s.key)
		cancel()
		if err != nil && c.generationChanged(generation) {
			return
		}
	}
	if c.generationChanged(generation) {
		return
	}
	c.notify(ReconnectInfo{Attempt: attempt, Subscriptions: len(subs), At: c.clock.Now()})
}

func (c *Conn) generationChanged(generation int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed || c.generation != generation
}

func (c *Conn) notify(info ReconnectInfo) {
	if c.onReconnect != nil {
		c.onReconnect(info)
	}
}

// takePending removes the oldest outstanding request matching tr. An ACK with
// no tr_id matches the oldest request of any transaction.
func (c *Conn) takePending(tr string) *pendingAck {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, wait := range c.pending {
		if tr == "" || wait.tr == tr {
			c.pending = slices.Delete(c.pending, i, i+1)
			return wait
		}
	}
	return nil
}

// forget removes an abandoned waiter, e.g. when its caller's context expired.
func (c *Conn) forget(wait *pendingAck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := slices.Index(c.pending, wait); i >= 0 {
		c.pending = slices.Delete(c.pending, i, i+1)
	}
}

// failPending releases every waiter when the socket dies, so no caller is left
// blocked on an ACK that can never arrive.
func (c *Conn) failPending(err error) {
	c.mu.Lock()
	waiters := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, wait := range waiters {
		c.resolve(wait, err)
	}
}

// resolve delivers a verdict. The channel is buffered, so this never blocks
// even if the waiter has already given up.
func (c *Conn) resolve(wait *pendingAck, err error) {
	select {
	case wait.ch <- err:
	default:
	}
}
