# Safety regression record

The repository tests reject an unapproved REST host, HTTP REST, removal of
the 60-second OAuth safety buffer, and removal of the REST limiter wait. The
read-only AST boundary test rejects account-mutation paths, transaction IDs,
and public mutation symbols in production Go source.

## `kis/ws` realtime WebSocket

Four mutants were introduced one at a time, run, and reverted. Each produced a
test assertion failure — not a compile error and not a panic — so each of the
four guarantees is genuinely held up by a test rather than by convention.

**M1 — decryption material misused** (`kis/ws/reader.go:225`)

`material, err := newMaterial(rawKey, rawIV)` → `newMaterial(rawIV, rawKey)`,
swapping the AES key and IV taken from a subscribe acknowledgement.

`TestEncryptedExecutionDecryptedFromAckMaterial` FAILED:

```
--- FAIL: TestEncryptedExecutionDecryptedFromAckMaterial/H0STCNI0 (3.00s)
    execution_test.go:62: no event arrived; stats={DroppedFrames:2 LastDropTR:H0STCNI0 LastDropReason:no decryption material}
--- FAIL: TestEncryptedExecutionDecryptedFromAckMaterial/H0STCNI9 (3.00s)
    execution_test.go:62: no event arrived; stats={DroppedFrames:2 LastDropTR:H0STCNI9 LastDropReason:no decryption material}
```

Reverted; `git status --short` empty.

**M2 — reconnect stops resubscribing** (`kis/ws/reconnect.go:126`)

`err := c.request(ctx, trTypeSubscribe, s.tr, s.key)` → `var err error`, so a
reconnect redials and reports success without restoring any subscription.

`TestReconnectResubscribesInOrder` FAILED:

```
--- FAIL: TestReconnectResubscribesInOrder (0.00s)
    reconnect_test.go:58: restored frames = 0, want 3: []
```

Reverted; `git status --short` empty.

**M3 — OPSP8996 treated as reissuable** (`kis/ws/errors.go:25`)

`msgCodeSessionOccupied: {}` added to `reissuableCodes`, which would make an
occupied session churn approval keys instead of stopping.

`TestSessionOccupiedStopsWithoutRetryOrReissue` and
`TestSessionOccupiedIsNotReissuable` FAILED:

```
--- FAIL: TestSessionOccupiedIsNotReissuable (0.00s)
    internal_test.go:157: OPSP8996 is in the reissuable set; an occupied session would churn approval keys
--- FAIL: TestSessionOccupiedStopsWithoutRetryOrReissue (0.00s)
    reconnect_test.go:109: Subscribe err = ws: approval key rejected (msg_cd=OPSP8996), want ErrSessionOccupied
```

Reverted; `git status --short` empty.

**M4 — event publication reordered** (`kis/ws/reader.go:216`)

The single ordered channel send in `publish` was replaced with a one-frame
hold that emits each pair of events in reverse arrival order.

`TestEventOrderPreserved` FAILED:

```
--- FAIL: TestEventOrderPreserved (0.00s)
    conn_test.go:217: event 0 price = "70001", want "70000" (order was not preserved; got sequence out of step)
```

Reverted; `git status --short` empty.
