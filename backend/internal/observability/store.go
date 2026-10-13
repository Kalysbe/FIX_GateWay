package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const insertSQL = `
INSERT INTO obs_events (
	id, ts, event_type, subtype, severity, request_id, trace_id,
	broker_id, broker_code, broker_name, token_id,
	client_ip, forwarded_for, user_agent, http_method, endpoint, route, query_params, protocol,
	status_code, response_time_ms, request_size, response_size, perf_class, result,
	order_id, client_order_id, account_id, instrument, side, quantity, price,
	adapter_id, session_id, fix_msg_type, fix_direction, fix_seq_num,
	error_code, error_message, failure_stage, message,
	actor_type, actor_id, target_type, target_id, before_state, after_state, metadata
) VALUES (
	$1,$2,$3,$4,$5,$6,$7,
	$8,$9,$10,$11,
	$12,$13,$14,$15,$16,$17,$18,$19,
	$20,$21,$22,$23,$24,$25,
	$26,$27,$28,$29,$30,$31,$32,
	$33,$34,$35,$36,$37,
	$38,$39,$40,$41,
	$42,$43,$44,$45,$46,$47,$48
)`

func (s *Store) InsertBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i := range events {
		e := &events[i]
		e.Normalize()
		batch.Queue(insertSQL, args(e)...)
	}
	br := s.pool.SendBatch(ctx, batch)
	return br.Close()
}

func (s *Store) Insert(ctx context.Context, e Event) error {
	e.Normalize()
	_, err := s.pool.Exec(ctx, insertSQL, args(&e)...)
	return err
}

