package ws

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Side is the buy/sell direction of an execution notice.
type Side string

const (
	// SideUnknown is reported when the field is absent or carries a code this
	// package does not recognise. The original code stays in Event.Fields and
	// Event.Raw.
	SideUnknown Side = ""
	// SideSell is the sell (ask) side: KIS codes "01", "1", "S".
	SideSell Side = "sell"
	// SideBuy is the buy (bid) side: KIS codes "02", "2", "B".
	SideBuy Side = "buy"
)

// Field indices within a decrypted execution-notice record, shared by
// TRExecutionLive and TRExecutionVTS.
const (
	idxOrderNo  = 2
	idxSide     = 4
	idxSymbol   = 8
	idxQty      = 9
	idxPrice    = 10
	idxFilledAt = 11
	idxFilled   = 13
)

// ExecutionFieldCount is the exact number of fields in a documented
// execution-notice record. Records with any other length are retained as
// events but are not interpreted as executions.
const ExecutionFieldCount = 14

// Execution is one domestic execution notice.
//
// Quantity and price stay as the strings KIS sent. A ledger reconciles against
// those exact digits, so this package never rounds them through a float; use
// QtyInt and PriceRat when a numeric form is needed.
type Execution struct {
	// OrderNo is the KIS order number the notice refers to.
	OrderNo string
	// Symbol is the domestic issue code, e.g. "005930".
	Symbol string
	// Side is the resolved buy/sell direction.
	Side Side
	// Qty is the executed quantity, verbatim.
	Qty string
	// Price is the executed unit price, verbatim.
	Price string
	// FilledAt is the execution time as sent, in HHMMSS.
	FilledAt string
	// Filled distinguishes an execution from an amend/cancel notice.
	Filled string
}

// errNotNumeric reports a field that does not parse as a number.
var errNotNumeric = errors.New("ws: field is not numeric")

// QtyInt parses Qty as an integer.
func (e *Execution) QtyInt() (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(e.Qty), 10, 64)
	if err != nil {
		return 0, errNotNumeric
	}
	return value, nil
}

// PriceRat parses Price into an exact rational. Decimal prices convert without
// the rounding a binary float would introduce.
func (e *Execution) PriceRat() (*big.Rat, error) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(e.Price))
	if !ok {
		return nil, errNotNumeric
	}
	return value, nil
}

// IsExecution reports whether tr is an execution-notice transaction. Live and
// mock share one record layout, so both parse identically.
func IsExecution(tr string) bool {
	return tr == TRExecutionLive || tr == TRExecutionVTS
}

// executionFieldCountError reports a record whose shape is not documented.
// It stays private so the public error vocabulary remains closed.
type executionFieldCountError int

func (e executionFieldCountError) Error() string {
	return fmt.Sprintf("ws: execution record has %d fields, want %d", int(e), ExecutionFieldCount)
}

// parseExecution maps an exactly documented record onto Execution. The caller
// still retains every field and the raw frame when the shape is invalid.
func parseExecution(fields []string) (*Execution, error) {
	if len(fields) != ExecutionFieldCount {
		return nil, executionFieldCountError(len(fields))
	}
	return &Execution{
		OrderNo:  field(fields, idxOrderNo),
		Symbol:   field(fields, idxSymbol),
		Side:     parseSide(field(fields, idxSide)),
		Qty:      field(fields, idxQty),
		Price:    field(fields, idxPrice),
		FilledAt: field(fields, idxFilledAt),
		Filled:   field(fields, idxFilled),
	}, nil
}

func parseSide(code string) Side {
	switch strings.TrimSpace(code) {
	case "01", "1", "S":
		return SideSell
	case "02", "2", "B":
		return SideBuy
	default:
		return SideUnknown
	}
}
