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
