// Package kis is an unofficial, read-only client for selected KIS protocols.
package kis

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	ErrTransportPinned = errors.New("kis: request target is not an approved KIS REST host")
	ErrUnsafeTransport = errors.New("kis: caller HTTP transport weakens TLS or authority verification")
)

// TokenProvider permits applications with their own token cache to supply a token.
// When omitted, Client obtains and safely caches OAuth tokens itself.
type TokenProvider interface {
	Token(context.Context) (string, error)
}
type TokenProviderFunc func(context.Context) (string, error)

func (f TokenProviderFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

// Clock permits deterministic rate-limit and token-life tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type Config struct {
	Host           string
	AppKey         string
	AppSecret      string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	TokenProvider  TokenProvider
	Clock          Clock
}

type Client struct {
	host, appKey, appSecret string
	httpClient              *http.Client
	timeout                 time.Duration
	tokens                  TokenProvider
	clock                   Clock
	limiter                 *limiter
	tokenMu                 sync.Mutex
	cachedToken             OAuthToken
	cachedUntil             time.Time
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
	clock := config.Clock
	if clock == nil {
		clock = realClock{}
	}
	transport, err := safeTransport(config.HTTPClient)
	if err != nil {
		return nil, err
	}
	copy := http.Client{Timeout: timeout, Transport: pinningTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirectBlocked }}
	interval := 50 * time.Millisecond
	if host == HostVTS {
		interval = 500 * time.Millisecond
	}
	return &Client{host: host, appKey: config.AppKey, appSecret: config.AppSecret, httpClient: &copy, timeout: timeout, tokens: config.TokenProvider, clock: clock, limiter: &limiter{clock: clock, interval: interval}}, nil
}

// safeTransport accepts fake RoundTrippers for offline tests. A caller-supplied
// standard transport must retain ordinary TLS peer and hostname verification;
// proxy use is independently removed from the clone below.
func safeTransport(client *http.Client) (http.RoundTripper, error) {
	var base http.RoundTripper
	callerSupplied := false
	if client != nil {
		base = client.Transport
		callerSupplied = base != nil
	}
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		if callerSupplied && unsafeStandardTransport(transport) {
			return nil, ErrUnsafeTransport
		}
		clone := transport.Clone()
		clone.Proxy = nil // credentials must never use an ambient proxy.
		return clone, nil
	}
	return base, nil
}

func unsafeStandardTransport(transport *http.Transport) bool {
	if transport.DialTLS != nil || transport.DialTLSContext != nil || transport.TLSNextProto != nil {
		return true
	}
	config := transport.TLSClientConfig
	if config == nil {
		return false
	}
	return config.InsecureSkipVerify || config.VerifyPeerCertificate != nil || config.VerifyConnection != nil || config.Time != nil || config.GetCertificate != nil || config.GetClientCertificate != nil || config.GetConfigForClient != nil || config.Renegotiation != tls.RenegotiateNever || config.KeyLogWriter != nil || config.Rand != nil || config.WrapSession != nil || config.UnwrapSession != nil || config.EncryptedClientHelloRejectionVerify != nil || config.GetEncryptedClientHelloKeys != nil || (config.MinVersion != 0 && config.MinVersion < tls.VersionTLS12) || (config.MaxVersion != 0 && config.MaxVersion < tls.VersionTLS12)
}

func normalizeHost(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\t\\") {
		return "", ErrHostRequired
	}
	if strings.ContainsAny(raw, "?#") {
		return "", errors.New("kis: invalid REST host")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("kis: invalid REST host")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("kis: REST must use HTTPS")
	}
	hostname, port := strings.ToLower(u.Hostname()), u.Port()
	if (hostname != "openapivts.koreainvestment.com" || port != "29443") && (hostname != "openapi.koreainvestment.com" || port != "9443") {
		return "", errors.New("kis: REST host is not allowlisted")
	}
	if hostname == "openapivts.koreainvestment.com" {
		return HostVTS, nil
	}
	return HostLive, nil
}

type pinningTransport struct{ base http.RoundTripper }

func (p pinningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || (req.URL.String() != HostVTS+req.URL.RequestURI() && req.URL.String() != HostLive+req.URL.RequestURI()) {
		return nil, ErrTransportPinned
	}
	return p.base.RoundTrip(req)
}

func safeHeader(value string) bool { return !strings.ContainsAny(value, "\r\n") }
func (c *Client) Host() string     { return c.host }

func (c *Client) token(ctx context.Context) (string, error) {
	if c.tokens != nil {
		token, err := c.tokens.Token(ctx)
		if err != nil || token == "" || !safeHeader(token) {
			return "", errors.New("kis: token unavailable")
		}
		return token, nil
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.cachedToken.AccessToken != "" && c.clock.Now().Before(c.cachedUntil) {
		return c.cachedToken.AccessToken, nil
	}
	issued, err := c.issueToken(ctx)
	if err != nil {
		return "", errors.New("kis: token unavailable")
	}
	if issued.AccessToken == "" || issued.ExpiresIn <= 0 || !safeHeader(issued.AccessToken) {
		return "", errors.New("kis: token unavailable")
	}
	c.cachedToken = issued
	c.cachedUntil = c.clock.Now().Add(time.Duration(issued.ExpiresIn)*time.Second - time.Minute)
	return issued.AccessToken, nil
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
