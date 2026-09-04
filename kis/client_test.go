package kis

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"msg_cd": {"MCA00000"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func clientWith(t *testing.T, host string, rt http.RoundTripper, clock Clock) *Client {
	t.Helper()
	c, err := NewClient(Config{Host: host, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: rt}, Clock: clock, TokenProvider: TokenProviderFunc(func(context.Context) (string, error) { return "fixture-token", nil })})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRESTHostAllowlistAndHTTPS(t *testing.T) {
	for _, host := range []string{"", "http://openapivts.koreainvestment.com:29443", "https://openapivts.koreainvestment.com", "https://openapivts.koreainvestment.com:29443/x", "https://openapivts.koreainvestment.com:29443//", "https://openapivts.koreainvestment.com:29443/%2e", "https://openapivts.koreainvestment.com:29443\\x", "https://openapivts.koreainvestment.com.:29443", "https://user@openapivts.koreainvestment.com:29443", "https://openapivts.koreainvestment.com:29443?q=1", HostVTS + "?", HostVTS + "#", "https://openapi.koreainvestment.com:9444"} {
		if _, err := NewClient(Config{Host: host, AppKey: "a", AppSecret: "b", RequestTimeout: time.Second}); err == nil {
			t.Fatalf("accepted %q", host)
		}
	}
	for _, host := range []string{HostVTS, HostLive} {
		if _, err := NewClient(Config{Host: host, AppKey: "a", AppSecret: "b", RequestTimeout: time.Second}); err != nil {
			t.Fatalf("host=%s err=%v", host, err)
		}
	}
}

func TestR5SemanticHTTPSOrigin(t *testing.T) {
	if _, err := NewClient(Config{Host: "HTTPS://OPENAPIVTS.KOREAINVESTMENT.COM:29443", AppKey: "a", AppSecret: "b", RequestTimeout: time.Second}); err != nil {
		t.Fatalf("uppercase semantic KIS origin rejected: %v", err)
	}
}
func TestFinalTransportPinAndReadOnlyMethod(t *testing.T) {
	called := false
	c := clientWith(t, HostVTS, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Method != http.MethodGet {
			t.Fatal("non-GET reached transport")
		}
		return response(200, `{"rt_cd":"0","msg_cd":"MCA00000","msg1":"ok","output":{}}`), nil
	}), nil)
	if err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("approved request not sent")
	}
	bad, _ := http.NewRequest(http.MethodGet, "https://example.invalid/x", nil)
	if _, err := c.httpClient.Transport.RoundTrip(bad); !errors.Is(err, ErrTransportPinned) {
		t.Fatalf("err=%v", err)
	}
}
func TestTransportDisablesAmbientProxy(t *testing.T) {
	configured := &http.Transport{Proxy: http.ProxyFromEnvironment}
	roundTripper, err := safeTransport(&http.Client{Transport: configured})
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripper.(*http.Transport)
	if transport.Proxy != nil || configured.Proxy == nil {
		t.Fatal("proxy was not removed from cloned transport")
	}
	defaultTransport, err := safeTransport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaultTransport.(*http.Transport).Proxy != nil {
		t.Fatal("default transport retained proxy")
	}
}

