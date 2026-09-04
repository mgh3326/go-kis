package overseas

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/kis"
)

type r4RoundTripper func(*http.Request) (*http.Response, error)

func (f r4RoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func r4Client(t *testing.T, roundTrip http.RoundTripper) *kis.Client {
	t.Helper()
	client, err := kis.NewClient(kis.Config{Host: kis.HostVTS, AppKey: "fixture-appkey", AppSecret: "fixture-secret", RequestTimeout: time.Second, TokenProvider: kis.TokenProviderFunc(func(context.Context) (string, error) { return "fixture-token", nil }), HTTPClient: &http.Client{Transport: roundTrip}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestR4HistoryResponseUsesOfficialOutput(t *testing.T) {
	client := r4Client(t, r4RoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"rt_cd":"0","msg_cd":"MCA00000","msg1":"OK","output":[{"odno":"US-42","pdno":"AAPL","ft_ord_qty":"001","ft_ord_unpr3":"190.00"}]}`))}, nil
	}))
	result, err := OrderHistory(context.Background(), client, kis.Mock, OrderHistoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) != 1 || result.Output[0].ODNO != "US-42" {
		t.Fatalf("official output was not decoded: %+v", result)
	}
}

func TestR4MockHistoryQueryRestrictions(t *testing.T) {
	client := r4Client(t, r4RoundTripper(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		for key, want := range map[string]string{"PDNO": "", "OVRS_EXCG_CD": "", "SLL_BUY_DVSN": "00", "CCLD_NCCS_DVSN": "00", "ODNO": "", "SORT_SQN": "DS"} {
			if got := query.Get(key); got != want {
				t.Errorf("%s=%q want %q", key, got, want)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"rt_cd":"0","msg_cd":"MCA00000","msg1":"OK","output":[]}`))}, nil
	}))
	_, err := OrderHistory(context.Background(), client, kis.Mock, OrderHistoryRequest{PDNO: "AAPL", OVRS_EXCG_CD: "NASD", SLL_BUY_DVSN: "02", CCLD_NCCS_DVSN: "01", ODNO: "override", SORT_SQN: "AS"})
	if err != nil {
		t.Fatal(err)
	}
}
