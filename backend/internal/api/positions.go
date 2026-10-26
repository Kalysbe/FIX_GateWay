package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/shopspring/decimal"
)

type setCashBody struct {
	FirmID      string           `json:"firm_id"`
	BrokerID    *int64           `json:"broker_id"`
	ClientCode  string           `json:"client_code"`
	AccountCode string           `json:"account_code"`
	Currency    string           `json:"currency"`
	Tag         string           `json:"tag"`
	Amount      *decimal.Decimal `json:"amount"`
	OpenBal     *decimal.Decimal `json:"open_bal"`
	Limit       *decimal.Decimal `json:"limit"`
	Locked      *decimal.Decimal `json:"locked"`
}

type setInstrumentBody struct {
	FirmID      string           `json:"firm_id"`
	BrokerID    *int64           `json:"broker_id"`
	ClientCode  string           `json:"client_code"`
	AccountCode string           `json:"account_code"`
	Symbol      string           `json:"symbol"`
	SecurityID  string           `json:"security_id"`
	Board       string           `json:"board"`
	Currency    string           `json:"currency"`
	Quantity    *decimal.Decimal `json:"quantity"`
	LongQty     *decimal.Decimal `json:"long_qty"`
	ShortQty    *decimal.Decimal `json:"short_qty"`
	SettlPrice  *decimal.Decimal `json:"settl_price"`
}

type setPositionsBody struct {
	FirmID      string              `json:"firm_id"`
	BrokerID    *int64              `json:"broker_id"`
	ClientCode  string              `json:"client_code"`
	AccountCode string              `json:"account_code"`
	Cash        []setCashBody       `json:"cash"`
	Instruments []setInstrumentBody `json:"instruments"`
}

func (s *Server) deleteAdminCash(w http.ResponseWriter, r *http.Request) {
	s.handleDeleteOverride(w, r, "CASH")
}

func (s *Server) deleteAdminInstrument(w http.ResponseWriter, r *http.Request) {
	s.handleDeleteOverride(w, r, "INSTRUMENT")
}

