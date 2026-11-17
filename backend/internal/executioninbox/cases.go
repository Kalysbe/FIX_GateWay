package executioninbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrCaseNote     = errors.New("note must be 1–2000 characters")
	ErrCaseNotFound = errors.New("quarantine case not found")
	ErrCaseResolved = errors.New("quarantine case already resolved")
)

// Case is an operator decision to set aside one incoming report. A case is
// never deleted: resolving it records the outcome of manual reconciliation.
type Case struct {
	EntryID        uuid.UUID       `json:"entry_id"`
	EntryName      string          `json:"entry_name"`
	ReceivedAt     *time.Time      `json:"received_at"`
	AdapterID      string          `json:"adapter_id"`
	ClOrdID        string          `json:"cl_ord_id"`
	ExecID         string          `json:"exec_id"`
	ExecType       string          `json:"exec_type"`
	OrderID        *uuid.UUID      `json:"order_id"`
	Report         json.RawMessage `json:"report"`
	ApplyError     string          `json:"apply_error"`
	FailedAt       *time.Time      `json:"failed_at"`
	Attempts       int             `json:"attempts"`
	Reason         string          `json:"reason"`
	QuarantinedBy  string          `json:"quarantined_by"`
	QuarantinedAt  time.Time       `json:"quarantined_at"`
	ResolvedBy     *string         `json:"resolved_by"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	ResolutionNote *string         `json:"resolution_note"`
}

type CaseStore struct{ pool *pgxpool.Pool }

func NewCaseStore(pool *pgxpool.Pool) *CaseStore { return &CaseStore{pool: pool} }

func validNote(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && utf8.RuneCountInString(s) <= 2000
}

// Open records a case for the blocked entry. Repeating it for the same entry
// (for example after a crash before the file was moved) keeps the first case.
func (s *CaseStore) Open(ctx context.Context, id uuid.UUID, b Blocked, orderID *uuid.UUID, actor, reason string) (Case, error) {
	reason, ok := validNote(reason)
	if !ok || strings.TrimSpace(actor) == "" {
		return Case{}, ErrCaseNote
	}
	var report []byte
	var received *time.Time
	if !b.ReadError {
		var err error
		if report, err = json.Marshal(b.Entry.Report); err != nil {
			return Case{}, err
		}
		received = &b.Entry.ReceivedAt
	}
	var failedAt *time.Time
	if !b.FailedAt.IsZero() {
		failedAt = &b.FailedAt
	}
	r := b.Entry.Report
	_, err := s.pool.Exec(ctx, `INSERT INTO execution_inbox_quarantine
 (entry_id,entry_name,received_at,adapter_id,cl_ord_id,exec_id,exec_type,order_id,report,apply_error,failed_at,attempts,reason,quarantined_by)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT (entry_id) DO NOTHING`,
		id, b.Name, received, r.AdapterID, r.ClOrdID, r.ExecID, r.ExecType, orderID, report, b.Error, failedAt, b.Attempts, reason, actor)
	if err != nil {
		return Case{}, err
	}
	return s.Get(ctx, id)
}

// Exists reports whether an entry has a case, resolved or not. Such an entry
// is never applied automatically.
func (s *CaseStore) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_inbox_quarantine WHERE entry_id=$1)`, id).Scan(&ok)
	return ok, err
}

const caseColumns = `entry_id,entry_name,received_at,adapter_id,cl_ord_id,exec_id,exec_type,order_id,report,apply_error,
 failed_at,attempts,reason,quarantined_by,quarantined_at,resolved_by,resolved_at,resolution_note`

func scanCase(row pgx.Row) (Case, error) {
	var c Case
	var report []byte
	err := row.Scan(&c.EntryID, &c.EntryName, &c.ReceivedAt, &c.AdapterID, &c.ClOrdID, &c.ExecID, &c.ExecType, &c.OrderID, &report,
		&c.ApplyError, &c.FailedAt, &c.Attempts, &c.Reason, &c.QuarantinedBy, &c.QuarantinedAt, &c.ResolvedBy, &c.ResolvedAt, &c.ResolutionNote)
	if report != nil {
		c.Report = report
	}
	return c, err
}

func (s *CaseStore) Get(ctx context.Context, id uuid.UUID) (Case, error) {
	c, err := scanCase(s.pool.QueryRow(ctx, `SELECT `+caseColumns+` FROM execution_inbox_quarantine WHERE entry_id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, ErrCaseNotFound
	}
	return c, err
}

// List returns open cases first, newest first.
func (s *CaseStore) List(ctx context.Context, openOnly bool, limit int) ([]Case, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT `+caseColumns+` FROM execution_inbox_quarantine
 WHERE NOT $1 OR resolved_at IS NULL ORDER BY (resolved_at IS NULL) DESC, quarantined_at DESC LIMIT $2`, openOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Case, 0)
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Resolve closes a case after manual reconciliation. It never replays the
// report or changes orders and trades.
func (s *CaseStore) Resolve(ctx context.Context, id uuid.UUID, actor, note string) (Case, error) {
	note, ok := validNote(note)
	if !ok || strings.TrimSpace(actor) == "" {
		return Case{}, ErrCaseNote
	}
	tag, err := s.pool.Exec(ctx, `UPDATE execution_inbox_quarantine SET resolved_by=$2,resolved_at=now(),resolution_note=$3
 WHERE entry_id=$1 AND resolved_at IS NULL`, id, actor, note)
	if err != nil {
		return Case{}, err
	}
	if tag.RowsAffected() == 0 {
		c, err := s.Get(ctx, id)
		if err == nil {
			return c, ErrCaseResolved
		}
		return Case{}, err
	}
	return s.Get(ctx, id)
}
