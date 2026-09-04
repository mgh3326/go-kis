package kis

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

const PongMessage = 10

// WSConnection is deliberately small so callers can inject a dialer and tests
// never need a network socket.
type WSConnection interface {
	WriteMessage(context.Context, []byte) error
	WriteControl(context.Context, int, []byte) error
}
type WSDialer interface {
	Dial(context.Context, string) (WSConnection, error)
}

type Subscribe struct {
	Header SubscribeHeader `json:"header"`
	Body   SubscribeBody   `json:"body"`
}
type SubscribeHeader struct {
	ApprovalKey string `json:"approval_key"`
	CustType    string `json:"custtype"`
	TRType      string `json:"tr_type"`
	ContentType string `json:"content-type"`
}
type SubscribeBody struct {
	Input SubscribeInput `json:"input"`
}
type SubscribeInput struct {
	TRID  string `json:"tr_id"`
	TRKey string `json:"tr_key"`
}

func NewSubscribe(approvalKey, trID, trKey string) Subscribe {
	return Subscribe{Header: SubscribeHeader{ApprovalKey: approvalKey, CustType: "P", TRType: "1", ContentType: "utf-8"}, Body: SubscribeBody{Input: SubscribeInput{TRID: trID, TRKey: trKey}}}
}

func ValidateWSURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "ws" || (u.Host != "ops.koreainvestment.com:31000" && u.Host != "ops.koreainvestment.com:21000") || (u.Path != "" && u.Path != "/" && u.Path != "/tryitout") {
		return "", errors.New("kis: WebSocket endpoint is not allowlisted")
	}
	return u.String(), nil
}

type WebSocket struct {
	connection WSConnection
	approval   string
}

func (c *Client) ConnectWS(ctx context.Context, dialer WSDialer, endpoint string) (*WebSocket, error) {
	if dialer == nil {
		return nil, errors.New("kis: WebSocket dialer is required")
	}
	endpoint, err := ValidateWSURL(endpoint)
	if err != nil {
		return nil, err
	}
	approval, err := c.IssueApprovalKey(ctx)
	if err != nil {
		return nil, errors.New("kis: approval unavailable")
	}
	connection, err := dialer.Dial(ctx, endpoint)
	if err != nil {
		return nil, errors.New("kis: WebSocket dial failed")
	}
	return &WebSocket{connection: connection, approval: approval.ApprovalKey}, nil
}
func (w *WebSocket) Subscribe(ctx context.Context, request Subscribe) error {
	request.Header.ApprovalKey = w.approval
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return w.connection.WriteMessage(ctx, raw)
}

// Handle returns application data. KIS PINGPONG system payloads are answered
// with an identical pong control frame and are never delivered to the caller.
func (w *WebSocket) Handle(ctx context.Context, raw []byte) ([]byte, bool, error) {
	var system struct {
		Header struct {
			TRID string `json:"tr_id"`
		} `json:"header"`
	}
	if json.Unmarshal(raw, &system) == nil && system.Header.TRID == "PINGPONG" {
		return nil, false, w.connection.WriteControl(ctx, PongMessage, raw)
	}
	return raw, true, nil
}
