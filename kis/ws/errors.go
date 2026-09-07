package ws

import (
	"errors"
	"fmt"
)

// KIS message codes this package reacts to.
const (
	// msgCodeSessionOccupied is returned when the app key already holds a
	// WebSocket session somewhere else.
	msgCodeSessionOccupied = "OPSP8996"
	// msgCodeApprovalRejected is returned when the approval key itself was
	// refused and a fresh one may work.
	msgCodeApprovalRejected = "OPSP0011"
)

// reissuableCodes are the rejection codes for which obtaining a new approval
// key is a plausible remedy.
//
// msgCodeSessionOccupied is deliberately absent and must stay absent. That
// code means another live session holds the key, so reissuing only churns
// keys while the real conflict is untouched.
var reissuableCodes = map[string]struct{}{
	msgCodeApprovalRejected: {},
}

func reissuable(msgCode string) bool {
	_, ok := reissuableCodes[msgCode]
	return ok
}

// The protocol error vocabulary of this package is closed and consists of
// exactly these three values. KIS protocol failures beyond them are reported
// as SubscribeError rather than as new exported sentinels.
var (
	// ErrSessionOccupied reports KIS msg_cd OPSP8996: this app key already has
	// a WebSocket session. It is never retried and never triggers an approval
	// reissue. See the package documentation for the break-before-make
	// handover order that avoids it.
	ErrSessionOccupied = errors.New("ws: app key already holds a WebSocket session")

	// ErrApprovalRejected reports that the approval key was refused with a
	// code for which a reissue may help. The connection reissues once and
	// retries; repeated rejection with the same code backs off instead of
	// reissuing again.
	ErrApprovalRejected = errors.New("ws: approval key rejected")

	// ErrSubscribeFailed is the sentinel behind every *SubscribeError, so
	// errors.Is(err, ErrSubscribeFailed) matches any subscribe rejection.
	ErrSubscribeFailed = errors.New("ws: subscribe rejected")
)

// SubscribeError carries the KIS response envelope of a rejected subscribe or
// unsubscribe. It holds only server-supplied protocol fields; no approval key,
// app key, or decryption material ever reaches it.
type SubscribeError struct {
	TR    string
	RTCD  string
	MsgCD string
	Msg1  string
}

func (e *SubscribeError) Error() string {
	return fmt.Sprintf("ws: subscribe rejected (tr=%s, rt_cd=%s, msg_cd=%s): %s", e.TR, e.RTCD, e.MsgCD, e.Msg1)
}

// Unwrap reports ErrSubscribeFailed so callers can match the whole class.
func (e *SubscribeError) Unwrap() error { return ErrSubscribeFailed }
