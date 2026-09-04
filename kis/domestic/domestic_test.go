package domestic

import (
	"testing"
)

func TestBalanceBrokerDefaults(t *testing.T) {
	q := (BalanceRequest{}).query()
	if q["INQR_DVSN"] != "00" || q["PRCS_DVSN"] != "01" {
		t.Fatalf("query=%v", q)
	}
}
