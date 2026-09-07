// Package ws implements the Korea Investment & Securities (KIS) realtime
// WebSocket protocol as a pure protocol library.
//
// It carries no account, order, or trading policy. Its whole job is to keep a
// KIS realtime socket alive, translate the wire format into typed events, and
// hand those events to the caller in the order the socket produced them.
//
// # Endpoints
//
// [EndpointLive] and [EndpointVTS] are separate constants precisely so a live
// endpoint cannot be paired with mock credentials by editing one character.
// [Dial] rejects any other endpoint through the shared allowlist in the parent
// package.
//
// # Transport injection
//
// The socket itself is reached through [Dialer]/[Transport]. [NewDialer]
// returns the bundled default, whose adapter file is the only production file
// in this package that imports a WebSocket library at all; see [NewDialer] for
// which one. Callers who need their own transport (an existing pooled
// connection, an in-process test double, a different library) implement
// [Dialer] and pass it in Config.Dialer; nothing else in this package changes.
//
// # Ordering and backpressure
//
// One goroutine reads the socket and one bounded channel publishes events, so
// events reach [Conn.Events] in exactly the order the server sent them. The
// channel is never dropped on the floor: when it is full the reader blocks.
// A consumer that stops draining Events therefore stalls the socket, which
// eventually stalls the server's PINGPONG liveness check and drops the
// connection. Drain Events promptly, or give it a buffer sized for your
// slowest consumer.
//
// # PINGPONG
//
// KIS sends application-level PINGPONG frames. The reader answers each one
// immediately with the identical payload and never publishes it as an event.
//
// # One socket per app key
//
// A KIS app key admits exactly one WebSocket session. Opening a second socket
// with the same key gets the second socket rejected with msg_cd OPSP8996,
// reported here as [ErrSessionOccupied]. Handover between processes is
// therefore break-before-make, in this order:
//
//  1. The outgoing process calls [Conn.Close], which sends tr_type "2"
//     unsubscribe frames for every active subscription and then closes the
//     socket.
//  2. Only after that does the incoming process obtain the same (cached)
//     approval key through its injected [ApprovalKeyProvider], call [Dial],
//     and resubscribe.
//  3. The resulting gap is one resubscribe round trip.
//
// Reversing steps 1 and 2 does not overlap the sessions; it makes the incoming
// process fail with [ErrSessionOccupied]. [ErrSessionOccupied] is never
// retried and never triggers an approval-key reissue, because neither can
// help: the remedy is for the other holder to let go.
package ws
