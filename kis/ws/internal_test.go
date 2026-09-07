package ws

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

// Material arrives either literally or base64-encoded, and the literal reading
// wins when it already has an accepted length.
func TestDecodeMaterial(t *testing.T) {
	literal24 := "SAMPLEKEYSAMPLEKEYSAMPLE" // valid base64 text and a valid 24-byte key
	for _, testCase := range []struct {
		name string
		raw  string
		want []int
		out  string
		fail bool
	}{
		{name: "literal 16-byte iv", raw: "SAMPLE-IV-000000", want: ivLengths, out: "SAMPLE-IV-000000"},
		{name: "literal 32-byte key", raw: "AES-SAMPLE-AES-SAMPLE-AES-SAMPLE", want: keyLengths, out: "AES-SAMPLE-AES-SAMPLE-AES-SAMPLE"},
		{name: "literal 24-byte key beats its base64 reading", raw: literal24, want: keyLengths, out: literal24},
		{name: "base64 16-byte iv", raw: base64.StdEncoding.EncodeToString([]byte("SAMPLE-IV-000001")), want: ivLengths, out: "SAMPLE-IV-000001"},
		{name: "wrong length", raw: "too-short", want: ivLengths, fail: true},
		{name: "base64 of a wrong length", raw: base64.StdEncoding.EncodeToString([]byte("nine char")), want: ivLengths, fail: true},
		{name: "not base64 either", raw: "!!!!!!!!!!!!!!!!!", want: ivLengths, fail: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := decodeMaterial(testCase.raw, testCase.want)
			if testCase.fail {
				if !errors.Is(err, errBadMaterial) {
					t.Fatalf("err = %v, want errBadMaterial", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if string(got) != testCase.out {
				t.Fatalf("got %q, want %q", got, testCase.out)
			}
		})
	}
}

// A frame is only a realtime frame when it has the documented shape.
func TestParseRealtime(t *testing.T) {
	for _, testCase := range []struct {
		name string
		raw  string
		ok   bool
		want realtimeFrame
	}{
		{
			name: "plaintext",
			raw:  "0|H0STCNT0|001|005930^091000^71000^001",
			ok:   true,
			want: realtimeFrame{Encrypted: false, TR: "H0STCNT0", Count: 1, Payload: "005930^091000^71000^001"},
		},
		{
			name: "encrypted",
			raw:  "1|H0STCNI0|001|Y2lwaGVy",
			ok:   true,
			want: realtimeFrame{Encrypted: true, TR: "H0STCNI0", Count: 1, Payload: "Y2lwaGVy"},
		},
		{
			name: "payload keeps its own pipes",
			raw:  "0|H0STASP0|002|a|b|c",
			ok:   true,
			want: realtimeFrame{TR: "H0STASP0", Count: 2, Payload: "a|b|c"},
		},
		{name: "system message", raw: `{"header":{"tr_id":"PINGPONG"}}`},
		{name: "too few segments", raw: "0|H0STCNT0|001"},
		{name: "unknown encryption flag", raw: "2|H0STCNT0|001|x"},
		{name: "non-numeric count", raw: "0|H0STCNT0|abc|x"},
		{name: "empty transaction", raw: "0||001|x"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := parseRealtime([]byte(testCase.raw))
			if ok != testCase.ok {
				t.Fatalf("ok = %v, want %v", ok, testCase.ok)
			}
			if ok && got != testCase.want {
				t.Fatalf("got %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// Fields split on '^' when present and on '|' otherwise, dropping the empty
// segments a pipe-separated payload leaves behind.
func TestSplitFields(t *testing.T) {
	if got := splitFields("a^b^^c"); !equal(got, []string{"a", "b", "", "c"}) {
		t.Fatalf("caret split = %v", got)
	}
	if got := splitFields("a|b||c"); !equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("pipe split = %v", got)
	}
	if got := splitFields(""); len(got) != 0 {
		t.Fatalf("empty payload = %v", got)
	}
	if got := field([]string{"a"}, 5); got != "" {
		t.Fatalf("out-of-range field = %q, want \"\"", got)
	}
}

// A corrupt ciphertext is reported, never returned as plausible plaintext.
func TestDecryptRejectsBadInput(t *testing.T) {
	material, err := newMaterial("AES-SAMPLE-AES-SAMPLE-AES-SAMPLE", "SAMPLE-IV-000000")
	if err != nil {
		t.Fatalf("newMaterial: %v", err)
	}
	for _, testCase := range []struct{ name, payload string }{
		{"not base64", "!!!not-base64!!!"},
		{"not a block multiple", base64.StdEncoding.EncodeToString([]byte("short"))},
		{"bad padding", base64.StdEncoding.EncodeToString(make([]byte, 32))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if plain, err := material.decrypt(testCase.payload); err == nil {
				t.Fatalf("decrypt accepted %q -> %q", testCase.payload, plain)
			}
		})
	}
}

// Backoff grows, is capped, and never returns a delay below the minimum.
func TestBackoffBounded(t *testing.T) {
	backoff := BackoffConfig{Min: 10 * time.Millisecond, Max: 80 * time.Millisecond, Factor: 2, Jitter: -1}.normalized()
	want := []time.Duration{10, 20, 40, 80, 80, 80}
	for i, expected := range want {
		if got := backoff.delay(i + 1); got != expected*time.Millisecond {
			t.Fatalf("delay(%d) = %v, want %v", i+1, got, expected*time.Millisecond)
		}
	}
	if got := backoff.delay(1000); got != 80*time.Millisecond {
		t.Fatalf("delay(1000) = %v, want the cap", got)
	}

	defaults := BackoffConfig{}.normalized()
	if defaults.Max != defaultBackoffMax || defaults.Min != defaultBackoffMin {
		t.Fatalf("defaults = %+v", defaults)
	}
	jittered := BackoffConfig{Min: 100 * time.Millisecond, Max: time.Second, Factor: 2, Jitter: 0.5}
	for attempt := 1; attempt < 10; attempt++ {
		got := jittered.normalized().delay(attempt)
		if got < 100*time.Millisecond || got > time.Second {
			t.Fatalf("jittered delay(%d) = %v, outside [Min, Max]", attempt, got)
		}
	}
}

// OPSP8996 must never be reachable through the reissuable set: that is what
// keeps an occupied session from turning into an approval-key churn loop.
func TestSessionOccupiedIsNotReissuable(t *testing.T) {
	if reissuable(msgCodeSessionOccupied) {
		t.Fatalf("%s is in the reissuable set; an occupied session would churn approval keys", msgCodeSessionOccupied)
	}
	if !reissuable(msgCodeApprovalRejected) {
		t.Fatalf("%s should be reissuable", msgCodeApprovalRejected)
	}
	if got := classifyAck("H0STCNI0", "9", msgCodeSessionOccupied, "ALREADY IN USE"); !errors.Is(got, ErrSessionOccupied) {
		t.Fatalf("classifyAck(OPSP8996) = %v, want ErrSessionOccupied", got)
	}
	if got := classifyAck("H0STCNI0", "9", msgCodeApprovalRejected, ""); !errors.Is(got, ErrApprovalRejected) {
		t.Fatalf("classifyAck(OPSP0011) = %v, want ErrApprovalRejected", got)
	}
	if got := classifyAck("H0STCNI0", "0", "MCA00000", "OK"); got != nil {
		t.Fatalf("classifyAck(success) = %v, want nil", got)
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