func TestRejectUnsafeStandardTransport(t *testing.T) {
	tests := []struct {
		name      string
		transport http.RoundTripper
		unsafe    bool
	}{
		{"insecure skip verify", &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, true},
		{"server name override", &http.Transport{TLSClientConfig: &tls.Config{ServerName: "other.invalid"}}, false},
		{"custom roots", &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}, false},
		{"verify peer callback", &http.Transport{TLSClientConfig: &tls.Config{VerifyPeerCertificate: func([][]byte, [][]*x509.Certificate) error { return nil }}}, true},
		{"verify connection callback", &http.Transport{TLSClientConfig: &tls.Config{VerifyConnection: func(tls.ConnectionState) error { return nil }}}, true},
		{"custom TCP dial", &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, nil }}, false},
		{"custom tls dial", &http.Transport{DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return nil, nil }}, true},
		{"custom tls protocol", &http.Transport{TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}, true},
		{"ordinary transport", &http.Transport{}, false},
		{"offline fake", roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{"rt_cd":"0"}`), nil }), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: test.transport}})
			if test.unsafe && !errors.Is(err, ErrUnsafeTransport) {
				t.Fatalf("unsafe transport err=%v", err)
			}
			if !test.unsafe && err != nil {
				t.Fatalf("safe transport err=%v", err)
			}
		})
	}
}

func TestR5TLSPropertyPolicy(t *testing.T) {
	defaultTransport := http.DefaultTransport.(*http.Transport)
	safeDefaultClone := defaultTransport.Clone()
	originalTLS := defaultTransport.TLSClientConfig
	defaultTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(func() { defaultTransport.TLSClientConfig = originalTLS })
	config := func(transport http.RoundTripper) Config {
		return Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: transport}}
	}
	if _, err := NewClient(config(defaultTransport)); !errors.Is(err, ErrUnsafeTransport) {
		t.Fatalf("mutated global default accepted: %v", err)
	}
	if _, err := NewClient(config(safeDefaultClone)); err != nil {
		t.Fatalf("safe default clone rejected: %v", err)
	}
	if _, err := NewClient(config(&http.Transport{TLSClientConfig: &tls.Config{Time: time.Now}})); !errors.Is(err, ErrUnsafeTransport) {
		t.Fatalf("TLS Time callback accepted: %v", err)
	}
	for _, transport := range []http.RoundTripper{
		&http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "example.test", MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS13}},
		&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}},
	} {
		if _, err := NewClient(config(transport)); err != nil {
			t.Fatalf("permitted TLS properties rejected: %v", err)
		}
	}
}

func TestR6ImplicitDefaultTransportIsInspectedAndIsolated(t *testing.T) {
	defaultTransport := http.DefaultTransport.(*http.Transport)
	originalTLS := defaultTransport.TLSClientConfig
	defaultTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	t.Cleanup(func() { defaultTransport.TLSClientConfig = originalTLS })
	_, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second})
	if !errors.Is(err, ErrUnsafeTransport) {
		t.Fatalf("implicit unsafe global default accepted: %v", err)
	}
}

func TestR6RejectsClientSessionCacheBeforeDial(t *testing.T) {
	cache := tls.NewLRUClientSessionCache(1)
	_, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{ClientSessionCache: cache}}}})
	if !errors.Is(err, ErrUnsafeTransport) {
		t.Fatalf("ClientSessionCache accepted: %v", err)
	}
}

func TestR7RootCAPoolIsClientOwned(t *testing.T) {
	callerPool := x509.NewCertPool()
	c, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: callerPool}}}})
	if err != nil {
		t.Fatal(err)
	}
	owned := c.httpClient.Transport.(pinningTransport).base.(*http.Transport).TLSClientConfig.RootCAs
	if owned == callerPool {
		t.Fatal("client retained caller RootCAs by reference")
	}
	if len(owned.Subjects()) != 0 {
		t.Fatalf("unexpected initial owned roots: %d", len(owned.Subjects()))
	}
	if ok := callerPool.AppendCertsFromPEM(ephemeralCAPEM(t)); !ok {
		t.Fatal("failed to add ephemeral caller CA")
	}
	if len(callerPool.Subjects()) == 0 || len(owned.Subjects()) != 0 {
		t.Fatalf("caller CA mutation changed client trust: caller=%d owned=%d", len(callerPool.Subjects()), len(owned.Subjects()))
	}
}

func TestR7ImplicitRootCAPoolIsClientOwned(t *testing.T) {
	defaultTransport := http.DefaultTransport.(*http.Transport)
	originalTLS := defaultTransport.TLSClientConfig
	callerPool := x509.NewCertPool()
	defaultTransport.TLSClientConfig = &tls.Config{RootCAs: callerPool}
	t.Cleanup(func() { defaultTransport.TLSClientConfig = originalTLS })
	c, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	owned := c.httpClient.Transport.(pinningTransport).base.(*http.Transport).TLSClientConfig.RootCAs
	if owned == callerPool {
		t.Fatal("client retained global RootCAs by reference")
	}
	if ok := callerPool.AppendCertsFromPEM(ephemeralCAPEM(t)); !ok {
		t.Fatal("failed to add ephemeral global CA")
	}
	if len(owned.Subjects()) != 0 {
		t.Fatalf("global CA mutation changed client trust: owned=%d", len(owned.Subjects()))
	}
}

func ephemeralCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.CreateCertificate(cryptorand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-test-ca"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local-test-ca"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
}

func TestR7TransportFailuresAreClassifiedAndRedacted(t *testing.T) {
	const sentinel = "transport-raw-sentinel"
	tests := []struct {
		name     string
		err      error
		category string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"dns", &net.DNSError{Name: sentinel, Err: sentinel}, "dns"},
		{"tls", x509.UnknownAuthorityError{Cert: &x509.Certificate{}}, "tls"},
		{"connection", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New(sentinel)}, "connection"},
		{"other wrapped URL", &url.Error{Op: "Get", URL: "https://user-info@host.invalid/path?query=" + sentinel, Err: errors.New(sentinel)}, "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := clientWith(t, HostLive, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, test.err }), nil)
			err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", map[string]string{"query": sentinel}, nil)
			transportErr, ok := err.(*TransportError)
			if !ok || transportErr.Category != test.category || !errors.Is(err, ErrTransportFailure) {
				t.Fatalf("transport error=%#v", err)
			}
			if transportErr.Location != HostLive+"/uapi/domestic-stock/v1/trading/inquire-balance" {
				t.Fatalf("unsafe request location=%q", transportErr.Location)
			}
			for value := err; value != nil; value = errors.Unwrap(value) {
				for _, rendered := range []string{value.Error(), fmt.Sprintf("%v", value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
					if strings.Contains(rendered, sentinel) || strings.Contains(rendered, "user-info") || strings.Contains(rendered, "query=") {
						t.Fatalf("transport detail leaked: %q", rendered)
					}
				}
			}
		})
	}
}

func TestR5DecodeErrorDoesNotExposeResponseValue(t *testing.T) {
	secretLike := "credential-shaped-sentinel"
	var output struct {
		Output []struct {
			When time.Time `json:"when"`
		} `json:"output"`
	}
	c := clientWith(t, HostLive, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"rt_cd":"0","msg_cd":"MCA00000","msg1":"ok","output":[{"when":"credential-shaped-sentinel"}]}`), nil
	}), nil)
	err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, &output)
	if err == nil || !errors.Is(err, ErrDecodeResponse) || strings.Contains(fmt.Sprint(err), secretLike) || strings.Contains(fmt.Sprintf("%+v", err), secretLike) {
		t.Fatalf("unsafe decode error: %v", err)
	}
}
func TestRedirectAndSafeEnvelopeErrors(t *testing.T) {
	c := clientWith(t, HostVTS, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(302, ""), nil }), nil)
	if err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil); !errors.Is(err, ErrRedirectBlocked) {
		t.Fatalf("redirect=%v", err)
	}
	for _, raw := range []string{`{}`, `{"rt_cd":0}`, `{"rt_cd":"1","msg_cd":"E","msg1":"fixture-secret fixture-token raw-payload"}`} {
		c = clientWith(t, HostVTS, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, raw), nil }), nil)
		err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil)
		if err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "raw-payload") {
			t.Fatalf("unsafe err=%v", err)
		}
	}
}