func (s *Server) setAdminPositions(w http.ResponseWriter, r *http.Request) {
	var req setPositionsBody
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrDetails(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if len(req.Cash) == 0 && len(req.Instruments) == 0 {
		writeErr(w, http.StatusBadRequest, "укажите cash и/или instruments")
		return
	}
	firm, brokerID, err := s.resolveSetFirm(r, req.FirmID, req.BrokerID)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	var cashIn []positions.CashSet
	for _, c := range req.Cash {
		if err := requireCashAmount(c); err != nil {
			s.writePositionErr(w, err)
			return
		}
		cashIn = append(cashIn, cashFromBody(c, firm, req.ClientCode, req.AccountCode, ""))
	}
	var instrIn []positions.InstrumentSet
	for _, it := range req.Instruments {
		instrIn = append(instrIn, instrumentFromBody(it, firm, req.ClientCode, req.AccountCode, ""))
	}
	s.sendPositionMaintenance(w, r, firm, brokerID, firstNonEmpty(req.ClientCode, firstCashClient(cashIn), firstInstrClient(instrIn)), firstNonEmpty(req.AccountCode, firstCashAccount(cashIn), firstInstrAccount(instrIn)), cashIn, instrIn, "1", "2")
}

func (s *Server) setAdminCash(w http.ResponseWriter, r *http.Request) {
	var req setCashBody
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrDetails(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if err := requireCashAmount(req); err != nil {
		s.writePositionErr(w, err)
		return
	}
	firm, brokerID, err := s.resolveSetFirm(r, req.FirmID, req.BrokerID)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	in := cashFromBody(req, firm, "", "", "")
	s.sendPositionMaintenance(w, r, firm, brokerID, in.ClientCode, in.AccountCode, []positions.CashSet{in}, nil, "1", "2")
}

func (s *Server) setAdminInstrument(w http.ResponseWriter, r *http.Request) {
	var req setInstrumentBody
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrDetails(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	firm, brokerID, err := s.resolveSetFirm(r, req.FirmID, req.BrokerID)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	in := instrumentFromBody(req, firm, "", "", "")
	s.sendPositionMaintenance(w, r, firm, brokerID, in.ClientCode, in.AccountCode, nil, []positions.InstrumentSet{in}, "1", "2")
}

func (s *Server) handleDeleteOverride(w http.ResponseWriter, r *http.Request, kind string) {
	q := r.URL.Query()
	var brokerID *int64
	if raw := q.Get("broker_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			brokerID = &id
		}
	}
	firm, _, err := s.resolveSetFirm(r, q.Get("firm_id"), brokerID)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	client := strings.TrimSpace(q.Get("client_code"))
	if client == "" {
		writeErr(w, http.StatusBadRequest, "client_code обязателен")
		return
	}
	account := strings.TrimSpace(q.Get("account_code"))
	symbol := strings.TrimSpace(firstNonEmpty(q.Get("symbol"), q.Get("tag")))
	board := strings.TrimSpace(q.Get("board"))
	currency := strings.TrimSpace(q.Get("currency"))
	var cashIn []positions.CashSet
	var instrIn []positions.InstrumentSet
	if kind == "CASH" {
		if currency == "" {
			writeErr(w, http.StatusBadRequest, "currency обязателен")
			return
		}
		zero := decimal.Zero
		cashIn = []positions.CashSet{{
			FirmID: firm, ClientCode: client, AccountCode: account,
			Currency: currency, Tag: symbol, Amount: zero,
		}}
	} else {
		if symbol == "" {
			writeErr(w, http.StatusBadRequest, "symbol обязателен")
			return
		}
		qty := decimal.Zero
		instrIn = []positions.InstrumentSet{{
			FirmID: firm, ClientCode: client, AccountCode: account,
			Symbol: symbol, Board: board, Quantity: &qty,
		}}
	}
	s.sendPositionMaintenance(w, r, firm, brokerID, client, account, cashIn, instrIn, "2", "3")
}

func (s *Server) sendPositionMaintenance(w http.ResponseWriter, r *http.Request, firm string, brokerID *int64, client, account string, cash []positions.CashSet, instr []positions.InstrumentSet, updateAction, maintAction string) {
	blocks, err := positions.BlocksFromSets(cash, instr, updateAction)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	req := domain.PositionMaintainRequest{
		Firm: firm, ClientCode: client, Account: account,
		Action: maintAction, TransType: "3", Blocks: blocks,
	}
	if err := domain.CheckPositionMaintainText(req); err != nil {
		s.writePositionErr(w, invalidError(err.Error()))
		return
	}
	adapterID, reqID, sn, listed, err := s.positions.SetViaFIX(r.Context(), s.manager, s.manager, req)
	if err != nil {
		s.writePositionErr(w, err)
		return
	}
	auditAfter := map[string]any{
		"firm_id": firm, "client_code": client, "adapter_id": adapterID, "pos_req_id": reqID,
		"blocks": len(blocks), "action": maintAction,
	}
	s.writeAdminAudit(r, "POSITION_SET_FIX", "position", firm, brokerID, nil, auditAfter)
	s.hub.Broadcast("positions.updated", map[string]any{
		"source": "fix", "firm_id": firm, "adapter_id": adapterID, "pos_req_id": reqID,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "source": "fix", "firm_id": firm,
		"adapter_id": adapterID, "pos_req_id": reqID,
		"result": mapPositionsResponse(sn, listed),
	})
}

func firstCashClient(in []positions.CashSet) string {
	if len(in) > 0 {
		return in[0].ClientCode
	}
	return ""
}
func firstInstrClient(in []positions.InstrumentSet) string {
	if len(in) > 0 {
		return in[0].ClientCode
	}
	return ""
}
func firstCashAccount(in []positions.CashSet) string {
	if len(in) > 0 {
		return in[0].AccountCode
	}
	return ""
}
func firstInstrAccount(in []positions.InstrumentSet) string {
	if len(in) > 0 {
		return in[0].AccountCode
	}
	return ""
}

func cashFromBody(c setCashBody, firm, parentClient, parentAccount, updatedBy string) positions.CashSet {
	in := positions.CashSet{
		FirmID:      firm,
		ClientCode:  firstNonEmpty(c.ClientCode, parentClient),
		AccountCode: firstNonEmpty(c.AccountCode, parentAccount),
		Currency:    c.Currency,
		Tag:         c.Tag,
		OpenBal:     c.OpenBal,
		Limit:       c.Limit,
		Locked:      c.Locked,
		UpdatedBy:   updatedBy,
	}
	if c.Amount != nil {
		in.Amount = *c.Amount
	} else {
		// BuildCash allows zero; missing amount is still an explicit zero unless we reject.
		// Require amount at API layer.
	}
	return in
}

func instrumentFromBody(it setInstrumentBody, firm, parentClient, parentAccount, updatedBy string) positions.InstrumentSet {
	return positions.InstrumentSet{
		FirmID:      firm,
		ClientCode:  firstNonEmpty(it.ClientCode, parentClient),
		AccountCode: firstNonEmpty(it.AccountCode, parentAccount),
		Symbol:      it.Symbol,
		SecurityID:  it.SecurityID,
		Board:       it.Board,
		Currency:    it.Currency,
		Quantity:    it.Quantity,
		LongQty:     it.LongQty,
		ShortQty:    it.ShortQty,
		SettlPrice:  it.SettlPrice,
		UpdatedBy:   updatedBy,
	}
}

func (s *Server) resolveSetFirm(r *http.Request, firmID string, brokerID *int64) (string, *int64, error) {
	firmID = strings.TrimSpace(firmID)
	if firmID == "" && brokerID != nil {
		b, err := s.brokers.GetBroker(r.Context(), *brokerID)
		if err != nil {
			if errors.Is(err, brokers.ErrNotFound) {
				return "", brokerID, invalidError("брокер не найден")
			}
			return "", brokerID, err
		}
		firmID = strings.TrimSpace(b.Code)
	}
	if firmID == "" {
		return "", brokerID, invalidError("firm_id или broker_id обязателен")
	}
	return firmID, brokerID, nil
}

type invalidAPIError string

func (e invalidAPIError) Error() string { return string(e) }
func (e invalidAPIError) Unwrap() error { return positions.ErrInvalid }

func invalidError(msg string) error { return invalidAPIError(msg) }

func (s *Server) writePositionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, positions.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, positions.ErrInvalid):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		if _, ok := err.(invalidAPIError); ok {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.Contains(err.Error(), "отклонил") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func positionsResponseSource(sn positions.Snapshot, items []domain.FIXPosition) string {
	hasManual, hasFix := false, false
	for _, p := range items {
		if p.Source == positions.SourceManual {
			hasManual = true
		} else {
			hasFix = true
		}
	}
	switch {
	case hasManual && (hasFix || sn.ID != 0):
		return "mixed"
	case hasManual:
		return "manual"
	default:
		return "fix"
	}
}

func requireCashAmount(c setCashBody) error {
	if c.Amount == nil {
		return invalidError("amount обязателен")
	}
	return nil
}
