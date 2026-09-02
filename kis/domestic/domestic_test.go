package domestic

import (
	"github.com/mgh3326/go-kis/kis"
	"testing"
)

func TestTransactionIDs(t *testing.T) {
	for _, tt := range []struct {
		mode kis.Mode
		want string
	}{{kis.Mock, "VTTC8434R"}, {kis.Live, "TTTC8434R"}} {
		tr, err := kis.TransactionID(tt.mode, "VTTC8434R", "TTTC8434R")
		if err != nil || tr != tt.want {
			t.Fatalf("tr=%s err=%v", tr, err)
		}
	}
}

func TestOrderHistoryQueryCompatibility(t *testing.T) {
	r := OrderHistoryRequest{CANO: "12345678", ACNT_PRDT_CD: "01", INQR_STRT_DT: "20260901", INQR_END_DT: "20260901"}
	q := r.query()
	if q["EXCG_ID_DVSN_CD"] != "ALL" || q["SLL_BUY_DVSN_CD"] != "00" || q["INQR_DVSN"] != "00" {
		t.Fatalf("query=%v", q)
	}
}
