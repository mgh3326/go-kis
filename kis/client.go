// Package kis is an unofficial, low-level client for the Korea Investment &
// Securities REST API. It deliberately contains no trading policy.
package kis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	HostVTS  = "https://openapivts.koreainvestment.com:29443"
	HostLive = "https://openapi.koreainvestment.com:9443"
)

var (
	ErrHostRequired    = errors.New("kis: host is required")
	ErrTimeoutRequired = errors.New("kis: request timeout is required")
	ErrRedirectBlocked = errors.New("kis: redirect blocked")
	ErrHashKeyRequired = errors.New("kis: hashkey is required for mutation requests")
)

// TokenProvider supplies a bearer token. Storage, refresh policy, and Redis
// integration belong to the application using this client.
type TokenProvider interface {
	Token(context.Context) (string, error)
}
type TokenProviderFunc func(context.Context) (string, error)

func (f TokenProviderFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

type Config struct {
	Host           string
	AppKey         string
	AppSecret      string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	TokenProvider  TokenProvider
}

type Client struct {
	host       string
	appKey     string
	appSecret  string
	httpClient *http.Client
	timeout    time.Duration
	tokens     TokenProvider
}

func NewClient(config Config) (*Client, error) {
	host, err := normalizeHost(config.Host)
	if err != nil {
		return nil, err
	}
	if config.AppKey == "" || config.AppSecret == "" || !safeHeader(config.AppKey) || !safeHeader(config.AppSecret) {
		return nil, errors.New("kis: app credentials are required")
	}
	timeout := config.RequestTimeout
	if timeout <= 0 && config.HTTPClient != nil {
		timeout = config.HTTPClient.Timeout
	}
	if timeout <= 0 {
		return nil, ErrTimeoutRequired
	}
	client := http.DefaultClient
	if config.HTTPClient != nil {
		client = config.HTTPClient
	}
	copy := *client
	copy.Timeout = timeout
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrRedirectBlocked }
	return &Client{host: host, appKey: config.AppKey, appSecret: config.AppSecret, httpClient: &copy, timeout: timeout, tokens: config.TokenProvider}, nil
}

func normalizeHost(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrHostRequired
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("kis: invalid host")
	}
	u.Path, u.RawPath = "", ""
	return u.String(), nil
}

func safeHeader(value string) bool { return !strings.ContainsAny(value, "\r\n") }
func (c *Client) Host() string     { return c.host }

func (c *Client) token(ctx context.Context) (string, error) {
	if c.tokens == nil {
		return "", errors.New("kis: token provider is required")
	}
	token, err := c.tokens.Token(ctx)
	if err != nil || token == "" || !safeHeader(token) {
		return "", errors.New("kis: token unavailable")
	}
	return token, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, context.CancelFunc, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, nil, fmt.Errorf("kis: invalid API path")
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	req, err := http.NewRequestWithContext(requestContext, method, c.host+path, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return req, cancel, nil
}
