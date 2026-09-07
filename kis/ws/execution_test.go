package ws_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mgh3326/go-kis/kis/ws"
)

// executionMaterial is the AES-CBC key/iv pair the execution ACK fixture
// delivers, restated here so the test asserts against a stated expectation
// rather than against whatever the fixture happens to contain.
const (
	executionKey = "AES-SAMPLE-AES-SAMPLE-AES-SAMPLE"
	executionIV  = "SAMPLE-IV-000000"
)

// wantExecution is the record encrypted into the execution frame fixture.
var wantExecution = ws.Execution{
	OrderNo:  "0000012345",
	Symbol:   "005930",
	Side:     ws.SideBuy,
	Qty:      "10",
	Price:    "71000",
	FilledAt: "091000",
	Filled:   "2",
}

// Test 2 and 12: the ACK's iv/key are stored per transaction, the encrypted
// frame is decrypted with them, its fields are split, and the execution notice
// maps identically for the live and mock transaction IDs.
func TestEncryptedExecutionDecryptedFromAckMaterial(t *testing.T) {
	for _, tr := range []string{ws.TRExecutionLive, ws.TRExecutionVTS} {
		t.Run(tr, func(t *testing.T) {
			// The acknowledgement is the recorded fixture, so the key and iv
			// under test are the ones a stored KIS response carries.
			ack := fixture(t, "ws-execution-ack.json")
			if !strings.Contains(ack, executionKey) || !strings.Contains(ack, executionIV) {
				t.Fatalf("fixture no longer carries the stated material: %s", ack)
			}
			if tr == ws.TRExecutionVTS {
				ack = strings.ReplaceAll(ack, ws.TRExecutionLive, ws.TRExecutionVTS)
			}
			server := newServer()
			server.setReply(func(conn *fakeTransport, _ wireRequest, _ []byte) {
				conn.push(ack)
			})
			conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
			if err := conn.Subscribe(context.Background(), tr, "USERID01"); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}

			// The fixture frame was captured under the live transaction ID;
			// the mock stream carries the identical record layout.
			frame := fixture(t, "ws-execution-frame.json")
			if tr == ws.TRExecutionVTS {
				frame = strings.Replace(frame, ws.TRExecutionLive, ws.TRExecutionVTS, 1)
			}
			server.conn(t, 0).push(frame)

			event := waitEvent(t, conn)
			if stats := conn.Stats(); stats.DroppedFrames != 0 {
				t.Fatalf("dropped %d frames (%s); decryption material was not applied", stats.DroppedFrames, stats.LastDropReason)
			}
			if event.TR != tr {
				t.Fatalf("TR = %q, want %q", event.TR, tr)
			}
			if event.Execution == nil {
				t.Fatalf("Execution is nil for %s; fields=%v", tr, event.Fields)
			}
			if got := *event.Execution; got != wantExecution {
				t.Fatalf("execution mismatch:\n got=%+v\nwant=%+v", got, wantExecution)
			}
			if len(event.Fields) != 14 {
				t.Fatalf("fields = %d (%v), want 14 decrypted fields", len(event.Fields), event.Fields)
			}
			if event.Key != "USERID01" {
				t.Fatalf("Key = %q, want the record's leading field", event.Key)
			}
			if string(event.Raw) != frame {
				t.Fatalf("Raw was not preserved verbatim:\n got=%s\nwant=%s", event.Raw, frame)
			}

			qty, err := event.Execution.QtyInt()
			if err != nil || qty != 10 {
				t.Fatalf("QtyInt = %d, %v", qty, err)
			}
			price, err := event.Execution.PriceRat()
			if err != nil || price.RatString() != "71000" {
				t.Fatalf("PriceRat = %v, %v", price, err)
			}
		})
	}
}

// An encrypted frame for a transaction with no ACK material is dropped with a
// reason, and the connection stays up.
func TestEncryptedFrameWithoutMaterialIsDropped(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	socket := server.conn(t, 0)
	socket.push(fixture(t, "ws-execution-frame.json"))
	socket.push(fixture(t, "ws-market-data.json"))

	// The plaintext frame behind it still arrives, so the socket survived.
	event := waitEvent(t, conn)
	if event.TR != ws.TRQuotePrice {
		t.Fatalf("TR = %q, want the plaintext frame to follow the dropped one", event.TR)
	}
	stats := conn.Stats()
	if stats.DroppedFrames != 1 || stats.LastDropTR != ws.TRExecutionLive {
		t.Fatalf("stats = %+v, want one drop for %s", stats, ws.TRExecutionLive)
	}
	if stats.LastDropReason == "" {
		t.Fatal("a dropped frame must record a reason")
	}
	if server.dialCount() != 1 {
		t.Fatalf("dials = %d, want 1: an undecryptable frame must not drop the socket", server.dialCount())
	}
}

// A plaintext quote frame parses without any material at all.
func TestPlaintextQuoteFrame(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRQuotePrice, "005930"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	server.conn(t, 0).push(fixture(t, "ws-market-data.json"))

	event := waitEvent(t, conn)
	if event.TR != ws.TRQuotePrice || event.Key != "005930" {
		t.Fatalf("event = %+v", event)
	}
	if want := []string{"005930", "091000", "71000", "001"}; !equalStrings(event.Fields, want) {
		t.Fatalf("fields = %v, want %v", event.Fields, want)
	}
	if event.Execution != nil {
		t.Fatal("a quote frame must not be parsed as an execution notice")
	}
	if event.ReceivedAt.IsZero() {
		t.Fatal("ReceivedAt was not stamped")
	}
}

// A truncated execution record fills what it can and never panics.
func TestShortExecutionRecordIsTolerated(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Only five fields: order number and side are present, the rest are not.
	frame := "0|H0STCNI0|001|USERID01^000000^0000012345^0000000000^01"
	server.conn(t, 0).push(frame)

	event := waitEvent(t, conn)
	if event.Execution == nil {
		t.Fatal("Execution is nil")
	}
	want := ws.Execution{OrderNo: "0000012345", Side: ws.SideSell}
	if got := *event.Execution; got != want {
		t.Fatalf("execution = %+v, want %+v", got, want)
	}
	if string(event.Raw) != frame {
		t.Fatalf("Raw = %s", event.Raw)
	}
}

// Side codes map to the documented directions and unknown codes stay unknown.
func TestSideCodes(t *testing.T) {
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	socket := server.conn(t, 0)
	for _, testCase := range []struct {
		code string
		want ws.Side
	}{
		{"01", ws.SideSell}, {"1", ws.SideSell}, {"S", ws.SideSell},
		{"02", ws.SideBuy}, {"2", ws.SideBuy}, {"B", ws.SideBuy},
		{"", ws.SideUnknown}, {"99", ws.SideUnknown},
	} {
		socket.push("0|H0STCNI0|001|USERID01^000000^0000012345^0000000000^" + testCase.code + "^0^0^0^005930^10^71000^091000^0^2")
		event := waitEvent(t, conn)
		if event.Execution.Side != testCase.want {
			t.Fatalf("side code %q = %q, want %q", testCase.code, event.Execution.Side, testCase.want)
		}
	}
}

func equalStrings(got, want []string) bool {
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
