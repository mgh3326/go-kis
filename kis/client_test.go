package kis_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgh3326/go-kis/kis"
	"github.com/mgh3326/go-kis/kis/domestic"
)

func newClient(t *testing.T, host string) *kis.Client {
	t.Helper()
	c, err := kis.NewClient(kis.Config{Host: host, AppKey: "test-appkey", AppSecret: "secret-never-render", RequestTimeout: time.Second, TokenProvider: kis.TokenProviderFunc(func(context.Context) (string, error) { return "token-value", nil })})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHostRequired(t *testing.T) {
	_, err := kis.NewClient(kis.Config{AppKey: "a", AppSecret: "b", RequestTimeout: time.Second})
	if !errors.Is(err, kis.ErrHostRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestTimeoutRequired(t *testing.T) {
	_, err := kis.NewClient(kis.Config{Host: kis.HostVTS, AppKey: "a", AppSecret: "b"})
	if !errors.Is(err, kis.ErrTimeoutRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestRedirectBlocked(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) }))
	defer s.Close()
	c := newClient(t, s.URL)
	var out map[string]any
	err := c.Do(context.Background(), "GET", "/start", "TTTC8434R", nil, nil, false, &out)
	if !errors.Is(err, kis.ErrRedirectBlocked) {
		t.Fatalf("err=%v", err)
	}
}

func TestHashKeyOrderVector(t *testing.T) {
	var gotHashBody, gotOrderBody, gotHash string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/uapi/hashkey":
			gotHashBody = string(body)
			_, _ = w.Write([]byte(`{"HASH":"vector-hash"}`))
		case "/uapi/domestic-stock/v1/trading/order-cash":
			gotOrderBody, gotHash = string(body), r.Header.Get("hashkey")
			_, _ = w.Write([]byte(`{"rt_cd":"0","output":{"ODNO":"1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()
	_, err := domestic.BuyCash(context.Background(), newClient(t, s.URL), kis.Mock, domestic.CashOrderRequest{CANO: "12345678", ACNT_PRDT_CD: "01", PDNO: "005930", ORD_DVSN: "00", ORD_QTY: "1", ORD_UNPR: "70000", EXCG_ID_DVSN_CD: "KRX"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"CANO":"12345678","ACNT_PRDT_CD":"01","PDNO":"005930","ORD_DVSN":"00","ORD_QTY":"1","ORD_UNPR":"70000","EXCG_ID_DVSN_CD":"KRX"}`
	if gotHashBody != want || gotOrderBody != want || gotHash != "vector-hash" {
		t.Fatalf("hash=%q order=%q header=%q", gotHashBody, gotOrderBody, gotHash)
	}
}

func TestAPIErrorRedactsSecret(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"rt_cd":"1","msg_cd":"E","msg1":"secret-never-render rejected"}`))
	}))
	defer s.Close()
	err := newClient(t, s.URL).Do(context.Background(), "GET", "/x", "TTTC8434R", nil, nil, false, nil)
	if err == nil || strings.Contains(err.Error(), "secret-never-render") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestIssueTokenUsesOAuthStub(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"access_token":"issued-token","expires_in":3600}`))
	}))
	defer s.Close()
	issued, err := newClient(t, s.URL).IssueToken(context.Background())
	if err != nil || issued.AccessToken != "issued-token" {
		t.Fatalf("token=%+v err=%v", issued, err)
	}
	if gotMethod != "POST" || gotPath != "/oauth2/tokenP" || !strings.Contains(gotBody, `"grant_type":"client_credentials"`) {
		t.Fatalf("request %s %s %s", gotMethod, gotPath, gotBody)
	}
}

// Mutant regression: making a mutation request omit the hashkey must stay RED.
func TestMutationWithoutHashKeyIsRejected(t *testing.T) {
	err := newClient(t, "https://example.test").Do(context.Background(), "POST", "/uapi/domestic-stock/v1/trading/order-cash", "TTTC0012U", nil, map[string]string{}, false, nil)
	if !errors.Is(err, kis.ErrHashKeyRequired) {
		t.Fatalf("err=%v", err)
	}
}
