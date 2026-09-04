package testutil

import (
	"encoding/json"
	"os"
)

// Exchange keeps a recorded local fixture as an HTTP status, headers, and raw
// response body without needing a server or a real KIS connection.
type Exchange struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	RawBody string            `json:"raw_body"`
}

func LoadFixture(path string) (Exchange, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Exchange{}, err
	}
	var exchange Exchange
	err = json.Unmarshal(raw, &exchange)
	return exchange, err
}