func args(e *Event) []any {
	return []any{
		e.ID, e.Timestamp, e.Type, e.Subtype, e.Severity, nullStr(e.RequestID), nullStr(e.TraceID),
		e.BrokerID, nullStr(e.BrokerCode), nullStr(e.BrokerName), e.TokenID,
		nullStr(e.ClientIP), nullStr(e.ForwardedFor), nullStr(e.UserAgent),
		nullStr(e.HTTPMethod), nullStr(e.Endpoint), nullStr(e.Route), nullStr(e.QueryParams), nullStr(e.Protocol),
		e.StatusCode, e.ResponseTimeMs, e.RequestSize, e.ResponseSize, nullStr(e.PerfClass), nullStr(e.Result),
		e.OrderID, nullStr(e.ClientOrderID), nullStr(e.AccountID), nullStr(e.Instrument), nullStr(e.Side), nullStr(e.Quantity), nullStr(e.Price),
		nullStr(e.AdapterID), nullStr(e.SessionID), nullStr(e.FIXMsgType), nullStr(e.FIXDirection), e.FIXSeqNum,
		nullStr(e.ErrorCode), nullStr(e.ErrorMessage), nullStr(e.FailureStage), nullStr(e.Message),
		nullStr(e.ActorType), nullStr(e.ActorID), nullStr(e.TargetType), nullStr(e.TargetID),
		nullRaw(e.Before), nullRaw(e.After), nullRaw(e.Metadata),
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullRaw(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (Event, error) {
	row := s.pool.QueryRow(ctx, eventSelect+` WHERE id=$1 LIMIT 1`, id)
	return scanEvent(row)
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

func (s *Store) List(ctx context.Context, f Filter) ([]Event, bool, error) {
	f.Limit = clampLogListLimit(f.Limit)
	where, args := f.where(1)
	q := eventListSelect + where + ` ORDER BY ts DESC, id DESC LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, f.Limit+1)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []Event{}
	}
	return out, more, nil
}

func (s *Store) Count(ctx context.Context, f Filter) (int64, error) {
	where, args := f.where(1)
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM obs_events`+where, args...).Scan(&n)
	return n, err
}

func (f Filter) where(start int) (string, []any) {
	var b strings.Builder
	var args []any
	i := start
	add := func(clause string, v any) {
		if b.Len() == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		b.WriteString(strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(i)))
		args = append(args, v)
		i++
	}
	if !f.From.IsZero() {
		add("ts >= ?", f.From)
	}
	if !f.To.IsZero() {
		add("ts <= ?", f.To)
	}
	if f.BrokerID != nil {
		add("broker_id = ?", *f.BrokerID)
	}
	if f.Type != "" {
		add("event_type = ?", f.Type)
	}
	if len(f.Types) > 0 {
		if b.Len() == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		b.WriteString("event_type = ANY($")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(")")
		args = append(args, f.Types)
		i++
	}
	if f.Subtype != "" {
		add("subtype = ?", f.Subtype)
	}
	if f.Severity != "" {
		add("severity = ?", f.Severity)
	}
	if f.Status != "" {
		add("result = ?", f.Status)
	}
	if f.Result != "" {
		add("result = ?", f.Result)
	}
	if f.StatusCode != nil {
		add("status_code = ?", *f.StatusCode)
	}
	if f.ClientIP != "" {
		add("client_ip = ?", f.ClientIP)
	}
	if f.RequestID != "" {
		add("request_id = ?", f.RequestID)
	}
	if f.TraceID != "" {
		add("trace_id = ?", f.TraceID)
	}
	if f.OrderID != nil {
		add("order_id = ?", *f.OrderID)
	}
	if f.ClientOrderID != "" {
		add("client_order_id = ?", f.ClientOrderID)
	}
	if f.AdapterID != "" {
		add("adapter_id = ?", f.AdapterID)
	}
	if f.Endpoint != "" {
		add("endpoint ILIKE ?", "%"+f.Endpoint+"%")
	}
	if f.HTTPMethod != "" {
		add("http_method = ?", strings.ToUpper(f.HTTPMethod))
	}
	if f.ErrorCode != "" {
		add("error_code = ?", f.ErrorCode)
	}
	if !f.CursorTS.IsZero() && f.CursorID != uuid.Nil {
		if b.Len() == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		b.WriteString("(ts, id) < ($")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(", $")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(")")
		args = append(args, f.CursorTS, f.CursorID)
		i += 2
	}
	return b.String(), args
}

const eventSelect = `
SELECT id, ts, event_type, subtype, severity, request_id, trace_id,
       broker_id, broker_code, broker_name, token_id,
       client_ip, forwarded_for, user_agent, http_method, endpoint, route, query_params, protocol,
       status_code, response_time_ms, request_size, response_size, perf_class, result,
       order_id, client_order_id, account_id, instrument, side, quantity, price,
       adapter_id, session_id, fix_msg_type, fix_direction, fix_seq_num,
       error_code, error_message, failure_stage, message,
       actor_type, actor_id, target_type, target_id, before_state, after_state, metadata, ts
FROM obs_events`

// eventListSelect omits bulky JSON blobs so the logs table can page cheaply.
const eventListSelect = `
SELECT id, ts, event_type, subtype, severity, request_id, trace_id,
       broker_id, broker_code, broker_name, token_id,
       client_ip, forwarded_for, user_agent, http_method, endpoint, route, query_params, protocol,
       status_code, response_time_ms, request_size, response_size, perf_class, result,
       order_id, client_order_id, account_id, instrument, side, quantity, price,
       adapter_id, session_id, fix_msg_type, fix_direction, fix_seq_num,
       error_code, error_message, failure_stage, message,
       actor_type, actor_id, target_type, target_id, NULL::jsonb, NULL::jsonb, NULL::jsonb, ts
FROM obs_events`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	var (
		reqID, trace, bCode, bName, ip, xff, ua, method, ep, route, qp, proto *string
		perf, result, coid, acct, instr, side, qty, px                        *string
		adapter, sess, msgType, dir, errCode, errMsg, stage, msg              *string
		actorT, actorID, tgtT, tgtID                                          *string
		before, after, meta                                                   []byte
		tokenID, orderID                                                      *uuid.UUID
		created                                                               time.Time
	)
	err := row.Scan(
		&e.ID, &e.Timestamp, &e.Type, &e.Subtype, &e.Severity, &reqID, &trace,
		&e.BrokerID, &bCode, &bName, &tokenID,
		&ip, &xff, &ua, &method, &ep, &route, &qp, &proto,
		&e.StatusCode, &e.ResponseTimeMs, &e.RequestSize, &e.ResponseSize, &perf, &result,
		&orderID, &coid, &acct, &instr, &side, &qty, &px,
		&adapter, &sess, &msgType, &dir, &e.FIXSeqNum,
		&errCode, &errMsg, &stage, &msg,
		&actorT, &actorID, &tgtT, &tgtID, &before, &after, &meta, &created,
	)
	if err != nil {
		return e, err
	}
	e.RequestID = deref(reqID)
	e.TraceID = deref(trace)
	e.BrokerCode = deref(bCode)
	e.BrokerName = deref(bName)
	e.TokenID = tokenID
	e.ClientIP = deref(ip)
	e.ForwardedFor = deref(xff)
	e.UserAgent = deref(ua)
	e.HTTPMethod = deref(method)
	e.Endpoint = deref(ep)
	e.Route = deref(route)
	e.QueryParams = deref(qp)
	e.Protocol = deref(proto)
	e.PerfClass = deref(perf)
	e.Result = deref(result)
	e.OrderID = orderID
	e.ClientOrderID = deref(coid)
	e.AccountID = deref(acct)
	e.Instrument = deref(instr)
	e.Side = deref(side)
	e.Quantity = deref(qty)
	e.Price = deref(px)
	e.AdapterID = deref(adapter)
	e.SessionID = deref(sess)
	e.FIXMsgType = deref(msgType)
	e.FIXDirection = deref(dir)
	e.ErrorCode = deref(errCode)
	e.ErrorMessage = deref(errMsg)
	e.FailureStage = deref(stage)
	e.Message = deref(msg)
	e.ActorType = deref(actorT)
	e.ActorID = deref(actorID)
	e.TargetType = deref(tgtT)
	e.TargetID = deref(tgtID)
	e.Before = before
	e.After = after
	e.Metadata = meta
	e.CreatedAt = created
	return e, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Store) EnsurePartitions(ctx context.Context) error {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	for i := 0; i < 4; i++ {
		d := start.AddDate(0, i, 0)
		next := d.AddDate(0, 1, 0)
		name := fmt.Sprintf("obs_events_%s", d.Format("2006_01"))
		_, err := s.pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF obs_events FOR VALUES FROM ('%s') TO ('%s')`,
			pgIdent(name), d.Format("2006-01-02"), next.Format("2006-01-02"),
		))
		if err != nil && !strings.Contains(err.Error(), "overlaps") {
			return err
		}
	}
	return nil
}

func pgIdent(name string) string {
	if len(name) == 0 {
		return name
	}
	for _, c := range name {
		if !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
	}
	return name
}

func (s *Store) ApplyRetention(ctx context.Context, cfg Retention) error {
	days := cfg.KeepDays()
	if days <= 0 {
		days = 15
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM obs_events WHERE ts < now() - make_interval(days => $1)`, days); err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM obs_notifications WHERE last_seen_at < now() - make_interval(days => $1)`, days)
	_, _ = s.pool.Exec(ctx, `DELETE FROM obs_export_jobs WHERE created_at < now() - interval '7 days'`)
	_, _ = s.pool.Exec(ctx, `DELETE FROM audit_logs WHERE created_at < now() - make_interval(days => $1)`, days)
	_, _ = s.pool.Exec(ctx, `DELETE FROM api_logs WHERE created_at < now() - make_interval(days => $1)`, days)
	return nil
}

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
