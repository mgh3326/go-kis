// Package overseas implements KIS overseas-stock REST transaction requests.
package overseas

import (
	"context"
	"github.com/mgh3326/go-kis/kis"
)

const (
	balancePath = "/uapi/overseas-stock/v1/trading/inquire-balance"
	historyPath = "/uapi/overseas-stock/v1/trading/inquire-ccnl"
	orderPath   = "/uapi/overseas-stock/v1/trading/order"
	cancelPath  = "/uapi/overseas-stock/v1/trading/order-rvsecncl"
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
	e = c.Do(ctx, "GET", balancePath, tr, map[string]string{"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "OVRS_EXCG_CD": v(r.OVRS_EXCG_CD, "NASD"), "TR_CRCY_CD": v(r.TR_CRCY_CD, "USD"), "CTX_AREA_FK200": r.CTX_AREA_FK200, "CTX_AREA_NK200": r.CTX_AREA_NK200}, nil, false, &out)
	return out, e
}

type OrderHistoryRequest struct {
	CANO           string
	ACNT_PRDT_CD   string
	OVRS_EXCG_CD   string
	SORT_SQN       string
	ORD_DT         string
	ORD_GNO_BRNO   string
	ODNO           string
	CTX_AREA_FK200 string
	CTX_AREA_NK200 string
}
type OrderHistoryResponse struct {
	kis.Envelope
	Output1        []OrderHistoryItem `json:"output1"`
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
	e = c.Do(ctx, "GET", historyPath, tr, map[string]string{"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "OVRS_EXCG_CD": v(r.OVRS_EXCG_CD, "NASD"), "SORT_SQN": v(r.SORT_SQN, "DS"), "ORD_DT": r.ORD_DT, "ORD_GNO_BRNO": r.ORD_GNO_BRNO, "ODNO": r.ODNO, "CTX_AREA_FK200": r.CTX_AREA_FK200, "CTX_AREA_NK200": r.CTX_AREA_NK200}, nil, false, &out)
	return out, e
}

type OrderRequest struct {
	CANO            string `json:"CANO"`
	ACNT_PRDT_CD    string `json:"ACNT_PRDT_CD"`
	OVRS_EXCG_CD    string `json:"OVRS_EXCG_CD"`
	PDNO            string `json:"PDNO"`
	ORD_QTY         string `json:"ORD_QTY"`
	OVRS_ORD_UNPR   string `json:"OVRS_ORD_UNPR"`
	CTAC_TLNO       string `json:"CTAC_TLNO"`
	MGCO_APTM_ODNO  string `json:"MGCO_APTM_ODNO"`
	SLL_TYPE        string `json:"SLL_TYPE"`
	ORD_SVR_DVSN_CD string `json:"ORD_SVR_DVSN_CD"`
	ORD_DVSN        string `json:"ORD_DVSN"`
}
type OrderResponse struct {
	kis.Envelope
	Output OrderOutput `json:"output"`
}
type OrderOutput struct {
	ODNO    string `json:"ODNO"`
	ORD_TMD string `json:"ORD_TMD"`
}

func Buy(ctx context.Context, c *kis.Client, m kis.Mode, r OrderRequest) (OrderResponse, error) {
	return order(ctx, c, m, "VTTT1002U", "TTTT1002U", r)
}
func Sell(ctx context.Context, c *kis.Client, m kis.Mode, r OrderRequest) (OrderResponse, error) {
	return order(ctx, c, m, "VTTT1001U", "TTTT1001U", r)
}
func order(ctx context.Context, c *kis.Client, m kis.Mode, mock, live string, r OrderRequest) (OrderResponse, error) {
	tr, e := kis.TransactionID(m, mock, live)
	if e != nil {
		return OrderResponse{}, e
	}
	var out OrderResponse
	e = c.Do(ctx, "POST", orderPath, tr, nil, r, true, &out)
	return out, e
}

type CancelRequest struct {
	CANO              string `json:"CANO"`
	ACNT_PRDT_CD      string `json:"ACNT_PRDT_CD"`
	OVRS_EXCG_CD      string `json:"OVRS_EXCG_CD"`
	PDNO              string `json:"PDNO"`
	ORGN_ODNO         string `json:"ORGN_ODNO"`
	RVSE_CNCL_DVSN_CD string `json:"RVSE_CNCL_DVSN_CD"`
	ORD_QTY           string `json:"ORD_QTY"`
	OVRS_ORD_UNPR     string `json:"OVRS_ORD_UNPR"`
	MGCO_APTM_ODNO    string `json:"MGCO_APTM_ODNO"`
	ORD_SVR_DVSN_CD   string `json:"ORD_SVR_DVSN_CD"`
}

func Cancel(ctx context.Context, c *kis.Client, m kis.Mode, r CancelRequest) (OrderResponse, error) {
	tr, e := kis.TransactionID(m, "VTTT1004U", "TTTT1004U")
	if e != nil {
		return OrderResponse{}, e
	}
	var out OrderResponse
	e = c.Do(ctx, "POST", cancelPath, tr, nil, r, true, &out)
	return out, e
}
func v(x, d string) string {
	if x == "" {
		return d
	}
	return x
}
