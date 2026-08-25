package observability

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxTraceID
	ctxBag
)

// Bag is a per-request annotation bucket. Handlers mutate it; HTTP middleware reads it after ServeHTTP.
type Bag struct {
	OrderID       *uuid.UUID
	ClientOrderID string
	AccountID     string
	Instrument    string
	Side          string
	Quantity      string
	Price         string
	AdapterID     string
	ErrorCode     string
	ErrorMessage  string
	FailureStage  string
	AuthSubtype   string
	Result        string
	SkipAPILog    bool
	BrokerID      *int64
	BrokerCode    string
	BrokerName    string
	TokenID       *uuid.UUID
}

func WithIDs(ctx context.Context, requestID, traceID string) context.Context {
	ctx = context.WithValue(ctx, ctxRequestID, requestID)
	ctx = context.WithValue(ctx, ctxTraceID, traceID)
	return ctx
}

func WithBag(ctx context.Context, b *Bag) context.Context {
	return context.WithValue(ctx, ctxBag, b)
}

func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxRequestID).(string)
	return s
}

func TraceIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxTraceID).(string)
	return s
}

func BagFrom(ctx context.Context) *Bag {
	b, _ := ctx.Value(ctxBag).(*Bag)
	return b
}

func NewRequestID() string {
	return "req_" + strings.ToUpper(newULID())
}

func NewTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Crockford ULID-ish id without extra deps. 26 chars, time-sortable.
var ulidEntropy atomic.Uint64

func newULID() string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var buf [16]byte
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint16(buf[0:2], uint16(ms>>32))
	binary.BigEndian.PutUint32(buf[2:6], uint32(ms))
	n := ulidEntropy.Add(1)
	binary.BigEndian.PutUint16(buf[6:8], uint16(n))
	_, _ = rand.Read(buf[8:])
	out := make([]byte, 26)
	// 48-bit time → 10 chars, 80-bit entropy → 16 chars
	var v uint64
	v = (uint64(buf[0]) << 40) | (uint64(buf[1]) << 32) | (uint64(buf[2]) << 24) | (uint64(buf[3]) << 16) | (uint64(buf[4]) << 8) | uint64(buf[5])
	for i := 9; i >= 0; i-- {
		out[i] = alphabet[v&31]
		v >>= 5
	}
	var hi uint64
	hi = binary.BigEndian.Uint64(buf[6:14])
	lo := uint64(binary.BigEndian.Uint16(buf[14:16]))
	// 80 bits in 16 * 5
	full := (hi << 16) | lo
	_ = full
	// encode buf[6:] 10 bytes as 16 chars
	var acc uint64
	bit := 0
	pos := 10
	for i := 6; i < 16; i++ {
		acc = (acc << 8) | uint64(buf[i])
		bit += 8
		for bit >= 5 && pos < 26 {
			out[pos] = alphabet[(acc>>(bit-5))&31]
			bit -= 5
			pos++
		}
	}
	if pos < 26 {
		acc <<= uint(5 - bit)
		out[pos] = alphabet[acc&31]
	}
	return string(out)
}
