# go-kis

`go-kis` is an unofficial Go client for Korea Investment & Securities (KIS) REST APIs.

This is a protocol library, not a trading application. It contains **no** host allowlist, mutation gate, account scope, witness, metrics, or trading policy. Those controls are the responsibility of the application that imports it.

Every client must explicitly set a host. There is no default:

```go
client, err := kis.NewClient(kis.Config{Host: kis.HostVTS /* or kis.HostLive */, /* credentials, token provider, timeout */})
```

Requests require a timeout; redirects are always rejected. Token, app key, and app secret values are redacted from KIS API errors. `kis.Mock` and `kis.Live` select the documented TR-ID column independently of the explicit host.

| Package | API | VTS / live TR IDs |
|---|---|---|
| `kis/domestic` | balance | `VTTC8434R` / `TTTC8434R` |
| `kis/domestic` | daily orders | `VTTC8001R` / `TTTC8001R` |
| `kis/domestic` | cash buy, sell, revise/cancel | `VTTC0012U`, `VTTC0011U`, `VTTC0013U` / `TTTC0012U`, `TTTC0011U`, `TTTC0013U` |
| `kis/overseas` | balance, order history | `VTTS3012R`, `VTTS3035R` / `TTTS3012R`, `TTTS3035R` |
| `kis/overseas` | US buy, sell, cancel | `VTTT1002U`, `VTTT1001U`, `VTTT1004U` / `TTTT1002U`, `TTTT1001U`, `TTTT1004U` |

See [examples/balance](examples/balance) for a read-only VTS balance request. Order APIs are available but intentionally have no executable example.
