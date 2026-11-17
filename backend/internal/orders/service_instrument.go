package orders

import (
	"context"
	"errors"
	"strings"

	"github.com/fix-gateway/backend/internal/quikexport"
)

// Instruments is the QUIK Securities source of what can be traded.
type Instruments interface {
	Instrument(ctx context.Context, classCode, secCode string) (quikexport.Row, error)
}

// SetInstruments must be called before serving requests; without it no
// instrument can be confirmed and orders are refused as unavailable.
func (s *Service) SetInstruments(src Instruments) { s.instruments = src }

type orderInstrument struct {
	Symbol, Board, Currency string
}

// resolveInstrument returns the codes as QUIK stores them (FIX 48, 100) and the
// face currency (FIX 15).
func (s *Service) resolveInstrument(ctx context.Context, classCode, secCode string) (orderInstrument, error) {
	if s.instruments == nil {
		return orderInstrument{}, quikexport.ErrNotConfigured
	}
	if err := s.checkServedClass(ctx, classCode); err != nil {
		return orderInstrument{}, err
	}
	row, err := s.instruments.Instrument(ctx, classCode, secCode)
	if errors.Is(err, quikexport.ErrInstrumentNotFound) {
		return orderInstrument{}, ErrInstrumentNotFound
	}
	if err != nil {
		return orderInstrument{}, err
	}
	return orderInstrument{
		Symbol:   strings.TrimSpace(row.Text("SecCode")),
		Board:    strings.TrimSpace(row.Text("ClassCode")),
		Currency: strings.TrimSpace(row.Text("FaceUnit")),
	}, nil
}
