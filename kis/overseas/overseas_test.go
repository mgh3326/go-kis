package overseas

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/kis"
)

func testClient(t *testing.T, host string) *kis.Client {
	t.Helper()
	client, err := kis.NewClient(kis.Config{
		Host:           host,
		AppKey:         "test-appkey",
		AppSecret:      "test-appsecret",
		RequestTimeout: time.Second,
		TokenProvider: kis.TokenProviderFunc(func(context.Context) (string, error) {
			return "test-token", nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// These sanitized payloads mirror broker-edge's observed US history field
// names, including the FT_* values required by VTTS3035R.
func TestUSReadResponseCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantTRID string
		payload  string
		check    func(*testing.T, BalanceResponse, OrderHistoryResponse)
		call     func(context.Context, *kis.Client) (BalanceResponse, OrderHistoryResponse, error)
	}{
		{
			name: "balance VTTS3012R", path: balancePath, wantTRID: "VTTS3012R",
			payload: `{"rt_cd":"0","output1":[{"ovrs_pdno":"AAPL","ovrs_item_name":"Apple","ovrs_cblc_qty":"001","pchs_avg_pric":"180.00","ovrs_stck_evlu_amt":"190.00"}],"output2":[{"frcr_evlu_tota":"190.00"}]}`,
			call: func(ctx context.Context, client *kis.Client) (BalanceResponse, OrderHistoryResponse, error) {
				result, err := Balance(ctx, client, kis.Mock, BalanceRequest{CANO: "12345678", ACNT_PRDT_CD: "01"})
				return result, OrderHistoryResponse{}, err
			},
			check: func(t *testing.T, balance BalanceResponse, _ OrderHistoryResponse) {
				if len(balance.Output1) != 1 || balance.Output1[0].OVRS_PDNO != "AAPL" || balance.Output1[0].OVRS_CBLN_QTY != "001" {
					t.Fatalf("balance=%+v", balance.Output1)
				}
			},
		},
		{
			name: "order history VTTS3035R", path: historyPath, wantTRID: "VTTS3035R",
			payload: `{"rt_cd":"0","output1":[{"odno":"US-42","sll_buy_dvsn_cd":"02","pdno":"BRK/B","ft_ord_qty":"001","ft_ord_unpr3":"1.00","ord_dt":"20260901","ord_tmd":"210100"}]}`,
			call: func(ctx context.Context, client *kis.Client) (BalanceResponse, OrderHistoryResponse, error) {
				result, err := OrderHistory(ctx, client, kis.Mock, OrderHistoryRequest{CANO: "12345678", ACNT_PRDT_CD: "01", ORD_DT: "20260901"})
				return BalanceResponse{}, result, err
			},
			check: func(t *testing.T, _ BalanceResponse, history OrderHistoryResponse) {
				if len(history.Output1) != 1 || history.Output1[0].ODNO != "US-42" || history.Output1[0].FT_ORD_QTY != "001" || history.Output1[0].FT_ORD_UNPR3 != "1.00" {
					t.Fatalf("history=%+v", history.Output1)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != tt.path || request.Header.Get("tr_id") != tt.wantTRID {
					t.Errorf("path=%s tr_id=%s", request.URL.Path, request.Header.Get("tr_id"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(tt.payload))
			}))
			defer server.Close()
			balance, history, err := tt.call(context.Background(), testClient(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, balance, history)
		})
	}
}

func TestUSOrderRequestAssemblyWithHashKey(t *testing.T) {
	request := OrderRequest{CANO: "12345678", ACNT_PRDT_CD: "01", OVRS_EXCG_CD: "NASD", PDNO: "AAPL", ORD_QTY: "1", OVRS_ORD_UNPR: "189.50", CTAC_TLNO: "", MGCO_APTM_ODNO: "", SLL_TYPE: "", ORD_SVR_DVSN_CD: "0", ORD_DVSN: "00"}
	cancel := CancelRequest{CANO: "12345678", ACNT_PRDT_CD: "01", OVRS_EXCG_CD: "NASD", PDNO: "AAPL", ORGN_ODNO: "US-42", RVSE_CNCL_DVSN_CD: "02", ORD_QTY: "1", OVRS_ORD_UNPR: "0", MGCO_APTM_ODNO: "", ORD_SVR_DVSN_CD: "0"}
	tests := []struct {
		name, wantTRID, wantPath, wantBody string
		call                               func(context.Context, *kis.Client) error
	}{
		{name: "mock buy", wantTRID: "VTTT1002U", wantPath: orderPath, wantBody: `{"CANO":"12345678","ACNT_PRDT_CD":"01","OVRS_EXCG_CD":"NASD","PDNO":"AAPL","ORD_QTY":"1","OVRS_ORD_UNPR":"189.50","CTAC_TLNO":"","MGCO_APTM_ODNO":"","SLL_TYPE":"","ORD_SVR_DVSN_CD":"0","ORD_DVSN":"00"}`, call: func(ctx context.Context, c *kis.Client) error { _, err := Buy(ctx, c, kis.Mock, request); return err }},
		{name: "mock sell", wantTRID: "VTTT1001U", wantPath: orderPath, wantBody: `{"CANO":"12345678","ACNT_PRDT_CD":"01","OVRS_EXCG_CD":"NASD","PDNO":"AAPL","ORD_QTY":"1","OVRS_ORD_UNPR":"189.50","CTAC_TLNO":"","MGCO_APTM_ODNO":"","SLL_TYPE":"","ORD_SVR_DVSN_CD":"0","ORD_DVSN":"00"}`, call: func(ctx context.Context, c *kis.Client) error { _, err := Sell(ctx, c, kis.Mock, request); return err }},
		{name: "mock cancel", wantTRID: "VTTT1004U", wantPath: cancelPath, wantBody: `{"CANO":"12345678","ACNT_PRDT_CD":"01","OVRS_EXCG_CD":"NASD","PDNO":"AAPL","ORGN_ODNO":"US-42","RVSE_CNCL_DVSN_CD":"02","ORD_QTY":"1","OVRS_ORD_UNPR":"0","MGCO_APTM_ODNO":"","ORD_SVR_DVSN_CD":"0"}`, call: func(ctx context.Context, c *kis.Client) error { _, err := Cancel(ctx, c, kis.Mock, cancel); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hashBody string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if r.URL.Path == "/uapi/hashkey" {
					hashBody = string(body)
					_, _ = w.Write([]byte(`{"HASH":"hash-vector"}`))
					return
				}
				if r.URL.Path != tt.wantPath || r.Header.Get("tr_id") != tt.wantTRID || r.Header.Get("hashkey") != "hash-vector" {
					t.Errorf("path=%s tr_id=%s hashkey=%s", r.URL.Path, r.Header.Get("tr_id"), r.Header.Get("hashkey"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if string(body) != tt.wantBody {
					t.Errorf("body=%s", body)
				}
				_, _ = w.Write([]byte(`{"rt_cd":"0","output":{"ODNO":"US-42"}}`))
			}))
			defer server.Close()
			if err := tt.call(context.Background(), testClient(t, server.URL)); err != nil {
				t.Fatal(err)
			}
			if hashBody != tt.wantBody {
				t.Fatalf("hash payload=%s want=%s", hashBody, tt.wantBody)
			}
		})
	}
}

func TestUSLiveTransactionIDs(t *testing.T) {
	for _, tt := range []struct{ mock, live string }{{"VTTT1002U", "TTTT1002U"}, {"VTTT1001U", "TTTT1001U"}, {"VTTT1004U", "TTTT1004U"}, {"VTTS3035R", "TTTS3035R"}, {"VTTS3012R", "TTTS3012R"}} {
		got, err := kis.TransactionID(kis.Live, tt.mock, tt.live)
		if err != nil || got != tt.live {
			t.Fatalf("transaction id=%q err=%v", got, err)
		}
	}
}
