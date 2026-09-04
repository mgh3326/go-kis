package overseas

import (
	"testing"

	"github.com/mgh3326/go-kis/kis"
)

func TestHistoryBrokerQuery(t *testing.T) {
	r := OrderHistoryRequest{ORD_DT: "ignored", ORD_STRT_DT: "20260901", ORD_END_DT: "20260902", SLL_BUY_DVSN: "00", CCLD_NCCS_DVSN: "01", PDNO: "AAPL"}
	q := r.query(kis.Live)
	for key, want := range map[string]string{"ORD_STRT_DT": "20260901", "ORD_END_DT": "20260902", "SLL_BUY_DVSN": "00", "CCLD_NCCS_DVSN": "01", "PDNO": "AAPL", "ORD_DT": ""} {
		if q[key] != want {
			t.Fatalf("%s=%q", key, q[key])
		}
	}
}

func TestMockHistoryQueryCannotBeOverridden(t *testing.T) {
	q := (OrderHistoryRequest{OVRS_EXCG_CD: "NASD", SORT_SQN: "AS", SLL_BUY_DVSN: "02", CCLD_NCCS_DVSN: "01", PDNO: "AAPL", ODNO: "override"}).query(kis.Mock)
	for key, want := range map[string]string{"PDNO": "", "OVRS_EXCG_CD": "", "SLL_BUY_DVSN": "00", "CCLD_NCCS_DVSN": "00", "ODNO": "", "SORT_SQN": "DS"} {
		if q[key] != want {
			t.Fatalf("%s=%q want %q", key, q[key], want)
		}
	}
}

func TestLiveHistoryQueryPreservesCallerFields(t *testing.T) {
	q := (OrderHistoryRequest{OVRS_EXCG_CD: "NYSE", SORT_SQN: "AS", SLL_BUY_DVSN: "02", CCLD_NCCS_DVSN: "01", PDNO: "AAPL", ODNO: "live-order"}).query(kis.Live)
	for key, want := range map[string]string{"OVRS_EXCG_CD": "NYSE", "SLL_BUY_DVSN": "02", "CCLD_NCCS_DVSN": "01", "PDNO": "AAPL", "ODNO": "live-order", "SORT_SQN": "AS"} {
		if q[key] != want {
			t.Fatalf("%s=%q want %q", key, q[key], want)
		}
	}
}
