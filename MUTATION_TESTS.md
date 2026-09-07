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

**M11 — reconnect hook notified before resubscriptions** (`kis/ws/reconnect.go:135`)

`c.notify(ReconnectInfo{...})` after the resubscribe loop → the same notify
call before the loop. This moves the callback ahead of the restore ACKs, which
would let a consumer observe an incomplete subscription set.

`TestReconnectResubscribesInOrder` FAILED by assertion:

```
=== RUN   TestReconnectResubscribesInOrder
    reconnect_test.go:64: frames visible inside OnReconnect = 0, want 3: hook ran before resubscriptions completed
--- FAIL: TestReconnectResubscribesInOrder (0.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	0.454s
```

Reverted; `git status --short` empty.

**M12 — decryption material stored under one fixed transaction key** (`kis/ws/reader.go:231`)

`c.material[tr] = material` → `c.material["*"] = material`. This makes a
valid ACK's material unreachable through its transaction ID, so the VTS
execution frame is discarded instead of decrypted.

`TestExecutionMaterialIsolatedByTransaction` FAILED by assertion:

```
=== RUN   TestExecutionMaterialIsolatedByTransaction
    execution_test.go:142: no event arrived; stats={DroppedFrames:1 LastDropTR:H0STCNI9 LastDropReason:no decryption material}
--- FAIL: TestExecutionMaterialIsolatedByTransaction (3.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	3.452s
```

Reverted; `git status --short` empty.

**M13 — decryption material lookup returns an arbitrary transaction's entry** (`kis/ws/reader.go:238`)

`material, ok := c.material[tr]; return material, ok` → return the first
material encountered while ranging over `c.material`. A frame's transaction
label can then select the wrong AES key/iv.

`TestExecutionMaterialIsolatedByTransaction` FAILED by assertion:

```
=== RUN   TestExecutionMaterialIsolatedByTransaction
    execution_test.go:142: no event arrived; stats={DroppedFrames:1 LastDropTR:H0STCNI9 LastDropReason:ws: encrypted payload is not decryptable}
--- FAIL: TestExecutionMaterialIsolatedByTransaction (3.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	3.467s
```

Reverted; `git status --short` empty.

**M14 — execution field-count guard accepts extra fields** (`kis/ws/execution.go:103`)

`if len(fields) != ExecutionFieldCount {` → `if len(fields) < ExecutionFieldCount {`.
The 15-field record is then silently interpreted as a normal execution.

`TestExecutionRecordWithWrongFieldCountIsRejected` FAILED by assertion:

```
=== RUN   TestExecutionRecordWithWrongFieldCountIsRejected
=== RUN   TestExecutionRecordWithWrongFieldCountIsRejected/13-fields
=== RUN   TestExecutionRecordWithWrongFieldCountIsRejected/15-fields
    execution_test.go:256: Execution = &{OrderNo:0000012345 Symbol:005930 Side:sell Qty:10 Price:71000 FilledAt:091000 Filled:2}, want nil for 15 fields
--- FAIL: TestExecutionRecordWithWrongFieldCountIsRejected (0.00s)
    --- PASS: TestExecutionRecordWithWrongFieldCountIsRejected/13-fields (0.00s)
    --- FAIL: TestExecutionRecordWithWrongFieldCountIsRejected/15-fields (0.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	0.446s
```

Reverted; `git status --short` empty.

**M15 — configured event buffer ignored in favour of the default** (`kis/ws/conn.go:176`)

`buffer := cfg.EventBuffer` → `buffer := defaultEventBuffer`. The one-slot
backpressure test can no longer observe unread frames in the fake socket.

`TestEventBufferCapacityIsApplied` FAILED by assertion:

```
=== RUN   TestEventBufferCapacityIsApplied
    conn_test.go:205: condition was never met
--- FAIL: TestEventBufferCapacityIsApplied (3.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	3.458s
```

Reverted; `git status --short` empty.

**M16 — client approval provider leaks an upstream issue error** (`kis/ws/approval.go:70`)

`return "", errApprovalUnavailable` → `return "", err` in `issue()`. This
would expose the client's upstream transport detail instead of the package's
sanitized approval failure.

`TestClientProviderIssueSanitizesUpstreamError` FAILED by assertion:

```
=== RUN   TestClientProviderIssueSanitizesUpstreamError
    internal_test.go:35: issue error = kis: transport failure (category=other, location=https://openapivts.koreainvestment.com:29443/oauth2/Approval), want errApprovalUnavailable
--- FAIL: TestClientProviderIssueSanitizesUpstreamError (0.00s)
FAIL
FAIL	github.com/mgh3326/go-kis/kis/ws	0.449s
```

Reverted; `git status --short` empty.
