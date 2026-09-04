// Package overseas implements KIS overseas-stock REST transaction requests.
package overseas

import (
	"context"
	"github.com/mgh3326/go-kis/kis"
)

const (
	balancePath = "/uapi/overseas-stock/v1/trading/inquire-balance"
	historyPath = "/uapi/overseas-stock/v1/trading/inquire-ccnl"
)

type BalanceRequest struct {
	CANO           string
	ACNT_PRDT_CD   string
	OVRS_EXCG_CD   string
	TR_CRCY_CD     string
	CTX_AREA_FK200 string
	CTX_AREA_NK200 string
}
type BalanceResponse struct {
	kis.Envelope
	Output1        []BalanceItem    `json:"output1"`
	Output2        []BalanceSummary `json:"output2"`
	CTX_AREA_FK200 string           `json:"ctx_area_fk200"`
	CTX_AREA_NK200 string           `json:"ctx_area_nk200"`
}
type BalanceItem struct {
	OVRS_PDNO          string `json:"ovrs_pdno"`
	OVRS_ITEM_NAME     string `json:"ovrs_item_name"`
	OVRS_CBLN_QTY      string `json:"ovrs_cblc_qty"`
	PCHS_AVG_PRIC      string `json:"pchs_avg_pric"`
	OVRS_STCK_EVLU_AMT string `json:"ovrs_stck_evlu_amt"`
}
type BalanceSummary struct {
	FRCR_EVLU_TOT_AMT string `json:"frcr_evlu_tota"`
	OVRS_TOT_PCHS_AMT string `json:"ovrs_tot_pchs_amt"`
}

func Balance(ctx context.Context, c *kis.Client, mode kis.Mode, r BalanceRequest) (BalanceResponse, error) {
	tr, e := kis.TransactionID(mode, "VTTS3012R", "TTTS3012R")
	if e != nil {
		return BalanceResponse{}, e
	}
	var out BalanceResponse
	e = c.Read(ctx, balancePath, tr, map[string]string{"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "OVRS_EXCG_CD": v(r.OVRS_EXCG_CD, "NASD"), "TR_CRCY_CD": v(r.TR_CRCY_CD, "USD"), "CTX_AREA_FK200": r.CTX_AREA_FK200, "CTX_AREA_NK200": r.CTX_AREA_NK200}, &out)
	return out, e
}

type OrderHistoryRequest struct {
	CANO           string
	ACNT_PRDT_CD   string
	OVRS_EXCG_CD   string
	SORT_SQN       string
	ORD_STRT_DT    string
	ORD_END_DT     string
	SLL_BUY_DVSN   string
	CCLD_NCCS_DVSN string
	PDNO           string
	ORD_DT         string
	ORD_GNO_BRNO   string
	ODNO           string
	CTX_AREA_FK200 string
	CTX_AREA_NK200 string
}
type OrderHistoryResponse struct {
	kis.Envelope
	Output         []OrderHistoryItem `json:"output"`
	CTX_AREA_FK200 string             `json:"ctx_area_fk200"`
	CTX_AREA_NK200 string             `json:"ctx_area_nk200"`
}
type OrderHistoryItem struct {
	ODNO            string `json:"odno"`
	PDNO            string `json:"pdno"`
	SLL_BUY_DVSN_CD string `json:"sll_buy_dvsn_cd"`
	FT_ORD_QTY      string `json:"ft_ord_qty"`
	FT_ORD_UNPR3    string `json:"ft_ord_unpr3"`
	ORD_DT          string `json:"ord_dt"`
	ORD_TMD         string `json:"ord_tmd"`
}

func OrderHistory(ctx context.Context, c *kis.Client, mode kis.Mode, r OrderHistoryRequest) (OrderHistoryResponse, error) {
	tr, e := kis.TransactionID(mode, "VTTS3035R", "TTTS3035R")
	if e != nil {
		return OrderHistoryResponse{}, e
	}
	var out OrderHistoryResponse
	e = c.Read(ctx, historyPath, tr, r.query(mode), &out)
	return out, e
}

func (r OrderHistoryRequest) query(mode kis.Mode) map[string]string {
	query := map[string]string{"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "OVRS_EXCG_CD": v(r.OVRS_EXCG_CD, "NASD"), "SORT_SQN": v(r.SORT_SQN, "DS"), "ORD_STRT_DT": r.ORD_STRT_DT, "ORD_END_DT": r.ORD_END_DT, "SLL_BUY_DVSN": r.SLL_BUY_DVSN, "CCLD_NCCS_DVSN": r.CCLD_NCCS_DVSN, "PDNO": r.PDNO, "ORD_DT": "", "ORD_GNO_BRNO": r.ORD_GNO_BRNO, "ODNO": r.ODNO, "CTX_AREA_FK200": r.CTX_AREA_FK200, "CTX_AREA_NK200": r.CTX_AREA_NK200}
	if mode == kis.Mock {
		query["PDNO"] = ""
		query["OVRS_EXCG_CD"] = ""
		query["SLL_BUY_DVSN"] = "00"
		query["CCLD_NCCS_DVSN"] = "00"
		query["ODNO"] = ""
		query["SORT_SQN"] = "DS"
	}
	return query
}

func v(x, d string) string {
	if x == "" {
		return d
	}
	return x
}
