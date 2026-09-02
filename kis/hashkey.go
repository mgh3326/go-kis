package kis

import (
	"context"
	"encoding/json"
	"errors"
)

type HashKeyProvider interface {
	HashKey(context.Context, []byte) (string, error)
}
type HashKeyProviderFunc func(context.Context, []byte) (string, error)

func (f HashKeyProviderFunc) HashKey(ctx context.Context, body []byte) (string, error) {
	return f(ctx, body)
}

type hashKeyResponse struct {
	Hash string `json:"HASH"`
}

func (c *Client) HashKey(ctx context.Context, body []byte) (string, error) {
	var result hashKeyResponse
	err := c.do(ctx, "POST", "/uapi/hashkey", "", nil, body, "", &result)
	if err != nil {
		return "", err
	}
	if result.Hash == "" {
		return "", errors.New("kis: empty hashkey")
	}
	return result.Hash, nil
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
