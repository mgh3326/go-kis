# go-kis

`go-kis` is an unofficial, **read-only** Go protocol client for Korea
Investment & Securities (KIS). It has no order, amendment, or cancellation
API. Trading policy, account scope, and authorization decisions remain the
responsibility of the calling application.

REST clients require one explicit approved HTTPS host: `kis.HostVTS` or
`kis.HostLive`. There is no default host; redirects and proxies are blocked.

| Package | Read API | VTS / live TR IDs |
|---|---|---|
| `kis/domestic` | balance | `VTTC8434R` / `TTTC8434R` |
| `kis/domestic` | order history | `VTTC8001R` / `TTTC8001R` |
| `kis/overseas` | balance | `VTTS3012R` / `TTTS3012R` |
| `kis/overseas` | order history | `VTTS3035R` / `TTTS3035R` |

WebSocket subscription uses KIS's official plaintext `ws://` transport only
for the allowlisted KIS authorities; this is distinct from REST, which is
always HTTPS. Inject a dialer in applications and tests; the library never
selects a user-defined WebSocket authority.

See [examples/balance](examples/balance) for a read-only balance request.

## Realtime WebSocket (`kis/ws`)

`kis/ws` is a pure protocol package for the KIS realtime stream. It has no
account, order, or trading policy of its own: it keeps the socket alive,
turns the wire format into typed events, and hands them to the caller.

```go
conn, err := ws.Dial(ctx, ws.Config{
        Endpoint: ws.EndpointVTS, // or ws.EndpointLive
        Client:   client,         // backs the default approval provider
        Dialer:   ws.NewDialer(),
})
if err != nil {
        return err
}
defer conn.Close()

if err := conn.Subscribe(ctx, ws.TRExecutionVTS, htsID); err != nil {
        return err
}
for event := range conn.Events() {
        if event.Execution != nil {
                fmt.Println(event.Execution.Symbol, event.Execution.Qty, event.Execution.Price)
        }
}
```

| Transaction | Stream |
|---|---|
| `ws.TRExecutionLive` (`H0STCNI0`) | domestic execution notice, live |
| `ws.TRExecutionVTS` (`H0STCNI9`) | domestic execution notice, mock |
| `ws.TRQuotePrice` (`H0STCNT0`) | domestic executed price |
| `ws.TRQuoteBook` (`H0STASP0`) | domestic order book |

Live and mock endpoints are separate constants (`ws.EndpointLive`,
`ws.EndpointVTS`) so the two cannot be crossed by editing one character, and
`Dial` rejects anything outside the shared `kis.ValidateWSURL` allowlist.

**Transport.** The default dialer is
[`github.com/coder/websocket`](https://github.com/coder/websocket) — the
module's only dependency, pinned to an exact version — and it is injectable:
`Config.Dialer` accepts any `ws.Dialer`, so a caller can supply a different
library, a pooled connection, or a test double. Only one production file in
`kis/ws` imports the WebSocket library at all; everything else is written
against the `ws.Transport` interface.

**Encryption.** Execution notices arrive AES-CBC encrypted. The key and IV
come from that transaction's own subscribe acknowledgement and are applied
automatically per transaction. A frame that cannot be decrypted is dropped
with a counted reason (`Conn.Stats`) rather than breaking the connection.

**Ordering and backpressure.** One reader goroutine feeds one bounded channel,
so events arrive in the order the server sent them. When the buffer is full
the reader blocks: a consumer that stops draining `Events()` stalls the socket
rather than silently losing events. Size `Config.EventBuffer` for your slowest
consumer.

**Errors.** The protocol vocabulary is closed: `ws.ErrSessionOccupied`,
`ws.ErrApprovalRejected`, and `*ws.SubscribeError` (matched in bulk with
`errors.Is(err, ws.ErrSubscribeFailed)`).

### Handover between processes: break before make

**One app key admits exactly one WebSocket session.** Opening a second socket
with the same key gets the second one refused with `msg_cd` `OPSP8996`, which
this package reports as `ws.ErrSessionOccupied`. It is never retried and never
triggers an approval-key reissue, because neither can help — the remedy is for
the current holder to let go.

Handover is therefore **break before make**, in this order:

1. The **outgoing** process calls `Conn.Close()`. That sends a `tr_type` `"2"`
   unsubscribe frame for every active subscription and then closes the socket.
2. **Only after that** does the **incoming** process obtain the same cached
   approval key through its injected `ws.ApprovalKeyProvider`, call `ws.Dial`,
   and resubscribe.
3. The resulting gap is one resubscribe round trip.

Reversing steps 1 and 2 does not overlap the sessions; it makes the incoming
process fail with `ws.ErrSessionOccupied`.

Within a single process no handover is needed: after an unexpected drop the
connection redials on a bounded exponential backoff, restores every active
subscription in its original subscribe order, and then calls
`Config.OnReconnect` — the signal to trigger any reconciliation the gap
requires.

See [examples/ws_executions](examples/ws_executions) for a runnable stream.
