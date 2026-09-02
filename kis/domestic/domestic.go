// Package domestic implements KIS domestic-stock REST transaction requests.
package domestic

import (
	"context"
	"github.com/mgh3326/go-kis/kis"
)

const (
	balancePath = "/uapi/domestic-stock/v1/trading/inquire-balance"
	historyPath = "/uapi/domestic-stock/v1/trading/inquire-daily-ccld"
	orderPath   = "/uapi/domestic-stock/v1/trading/order-cash"
	cancelPath  = "/uapi/domestic-stock/v1/trading/order-rvsecncl"
)

type BalanceRequest struct {
	CANO                  string
	ACNT_PRDT_CD          string
	AFHR_FLPR_YN          string
	OFL_YN                string
	INQR_DVSN             string
	UNPR_DVSN             string
	FUND_STTL_ICLD_YN     string
	FNCG_AMT_AUTO_RDPT_YN string
	PRCS_DVSN             string
	CTX_AREA_FK100        string
	CTX_AREA_NK100        string
}
type BalanceResponse struct {
	kis.Envelope
	Output1        []BalanceItem    `json:"output1"`
	Output2        []BalanceSummary `json:"output2"`
	CTX_AREA_FK100 string           `json:"ctx_area_fk100"`
	CTX_AREA_NK100 string           `json:"ctx_area_nk100"`
}
type BalanceItem struct {
	PDNO          string `json:"pdno"`
	PRDT_NAME     string `json:"prdt_name"`
	HLDG_QTY      string `json:"hldg_qty"`
	PCHS_AVG_PRIC string `json:"pchs_avg_pric"`
	EVLU_AMT      string `json:"evlu_amt"`
	EVLU_PFLS_AMT string `json:"evlu_pfls_amt"`
}
type BalanceSummary struct {
	DNCA_TOT_AMT      string `json:"dnca_tot_amt"`
	TOT_EVLU_AMT      string `json:"tot_evlu_amt"`
	TOT_ASST_EVLU_AMT string `json:"tot_asst_evlu_amt"`
}

