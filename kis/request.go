package kis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const responseLimit = 2 << 20

var ErrReadNotAllowed = errors.New("kis: read endpoint is not supported")

var supportedReads = map[string]map[string]struct{}{
	"/uapi/domestic-stock/v1/trading/inquire-balance": {
		"VTTC8434R": {}, "TTTC8434R": {},
	},
	"/uapi/domestic-stock/v1/trading/inquire-daily-ccld": {
		"VTTC8001R": {}, "TTTC8001R": {},
	},
	"/uapi/overseas-stock/v1/trading/inquire-balance": {
		"VTTS3012R": {}, "TTTS3012R": {},
	},
	"/uapi/overseas-stock/v1/trading/inquire-ccnl": {
		"VTTS3035R": {}, "TTTS3035R": {},
	},
}

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

// Read sends a GET-only, read-only KIS transaction. There is no public method
// capable of sending an account mutation.
func (c *Client) Read(ctx context.Context, path, trID string, query map[string]string, output any) error {
	if _, ok := supportedReads[path][trID]; !ok {
		return ErrReadNotAllowed
	}
	return c.send(ctx, http.MethodGet, path, trID, query, nil, true, output)
}

func (c *Client) send(ctx context.Context, method, path, trID string, query map[string]string, body []byte, envelope bool, output any) error {
	req, cancel, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer cancel()
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	isAuth := path == "/oauth2/tokenP" || path == "/oauth2/Approval"
	if !isAuth && path != "/uapi/hashkey" {
		token, e := c.token(ctx)
		if e != nil {
			return e
		}
		req.Header.Set("authorization", "Bearer "+token)
	}
	if !isAuth {
		req.Header.Set("appkey", c.appKey)
		req.Header.Set("appsecret", c.appSecret)
		req.Header.Set("tr_id", trID)
		req.Header.Set("custtype", "P")
	}
	if len(body) > 0 {
		req.Header.Set("content-type", "application/json")
	}
	// Limit each actual transmission, rather than reserving a slot before an
	// uncached OAuth exchange that may itself transmit first.
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirectBlocked) {
			return ErrRedirectBlocked
		}
		return fmt.Errorf("kis: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ErrRedirectBlocked
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return errors.New("kis: read response failed")
	}
	if len(raw) > responseLimit {
		return errors.New("kis: response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Code: "HTTP_REJECTED", Message: "request rejected"}
	}
	if envelope {
		var shape map[string]json.RawMessage
		if json.Unmarshal(raw, &shape) != nil || shape["rt_cd"] == nil {
			return &APIError{Status: resp.StatusCode, Code: "INVALID_ENVELOPE", Message: "invalid response envelope"}
		}
		var rt string
		if json.Unmarshal(shape["rt_cd"], &rt) != nil || rt != "0" {
			return &APIError{Status: resp.StatusCode, Code: "UPSTREAM_REJECTED", Message: "request rejected"}
		}
	}
	if output != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, output); err != nil {
			return fmt.Errorf("kis: decode response: %w", err)
		}
	}
	return nil
}
