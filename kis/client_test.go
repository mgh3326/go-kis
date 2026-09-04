package kis

import (
	"context"
	"errors"
	"io"
	"net/http"
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
	for _, host := range []string{"", "http://openapivts.koreainvestment.com:29443", "https://openapivts.koreainvestment.com", "https://openapivts.koreainvestment.com:29443/x", "https://user@openapivts.koreainvestment.com:29443", "https://openapivts.koreainvestment.com:29443?q=1", "https://openapi.koreainvestment.com:9444"} {
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
func TestFinalTransportPinAndReadOnlyMethod(t *testing.T) {
	called := false
	c := clientWith(t, HostVTS, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.Method != http.MethodGet {
			t.Fatal("non-GET reached transport")
		}
		return response(200, `{"rt_cd":"0","msg_cd":"MCA00000","msg1":"ok","output":{}}`), nil
	}), nil)
	if err := c.Read(context.Background(), "/uapi/x", "VTTC8434R", nil, nil); err != nil {
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
	transport := safeTransport(&http.Client{Transport: configured}).(*http.Transport)
	if transport.Proxy != nil || configured.Proxy == nil {
		t.Fatal("proxy was not removed from cloned transport")
	}
	if safeTransport(nil).(*http.Transport).Proxy != nil {
		t.Fatal("default transport retained proxy")
	}
}
func TestRedirectAndSafeEnvelopeErrors(t *testing.T) {
	c := clientWith(t, HostVTS, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(302, ""), nil }), nil)
	if err := c.Read(context.Background(), "/x", "VTTC8434R", nil, nil); !errors.Is(err, ErrRedirectBlocked) {
		t.Fatalf("redirect=%v", err)
	}
	for _, raw := range []string{`{}`, `{"rt_cd":0}`, `{"rt_cd":"1","msg_cd":"E","msg1":"fixture-secret fixture-token raw-payload"}`} {
		c = clientWith(t, HostVTS, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, raw), nil }), nil)
		err := c.Read(context.Background(), "/x", "VTTC8434R", nil, nil)
		if err == nil || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "raw-payload") {
			t.Fatalf("unsafe err=%v", err)
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
	if err := c.Read(context.Background(), "/first", "VTTC8434R", nil, nil); err != nil {
		t.Fatal(err)
	}
	<-sent
	done := make(chan error, 1)
	go func() { done <- c.Read(context.Background(), "/second", "VTTC8434R", nil, nil) }()
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
