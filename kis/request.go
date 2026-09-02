package kis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const responseLimit = 2 << 20

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("kis: API error (status=%d, code=%s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("kis: API error (status=%d): %s", e.Status, e.Message)
}

type Envelope struct {
	RTCD  string `json:"rt_cd"`
	MsgCD string `json:"msg_cd"`
	Msg1  string `json:"msg1"`
}

// Do executes an API request. It adds standard KIS headers and maps the KIS
// response envelope to APIError. It never includes credential values in errors.
func (c *Client) Do(ctx context.Context, method, path, trID string, query map[string]string, payload any, requireHashKey bool, output any) error {
	if method != "GET" && strings.HasPrefix(path, "/uapi/") && path != "/uapi/hashkey" && !requireHashKey {
		return ErrHashKeyRequired
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("kis: encode request: %w", err)
		}
	}
	if requireHashKey {
		key, keyErr := c.HashKey(ctx, body)
		if keyErr != nil {
			return keyErr
		}
		return c.do(ctx, method, path, trID, query, body, key, output)
	}
	return c.do(ctx, method, path, trID, query, body, "", output)
}

func (c *Client) do(ctx context.Context, method, path, trID string, query map[string]string, body []byte, hashKey string, output any) error {
	req, cancel, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer cancel()
	q := req.URL.Query()
	for key, value := range query {
		q.Set(key, value)
	}
	req.URL.RawQuery = q.Encode()
	token := ""
	isOAuth := path == "/oauth2/tokenP" || path == "/oauth2/Approval"
	if !isOAuth && path != "/uapi/hashkey" {
		token, err = c.token(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("authorization", "Bearer "+token)
	}
	if !isOAuth {
		req.Header.Set("appkey", c.appKey)
		req.Header.Set("appsecret", c.appSecret)
		req.Header.Set("tr_id", trID)
		req.Header.Set("custtype", "P")
	}
	if len(body) > 0 {
		req.Header.Set("content-type", "application/json")
	}
	if hashKey != "" {
		req.Header.Set("hashkey", hashKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirectBlocked) {
			return ErrRedirectBlocked
		}
		return fmt.Errorf("kis: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return errors.New("kis: read response failed")
	}
	if len(raw) > responseLimit {
		return errors.New("kis: response too large")
	}
	var envelope Envelope
	_ = json.Unmarshal(raw, &envelope)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.RTCD != "" && envelope.RTCD != "0") {
		return &APIError{Status: resp.StatusCode, Code: envelope.MsgCD, Message: c.redact(envelope.Msg1, token)}
	}
	if output != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, output); err != nil {
			return fmt.Errorf("kis: decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) redact(message string, values ...string) string {
	values = append(values, c.appKey, c.appSecret)
	for _, secret := range values {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}