func TestReadAllowlistRejectsMutationLikeInputsBeforeTransport(t *testing.T) {
	called := false
	f := &fakeClock{now: time.Unix(0, 0), started: make(chan struct{}, 1)}
	c := clientWith(t, HostLive, roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return response(200, `{"rt_cd":"0"}`), nil
	}), f)
	for _, request := range []struct{ path, trID string }{
		{"/uapi/domestic-stock/v1/trading/order-cash", "TTTC0012U"},
		{"/uapi/domestic-stock/v1/trading/inquire-balance", "TTTC0012U"},
		{"/uapi/domestic-stock/v1/trading/inquire-balance", "not-a-read"},
	} {
		if err := c.Read(context.Background(), request.path, request.trID, nil, nil); err == nil {
			t.Fatalf("path=%s tr=%s err=%v", request.path, request.trID, err)
		}
	}
	if called || len(f.waits) != 0 {
		t.Fatalf("rejected read reached safety boundary: called=%v waits=%v", called, f.waits)
	}
}

func TestAPIErrorNeverContainsUntrustedMessageCode(t *testing.T) {
	secretLike := "fixture-secret"
	for _, test := range []struct {
		status int
		body   string
		header string
	}{
		{http.StatusOK, `{"rt_cd":"1","msg_cd":"fixture-secret","msg1":"ignored"}`, ""},
		{http.StatusBadRequest, `{"rt_cd":"1","msg_cd":"ignored","msg1":"ignored"}`, secretLike},
	} {
		c := clientWith(t, HostLive, roundTripFunc(func(*http.Request) (*http.Response, error) {
			resp := response(test.status, test.body)
			resp.Header.Set("msg_cd", test.header)
			return resp, nil
		}), nil)
		err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil)
		apiErr, ok := err.(*APIError)
		if !ok || strings.Contains(apiErr.Code, secretLike) || strings.Contains(apiErr.Message, secretLike) || strings.Contains(err.Error(), secretLike) {
			t.Fatalf("untrusted code leaked: err=%#v", err)
		}
	}
}

