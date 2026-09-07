package ws_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"slices"
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
	liveKey      = "AES-LIVE-KEY-000"
	liveIV       = "IV-LIVE-00000000"
	vtsKey       = "AES-VTS-KEY-0000"
	vtsIV        = "IV-VTS-000000000"
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

// Two execution subscriptions on one connection keep independent synthetic
// key/iv pairs. A frame labelled as VTS must use the VTS pair, never another
// transaction's material.
func TestExecutionMaterialIsolatedByTransaction(t *testing.T) {
	server := newServer()
	server.setReply(func(conn *fakeTransport, request wireRequest, _ []byte) {
		name := "ws-execution-ack-live.json"
		if request.Body.Input.TRID == ws.TRExecutionVTS {
			name = "ws-execution-ack-vts.json"
		}
		conn.push(fixture(t, name))
	})
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); err != nil {
		t.Fatalf("live Subscribe: %v", err)
	}
	if err := conn.Subscribe(context.Background(), ws.TRExecutionVTS, "USERID01"); err != nil {
		t.Fatalf("VTS Subscribe: %v", err)
	}

	liveAck := fixture(t, "ws-execution-ack-live.json")
	vtsAck := fixture(t, "ws-execution-ack-vts.json")
	if !strings.Contains(liveAck, liveKey) || !strings.Contains(liveAck, liveIV) {
		t.Fatalf("live fixture does not carry the stated synthetic material: %s", liveAck)
	}
	if !strings.Contains(vtsAck, vtsKey) || !strings.Contains(vtsAck, vtsIV) {
		t.Fatalf("VTS fixture does not carry the stated synthetic material: %s", vtsAck)
	}
	if liveKey == vtsKey || liveIV == vtsIV {
		t.Fatal("the two transactions must use different synthetic key/iv values")
	}

	fields := "USERID01^000000^0000012345^0000000000^02^0^0^0^005930^10^71000^091000^0^2"
	socket := server.conn(t, 0)
	socket.push(encryptedExecutionFrame(ws.TRExecutionVTS, vtsKey, vtsIV, fields))
	event := waitEvent(t, conn)
	if event.TR != ws.TRExecutionVTS || event.Execution == nil || event.ExecutionErr != nil {
		t.Fatalf("VTS event = %+v, want successful VTS decryption", event)
	}
	if event.Execution.Symbol != "005930" || event.Execution.Price != "71000" {
		t.Fatalf("VTS execution = %+v, want the synthetic record", *event.Execution)
	}

	// The same payload encrypted with the live pair is labelled VTS. It must
	// not be accepted as a second execution.
	socket.push(encryptedExecutionFrame(ws.TRExecutionVTS, liveKey, liveIV, fields))
	waitFor(t, func() bool { return conn.Stats().DroppedFrames == 1 })
	if stats := conn.Stats(); stats.LastDropTR != ws.TRExecutionVTS || stats.LastDropReason == "" {
		t.Fatalf("wrong-material drop stats = %+v", stats)
	}
	if len(conn.Events()) != 0 {
		t.Fatalf("wrong-material frame published an event: %d buffered", len(conn.Events()))
	}
}

func encryptedExecutionFrame(tr, key, iv, fields string) string {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		panic(err)
	}
	padded := append([]byte(fields), bytesForPadding(len(fields), block.BlockSize())...)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(iv)).CryptBlocks(ciphertext, padded)
	return "1|" + tr + "|001|" + base64.StdEncoding.EncodeToString(ciphertext)
}

func bytesForPadding(length, blockSize int) []byte {
	padding := blockSize - length%blockSize
	return bytes.Repeat([]byte{byte(padding)}, padding)
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

// Execution records with the wrong shape remain visible as events but are not
// silently interpreted as ledger records.
func TestExecutionRecordWithWrongFieldCountIsRejected(t *testing.T) {
	if ws.ExecutionFieldCount != 14 {
		t.Fatalf("ExecutionFieldCount = %d, want 14", ws.ExecutionFieldCount)
	}
	server := newServer()
	conn := dialTest(t, ws.Config{Approval: &staticProvider{key: "k"}, Dialer: server})
	if err := conn.Subscribe(context.Background(), ws.TRExecutionLive, "USERID01"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	base := []string{"USERID01", "000000", "0000012345", "0000000000", "01", "0", "0", "0", "005930", "10", "71000", "091000", "0", "2"}
	for _, count := range []int{13, 15} {
		t.Run(fmt.Sprintf("%d-fields", count), func(t *testing.T) {
			fields := slices.Clone(base)
			if count > len(fields) {
				fields = append(fields, "extra")
			} else {
				fields = fields[:count]
			}
			frame := "0|H0STCNI0|001|" + strings.Join(fields, "^")
			server.conn(t, 0).push(frame)

			event := waitEvent(t, conn)
			if event.Execution != nil {
				t.Fatalf("Execution = %+v, want nil for %d fields", event.Execution, count)
			}
			if event.ExecutionErr == nil || !strings.Contains(event.ExecutionErr.Error(), fmt.Sprintf("%d fields", count)) || !strings.Contains(event.ExecutionErr.Error(), "want 14") {
				t.Fatalf("ExecutionErr = %v, want an explicit %d-versus-14 error", event.ExecutionErr, count)
			}
			if event.TR != ws.TRExecutionLive || len(event.Fields) != count || string(event.Raw) != frame {
				t.Fatalf("event = %+v, want TR, %d fields, and original Raw", event, count)
			}
		})
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