func (r BalanceRequest) query() map[string]string {
	return map[string]string{
		"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "AFHR_FLPR_YN": value(r.AFHR_FLPR_YN, "N"), "OFL_YN": r.OFL_YN, "INQR_DVSN": value(r.INQR_DVSN, "01"), "UNPR_DVSN": value(r.UNPR_DVSN, "01"), "FUND_STTL_ICLD_YN": value(r.FUND_STTL_ICLD_YN, "N"), "FNCG_AMT_AUTO_RDPT_YN": value(r.FNCG_AMT_AUTO_RDPT_YN, "N"), "PRCS_DVSN": value(r.PRCS_DVSN, "00"), "CTX_AREA_FK100": r.CTX_AREA_FK100, "CTX_AREA_NK100": r.CTX_AREA_NK100}
}
func Balance(ctx context.Context, client *kis.Client, mode kis.Mode, request BalanceRequest) (BalanceResponse, error) {
	tr, err := kis.TransactionID(mode, "VTTC8434R", "TTTC8434R")
	if err != nil {
		return BalanceResponse{}, err
	}
	var response BalanceResponse
	err = client.Do(ctx, "GET", balancePath, tr, request.query(), nil, false, &response)
	return response, err
}

type OrderHistoryRequest struct {
	CANO            string
	ACNT_PRDT_CD    string
	INQR_STRT_DT    string
	INQR_END_DT     string
	SLL_BUY_DVSN_CD string
	INQR_DVSN       string
	INQR_DVSN_3     string
	INQR_DVSN_1     string
	CTX_AREA_FK100  string
	CTX_AREA_NK100  string
	ORD_GNO_BRNO    string
	ODNO            string
	PDNO            string
	CCLD_DVSN       string
	EXCG_ID_DVSN_CD string
}
type OrderHistoryResponse struct {
	kis.Envelope
	Output1        []OrderHistoryItem `json:"output1"`
	CTX_AREA_FK100 string             `json:"ctx_area_fk100"`
	CTX_AREA_NK100 string             `json:"ctx_area_nk100"`
}
type OrderHistoryItem struct {
	ODNO            string `json:"odno"`
	PDNO            string `json:"pdno"`
	SLL_BUY_DVSN_CD string `json:"sll_buy_dvsn_cd"`
	ORD_QTY         string `json:"ord_qty"`
	ORD_UNPR        string `json:"ord_unpr"`
	ORD_DT          string `json:"ord_dt"`
	ORD_TMD         string `json:"ord_tmd"`
}

func (r OrderHistoryRequest) query() map[string]string {
	return map[string]string{"CANO": r.CANO, "ACNT_PRDT_CD": r.ACNT_PRDT_CD, "INQR_STRT_DT": r.INQR_STRT_DT, "INQR_END_DT": r.INQR_END_DT, "SLL_BUY_DVSN_CD": value(r.SLL_BUY_DVSN_CD, "00"), "INQR_DVSN": value(r.INQR_DVSN, "00"), "INQR_DVSN_3": value(r.INQR_DVSN_3, "00"), "INQR_DVSN_1": r.INQR_DVSN_1, "CTX_AREA_FK100": r.CTX_AREA_FK100, "CTX_AREA_NK100": r.CTX_AREA_NK100, "ORD_GNO_BRNO": r.ORD_GNO_BRNO, "ODNO": r.ODNO, "PDNO": r.PDNO, "CCLD_DVSN": value(r.CCLD_DVSN, "00"), "EXCG_ID_DVSN_CD": value(r.EXCG_ID_DVSN_CD, "ALL")}
}
func OrderHistory(ctx context.Context, client *kis.Client, mode kis.Mode, request OrderHistoryRequest) (OrderHistoryResponse, error) {
	tr, err := kis.TransactionID(mode, "VTTC8001R", "TTTC8001R")
	if err != nil {
		return OrderHistoryResponse{}, err
	}
	var response OrderHistoryResponse
	err = client.Do(ctx, "GET", historyPath, tr, request.query(), nil, false, &response)
	return response, err
}

type CashOrderRequest struct {
	CANO            string `json:"CANO"`
	ACNT_PRDT_CD    string `json:"ACNT_PRDT_CD"`
	PDNO            string `json:"PDNO"`
	ORD_DVSN        string `json:"ORD_DVSN"`
	ORD_QTY         string `json:"ORD_QTY"`
	ORD_UNPR        string `json:"ORD_UNPR"`
	EXCG_ID_DVSN_CD string `json:"EXCG_ID_DVSN_CD"`
}
type OrderResponse struct {
	kis.Envelope
	Output OrderOutput `json:"output"`
}
type OrderOutput struct {
	KRX_FWDG_ORD_ORGNO string `json:"KRX_FWDG_ORD_ORGNO"`
	ODNO               string `json:"ODNO"`
	ORD_TMD            string `json:"ORD_TMD"`
}

func BuyCash(ctx context.Context, client *kis.Client, mode kis.Mode, request CashOrderRequest) (OrderResponse, error) {
	return cash(ctx, client, mode, "VTTC0012U", "TTTC0012U", request)
}
func SellCash(ctx context.Context, client *kis.Client, mode kis.Mode, request CashOrderRequest) (OrderResponse, error) {
	return cash(ctx, client, mode, "VTTC0011U", "TTTC0011U", request)
}
func cash(ctx context.Context, client *kis.Client, mode kis.Mode, mock, live string, request CashOrderRequest) (OrderResponse, error) {
	tr, err := kis.TransactionID(mode, mock, live)
	if err != nil {
		return OrderResponse{}, err
	}
	var response OrderResponse
	err = client.Do(ctx, "POST", orderPath, tr, nil, request, true, &response)
	return response, err
}

type ReviseCancelRequest struct {
	CANO               string `json:"CANO"`
	ACNT_PRDT_CD       string `json:"ACNT_PRDT_CD"`
	KRX_FWDG_ORD_ORGNO string `json:"KRX_FWDG_ORD_ORGNO"`
	ORGN_ODNO          string `json:"ORGN_ODNO"`
	ORD_DVSN           string `json:"ORD_DVSN"`
	RVSE_CNCL_DVSN_CD  string `json:"RVSE_CNCL_DVSN_CD"`
	ORD_QTY            string `json:"ORD_QTY"`
	ORD_UNPR           string `json:"ORD_UNPR"`
	QTY_ALL_ORD_YN     string `json:"QTY_ALL_ORD_YN"`
	EXCG_ID_DVSN_CD    string `json:"EXCG_ID_DVSN_CD"`
}

func ReviseCancel(ctx context.Context, client *kis.Client, mode kis.Mode, request ReviseCancelRequest) (OrderResponse, error) {
	tr, err := kis.TransactionID(mode, "VTTC0013U", "TTTC0013U")
	if err != nil {
		return OrderResponse{}, err
	}
	var response OrderResponse
	err = client.Do(ctx, "POST", cancelPath, tr, nil, request, true, &response)
	return response, err
}
func value(given, fallback string) string {
	if given == "" {
		return fallback
	}
	return given
}