type fakeClock struct {
	mu       sync.Mutex
	now      time.Time
	waits    []time.Duration
	channels []chan time.Time
	started  chan struct{}
}

func (f *fakeClock) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waits = append(f.waits, d)
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	ch := make(chan time.Time, 1)
	f.channels = append(f.channels, ch)
	return ch
}
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	for _, ch := range f.channels {
		ch <- f.now
	}
	f.channels = nil
	f.mu.Unlock()
}
func TestLimiterSpacingAndCancellation(t *testing.T) {
	f := &fakeClock{now: time.Unix(0, 0), started: make(chan struct{}, 2)}
	l := &limiter{clock: f, interval: 500 * time.Millisecond}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background()) }()
	<-f.started
	if f.waits[0] != 500*time.Millisecond {
		t.Fatalf("wait=%v", f.waits)
	}
	f.Advance(500 * time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
func TestRESTLimiterIsApplied(t *testing.T) {
	f := &fakeClock{now: time.Unix(0, 0), started: make(chan struct{}, 2)}
	sent := make(chan struct{}, 2)
	c := clientWith(t, HostLive, roundTripFunc(func(*http.Request) (*http.Response, error) {
		sent <- struct{}{}
		return response(200, `{"rt_cd":"0","msg_cd":"MCA00000","msg1":"ok"}`), nil
	}), f)
	if err := c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil); err != nil {
		t.Fatal(err)
	}
	<-sent
	done := make(chan error, 1)
	go func() {
		done <- c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil)
	}()
	select {
	case <-sent:
		t.Fatal("second REST send bypassed limiter")
	case <-f.started:
	}
	f.Advance(50 * time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUncachedReadSpacesOAuthAndReadTransmissions(t *testing.T) {
	f := &fakeClock{now: time.Unix(0, 0), started: make(chan struct{}, 2)}
	type sentRequest struct {
		path string
		at   time.Time
	}
	sent := make(chan sentRequest, 2)
	c, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, Clock: f, HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		entry := sentRequest{path: r.URL.Path, at: f.Now()}
		sent <- entry
		if r.URL.Path == "/oauth2/tokenP" {
			return response(200, `{"access_token":"fixture-token","token_type":"Bearer","expires_in":3600,"access_token_token_expired":"2099"}`), nil
		}
		return response(200, `{"rt_cd":"0","msg_cd":"MCA00000","msg1":"ok"}`), nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- c.Read(context.Background(), "/uapi/domestic-stock/v1/trading/inquire-balance", "VTTC8434R", nil, nil)
	}()
	var first sentRequest
	select {
	case first = <-sent:
	case <-f.started:
		t.Fatal("OAuth issuance was rate-limited behind an outer read reservation")
	}
	if first.path != "/oauth2/tokenP" {
		t.Fatalf("first send=%s", first.path)
	}
	select {
	case next := <-sent:
		t.Fatalf("read bypassed limiter: %s", next.path)
	case <-f.started:
	}
	f.Advance(50 * time.Millisecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	second := <-sent
	if second.path != "/uapi/domestic-stock/v1/trading/inquire-balance" || second.at.Sub(first.at) != 50*time.Millisecond {
		t.Fatalf("transmissions=%+v then %+v", first, second)
	}
}
func TestTokenLifecycleReuseRefreshFailureAndConcurrency(t *testing.T) {
	f := &fakeClock{now: time.Unix(0, 0)}
	var mu sync.Mutex
	issues := 0
	fail := false
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		issues++
		if fail {
			return response(500, ""), nil
		}
		return response(200, `{"access_token":"token","token_type":"Bearer","expires_in":120,"access_token_token_expired":"2099"}`), nil
	})
	c, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: rt}, Clock: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if issues != 1 {
		t.Fatalf("issues=%d", issues)
	}
	f.Advance(61 * time.Second)
	if _, err := c.token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if issues != 2 {
		t.Fatalf("refresh issues=%d", issues)
	}
	f.Advance(61 * time.Second)
	fail = true
	if _, err := c.token(context.Background()); err == nil {
		t.Fatal("stale token used after refresh failure")
	}
	fail = false
	c2, err := NewClient(Config{Host: HostLive, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, HTTPClient: &http.Client{Transport: rt}, Clock: f})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := c2.token(context.Background()); err != nil {
				t.Errorf("token=%v", err)
			}
		}()
	}
	group.Wait()
	if issues != 4 {
		t.Fatalf("concurrent duplicate issuance: %d", issues)
	}
}
