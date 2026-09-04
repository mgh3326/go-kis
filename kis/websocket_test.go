package kis

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeWS struct {
	message     []byte
	control     []byte
	controlType int
}

func (f *fakeWS) WriteMessage(_ context.Context, raw []byte) error {
	f.message = append([]byte(nil), raw...)
	return nil
}
func (f *fakeWS) WriteControl(_ context.Context, kind int, raw []byte) error {
	f.controlType = kind
	f.control = append([]byte(nil), raw...)
	return nil
}

type fakeDialer struct {
	connection *fakeWS
	endpoint   string
}

func (d *fakeDialer) Dial(_ context.Context, endpoint string) (WSConnection, error) {
	d.endpoint = endpoint
	return d.connection, nil
}
func TestWebSocketAuthoritySubscribeAndPingPong(t *testing.T) {
	for _, endpoint := range []string{"ws://ops.koreainvestment.com:31000/tryitout", "ws://ops.koreainvestment.com:21000/tryitout"} {
		if got, err := ValidateWSURL(endpoint); err != nil || got != endpoint {
			t.Fatalf("endpoint=%s got=%s err=%v", endpoint, got, err)
		}
	}
	for _, endpoint := range []string{"wss://ops.koreainvestment.com:31000/tryitout", "ws://example.test:31000/tryitout", "ws://ops.koreainvestment.com:31000", "ws://ops.koreainvestment.com:31000/", "ws://ops.koreainvestment.com:31000/tryitout?", "ws://ops.koreainvestment.com:31000/tryitout?x=1", "ws://ops.koreainvestment.com:31000/tryitout#", "ws://user@ops.koreainvestment.com:31000/tryitout", "ws://ops.koreainvestment.com:9999/tryitout"} {
		if _, err := ValidateWSURL(endpoint); err == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	approvalRT := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/oauth2/Approval" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"approval_key":"fixture-approval"}`))}, nil
	})
	c := clientWith(t, HostVTS, approvalRT, nil)
	connection := &fakeWS{}
	dialer := &fakeDialer{connection: connection}
	ws, err := c.ConnectWS(context.Background(), dialer, "ws://ops.koreainvestment.com:31000/tryitout")
	if err != nil {
		t.Fatal(err)
	}
	if dialer.endpoint == "" {
		t.Fatal("no fake dial")
	}
	if err := ws.Subscribe(context.Background(), NewSubscribe("ignored", "H0STCNT0", "005930")); err != nil {
		t.Fatal(err)
	}
	want := `{"header":{"approval_key":"fixture-approval","custtype":"P","tr_type":"1","content-type":"utf-8"},"body":{"input":{"tr_id":"H0STCNT0","tr_key":"005930"}}}`
	if string(connection.message) != want {
		t.Fatalf("subscribe=%s", connection.message)
	}
	raw := []byte(`{"header":{"tr_id":"PINGPONG"},"body":{"rt_cd":"0","msg_cd":"MCA00000","msg1":"PING","output":{}}}`)
	data, deliver, err := ws.Handle(context.Background(), raw)
	if err != nil || deliver || data != nil || connection.controlType != PongMessage || string(connection.control) != string(raw) {
		t.Fatalf("ping result=%q deliver=%v err=%v", data, deliver, err)
	}
}
