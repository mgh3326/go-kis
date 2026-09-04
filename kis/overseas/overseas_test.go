package overseas

import "testing"

func TestHistoryBrokerQuery(t *testing.T) {
	r := OrderHistoryRequest{ORD_DT: "ignored", ORD_STRT_DT: "20260901", ORD_END_DT: "20260902", SLL_BUY_DVSN: "00", CCLD_NCCS_DVSN: "01", PDNO: "AAPL"}
	q := r.query()
	for key, want := range map[string]string{"ORD_STRT_DT": "20260901", "ORD_END_DT": "20260902", "SLL_BUY_DVSN": "00", "CCLD_NCCS_DVSN": "01", "PDNO": "AAPL", "ORD_DT": ""} {
		if q[key] != want {
			t.Fatalf("%s=%q", key, q[key])
		}
	}
}
