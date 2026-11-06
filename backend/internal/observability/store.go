package observability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLogNotFound = errors.New("log event not found")

// Store keeps log events in daily files (FileLog); notifications and export
// jobs stay in the database.
type Store struct {
	pool  *pgxpool.Pool
	files *FileLog
}

func NewStore(pool *pgxpool.Pool, files *FileLog) *Store { return &Store{pool: pool, files: files} }

func (s *Store) Files() *FileLog { return s.files }

func (s *Store) InsertBatch(ctx context.Context, events []Event) error {
	if s.files == nil {
		return errors.New("log files unavailable")
	}
	return s.files.Append(events...)
}

func (s *Store) Insert(ctx context.Context, e Event) error {
	return s.InsertBatch(ctx, []Event{e})
}

// scan calls fn for every stored event of the days touching from..to, newest
// day first. fn returns false to stop.
func (s *Store) scan(from, to time.Time, fn func(*Event) bool) error {
	if s.files == nil {
		return nil
	}
	days, err := s.files.Events(from, to)
	if err != nil {
		return err
	}
	for _, events := range days {
		for i := len(events) - 1; i >= 0; i-- {
			if !fn(&events[i]) {
				return nil
			}
		}
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (Event, error) {
	var out *Event
	err := s.scan(time.Time{}, time.Time{}, func(e *Event) bool {
		if e.ID == id {
			out = e
			return false
		}
		return true
	})
	if err != nil {
		return Event{}, err
	}
	if out == nil {
		return Event{}, ErrLogNotFound
	}
	return *out, nil
}

func clampLogListLimit(n int) int {
	if n <= 0 {
		return 25
	}
	if n > 100 {
		return 100
	}
	return n
}

func newer(a, b *Event) bool {
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.After(b.Timestamp)
	}
	return strings.Compare(a.ID.String(), b.ID.String()) > 0
}

// List returns matching events newest first; the list view omits the bulky
// before/after/metadata blobs, as the database query did.
func (s *Store) List(ctx context.Context, f Filter) ([]Event, bool, error) {
	f.Limit = clampLogListLimit(f.Limit)
	var found []*Event
	err := s.scan(f.From, f.To, func(e *Event) bool {
		if f.match(e) {
			found = append(found, e)
		}
		return true
	})
	if err != nil {
		return nil, false, err
	}
	sort.SliceStable(found, func(i, j int) bool { return newer(found[i], found[j]) })
	more := len(found) > f.Limit
	if more {
		found = found[:f.Limit]
	}
	out := make([]Event, 0, len(found))
	for _, e := range found {
		c := *e
		c.Before, c.After, c.Metadata = nil, nil, nil
		out = append(out, c)
	}
	return out, more, nil
}

func (s *Store) Count(ctx context.Context, f Filter) (int64, error) {
	var n int64
	err := s.scan(f.From, f.To, func(e *Event) bool {
		if f.match(e) {
			n++
		}
		return true
	})
	return n, err
}

func (f Filter) match(e *Event) bool {
	if !f.From.IsZero() && e.Timestamp.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && e.Timestamp.After(f.To) {
		return false
	}
	if f.BrokerID != nil && (e.BrokerID == nil || *e.BrokerID != *f.BrokerID) {
		return false
	}
	if f.Type != "" && e.Type != f.Type {
		return false
	}
	if len(f.Types) > 0 {
		ok := false
		for _, t := range f.Types {
			if e.Type == t {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.Subtype != "" && e.Subtype != f.Subtype {
		return false
	}
	if f.Severity != "" && e.Severity != f.Severity {
		return false
	}
	if f.Status != "" && e.Result != f.Status {
		return false
	}
	if f.Result != "" && e.Result != f.Result {
		return false
	}
	if f.StatusCode != nil && (e.StatusCode == nil || *e.StatusCode != *f.StatusCode) {
		return false
	}
	if f.ClientIP != "" && e.ClientIP != f.ClientIP {
		return false
	}
	if f.RequestID != "" && e.RequestID != f.RequestID {
		return false
	}
	if f.TraceID != "" && e.TraceID != f.TraceID {
		return false
	}
	if f.OrderID != nil && (e.OrderID == nil || *e.OrderID != *f.OrderID) {
		return false
	}
	if f.ClientOrderID != "" && e.ClientOrderID != f.ClientOrderID {
		return false
	}
	if f.AdapterID != "" && e.AdapterID != f.AdapterID {
		return false
	}
	if f.Endpoint != "" && !strings.Contains(strings.ToLower(e.Endpoint), strings.ToLower(f.Endpoint)) {
		return false
	}
	if f.HTTPMethod != "" && e.HTTPMethod != strings.ToUpper(f.HTTPMethod) {
		return false
	}
	if f.ErrorCode != "" && e.ErrorCode != f.ErrorCode {
		return false
	}
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		cursor := Event{Timestamp: f.CursorTS, ID: f.CursorID}
		if !newer(&cursor, e) {
			return false
		}
	}
	return true
}

// ApplyRetention deletes log files older than LogKeepDays and old notifications.
func (s *Store) ApplyRetention(ctx context.Context, cfg Retention) error {
	var errs []error
	if s.files != nil {
		errs = append(errs, s.files.Cleanup(time.Now()))
	}
	if s.pool != nil {
		days := cfg.KeepDays()
		_, _ = s.pool.Exec(ctx, `DELETE FROM obs_notifications WHERE last_seen_at < now() - make_interval(days => $1)`, days)
		_, _ = s.pool.Exec(ctx, `DELETE FROM obs_export_jobs WHERE created_at < now() - interval '7 days'`)
	}
	return errors.Join(errs...)
}

// KeepDays is the notification retention; log files always keep LogKeepDays.
func (cfg Retention) KeepDays() int {
	days := cfg.APIDays
	for _, n := range []int{cfg.BusinessDays, cfg.AuthDays, cfg.SystemDays, cfg.ErrorDays, cfg.DebugDays, cfg.AuditDays} {
		if n > 0 && (days <= 0 || n < days) {
			days = n
		}
	}
	if days <= 0 {
		return 15
	}
	return days
}

type Retention struct {
	APIDays      int
	BusinessDays int
	AuthDays     int
	SystemDays   int
	ErrorDays    int
	DebugDays    int
	AuditDays    int
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Store) UpsertNotification(ctx context.Context, n Notification, window time.Duration) (Notification, bool, error) {
	if window <= 0 {
		window = time.Minute
	}
	existing, err := scanNotification(s.pool.QueryRow(ctx, `
		SELECT id, fingerprint, severity, event_type, subtype, error_code, broker_id, broker_name,
		       title, message, count, window_start, last_seen_at, read_at, created_at
		FROM obs_notifications
		WHERE fingerprint=$1 AND window_start > now() - ($2 * interval '1 millisecond')
		ORDER BY window_start DESC LIMIT 1`, n.Fingerprint, window.Milliseconds()))
	if err == nil {
		existing.Count += max(1, n.Count)
		existing.LastSeenAt = time.Now().UTC()
		existing.Severity = worseSeverity(existing.Severity, n.Severity)
		existing.Title = aggregateTitle(n, existing.Count, window)
		existing.Message = n.Message
		_, err = s.pool.Exec(ctx, `
			UPDATE obs_notifications SET count=$2, last_seen_at=$3, severity=$4, title=$5, message=$6
			WHERE id=$1`, existing.ID, existing.Count, existing.LastSeenAt, existing.Severity, existing.Title, existing.Message)
		return existing, false, err
	}
	if err != pgx.ErrNoRows {
		return n, false, err
	}
	n.ID = uuid.New()
	n.Count = max(1, n.Count)
	n.WindowStart = time.Now().UTC()
	n.LastSeenAt = n.WindowStart
	n.CreatedAt = n.WindowStart
	_, err = s.pool.Exec(ctx, `
		INSERT INTO obs_notifications (
			id, fingerprint, severity, event_type, subtype, error_code, broker_id, broker_name,
			title, message, count, window_start, last_seen_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		n.ID, n.Fingerprint, n.Severity, n.EventType, n.Subtype, n.ErrorCode, n.BrokerID, nullStr(n.BrokerName),
		n.Title, n.Message, n.Count, n.WindowStart, n.LastSeenAt,
	)
	return n, true, err
}

func worseSeverity(a, b string) string {
	rank := map[string]int{SeverityDebug: 0, SeverityInfo: 1, SeverityWarning: 2, SeverityError: 3, SeverityCritical: 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func aggregateTitle(n Notification, count int, window time.Duration) string {
	if count <= 1 {
		return n.Title
	}
	label := n.ErrorCode
	if label == "" {
		label = n.Subtype
	}
	if label == "" {
		label = n.EventType
	}
	mins := int(window.Minutes())
	if mins < 1 {
		mins = 1
	}
	unit := "минуту"
	if mins != 1 {
		unit = fmt.Sprintf("%d минут", mins)
	}
	return fmt.Sprintf("%s: %d ошибок за последнюю %s", label, count, unit)
}

func (s *Store) ListNotifications(ctx context.Context, unreadOnly bool, limit, offset int) ([]Notification, int64, int64, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	var unread, total int64
	countQ := `SELECT COUNT(*) FILTER (WHERE read_at IS NULL), COUNT(*) FROM obs_notifications`
	if unreadOnly {
		countQ = `SELECT COUNT(*), COUNT(*) FROM obs_notifications WHERE read_at IS NULL`
	}
	_ = s.pool.QueryRow(ctx, countQ).Scan(&unread, &total)
	q := `
		SELECT id, fingerprint, severity, event_type, subtype, error_code, broker_id, broker_name,
		       title, message, count, window_start, last_seen_at, read_at, created_at
		FROM obs_notifications`
	if unreadOnly {
		q += ` WHERE read_at IS NULL`
	}
	q += ` ORDER BY CASE severity WHEN 'CRITICAL' THEN 0 WHEN 'ERROR' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END, last_seen_at DESC LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, unread, total, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, unread, total, err
		}
		out = append(out, n)
	}
	return out, unread, total, rows.Err()
}

func scanNotification(row pgx.Row) (Notification, error) {
	var n Notification
	var brokerName pgtype.Text
	err := row.Scan(&n.ID, &n.Fingerprint, &n.Severity, &n.EventType, &n.Subtype, &n.ErrorCode,
		&n.BrokerID, &brokerName, &n.Title, &n.Message, &n.Count, &n.WindowStart, &n.LastSeenAt, &n.ReadAt, &n.CreatedAt)
	if brokerName.Valid {
		n.BrokerName = brokerName.String
	}
	return n, err
}

func (s *Store) MarkNotificationRead(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE obs_notifications SET read_at=now() WHERE id=$1 AND read_at IS NULL`, id)
	return err
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE obs_notifications SET read_at=now() WHERE read_at IS NULL`)
	return err
}

func (s *Store) CreateExportJob(ctx context.Context, job ExportJob) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO obs_export_jobs (id, admin_user_id, format, status, filter, file_path)
		VALUES ($1,$2,$3,$4,$5,$6)`, job.ID, nullStr(job.AdminUserID), job.Format, job.Status, job.Filter, nullStr(job.FilePath))
	return err
}

func (s *Store) UpdateExportJob(ctx context.Context, job ExportJob) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE obs_export_jobs SET status=$2, file_path=$3, row_count=$4, error=$5, completed_at=$6 WHERE id=$1`,
		job.ID, job.Status, nullStr(job.FilePath), job.RowCount, nullStr(job.Error), job.CompletedAt)
	return err
}

func (s *Store) GetExportJob(ctx context.Context, id uuid.UUID) (ExportJob, error) {
	var j ExportJob
	var path, errMsg *string
	var rows *int
	var completed *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(admin_user_id,''), format, status, filter, file_path, row_count, error, created_at, completed_at
		FROM obs_export_jobs WHERE id=$1`, id,
	).Scan(&j.ID, &j.AdminUserID, &j.Format, &j.Status, &j.Filter, &path, &rows, &errMsg, &j.CreatedAt, &completed)
	if err != nil {
		return j, err
	}
	j.FilePath = deref(path)
	j.RowCount = rows
	j.Error = deref(errMsg)
	j.CompletedAt = completed
	return j, nil
}
