package testutil_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/internal/testutil"
	"github.com/mgh3326/go-kis/kis"
	"github.com/mgh3326/go-kis/kis/domestic"
	"github.com/mgh3326/go-kis/kis/overseas"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixturePath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "fixtures", name)
}
func load(t *testing.T, name string) testutil.Exchange {
	t.Helper()
	x, err := testutil.LoadFixture(fixturePath(name))
	if err != nil {
		t.Fatal(err)
	}
	if x.Status == 0 || x.RawBody == "" || x.Headers["tr_id"] == "" || x.Headers["msg_cd"] == "" {
		t.Fatalf("incomplete fixture %s", name)
	}
	return x
}
func fixtureClient(t *testing.T, exchange testutil.Exchange, check func(*http.Request)) *kis.Client {
	t.Helper()
	c, err := kis.NewClient(kis.Config{Host: kis.HostVTS, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, TokenProvider: kis.TokenProviderFunc(func(context.Context) (string, error) { return "fixture-token", nil }), HTTPClient: &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		check(r)
		h := http.Header{}
		for k, v := range exchange.Headers {
			h.Set(k, v)
		}
		return &http.Response{StatusCode: exchange.Status, Header: h, Body: io.NopCloser(strings.NewReader(exchange.RawBody))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestEveryFixtureLoads(t *testing.T) {
	for _, name := range []string{"token.json", "approval.json", "domestic-balance.json", "domestic-order-history.json", "overseas-balance.json", "overseas-order-history.json", "rest-error.json", "ws-subscribe-ack.json", "ws-pingpong.json", "ws-market-data.json"} {
		_ = load(t, name)
	}
}
func TestReadFixturesAndBrokerQueries(t *testing.T) {
	domesticFixture := load(t, "domestic-balance.json")
	domesticClient := fixtureClient(t, domesticFixture, func(r *http.Request) {
		if r.Header.Get("tr_id") != "VTTC8434R" || r.URL.Query().Get("INQR_DVSN") != "00" || r.URL.Query().Get("PRCS_DVSN") != "01" {
			t.Errorf("domestic request=%s %v", r.Header.Get("tr_id"), r.URL.Query())
		}
	})
	balance, err := domestic.Balance(context.Background(), domesticClient, kis.Mock, domestic.BalanceRequest{CANO: "12345678", ACNT_PRDT_CD: "01"})
	if err != nil || len(balance.Output1) != 1 || balance.Output1[0].HLDG_QTY != "001" {
		t.Fatalf("balance=%+v err=%v", balance, err)
	}
	historyFixture := load(t, "overseas-order-history.json")
	historyClient := fixtureClient(t, historyFixture, func(r *http.Request) {
		q := r.URL.Query()
		for k, want := range map[string]string{"ORD_STRT_DT": "20260901", "ORD_END_DT": "20260902", "SLL_BUY_DVSN": "00", "CCLD_NCCS_DVSN": "01", "PDNO": "AAPL", "ORD_DT": ""} {
			if q.Get(k) != want {
				t.Errorf("%s=%q", k, q.Get(k))
			}
		}
		if r.Header.Get("tr_id") != "VTTS3035R" {
			t.Errorf("tr=%q", r.Header.Get("tr_id"))
		}
	})
	history, err := overseas.OrderHistory(context.Background(), historyClient, kis.Mock, overseas.OrderHistoryRequest{CANO: "12345678", ACNT_PRDT_CD: "01", ORD_STRT_DT: "20260901", ORD_END_DT: "20260902", SLL_BUY_DVSN: "00", CCLD_NCCS_DVSN: "01", PDNO: "AAPL"})
	if err != nil || len(history.Output1) != 1 || history.Output1[0].FT_ORD_QTY != "001" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}
func TestRESTErrorFixtureNeverExposesUpstreamMessage(t *testing.T) {
	x := load(t, "rest-error.json")
	c := fixtureClient(t, x, func(*http.Request) {})
	err := c.Read(context.Background(), "/uapi/x", "VTTC8434R", nil, nil)
	if err == nil || strings.Contains(err.Error(), "upstream-private-detail") || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("unsafe error=%v", err)
	}
}
