package observability

import (
	"context"
	"time"
)

type OverviewStats struct {
	RequestsToday     int64   `json:"requests_today"`
	OrdersToday       int64   `json:"orders_today"`
	Successful        int64   `json:"successful"`
	Failed            int64   `json:"failed"`
	SuccessRate       float64 `json:"success_rate"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	P95LatencyMs      float64 `json:"p95_latency_ms"`
	P99LatencyMs      float64 `json:"p99_latency_ms"`
	ErrorsToday       int64   `json:"errors_today"`
	CriticalToday     int64   `json:"critical_today"`
	AuthFailuresToday int64   `json:"auth_failures_today"`
	ActiveBrokers     int64   `json:"active_brokers"`
	LastActivity      *string `json:"last_activity,omitempty"`
}

type BrokerStats struct {
	BrokerID     int64         `json:"broker_id"`
	Today        PeriodStats   `json:"today"`
	Week         PeriodStats   `json:"week"`
	Month        PeriodStats   `json:"month"`
	LastActivity *time.Time    `json:"last_activity,omitempty"`
	TopErrors    []ErrorCount  `json:"top_errors"`
	IPs          []IPStats     `json:"ips"`
	Series       []SeriesPoint `json:"series"`
}

type PeriodStats struct {
	Requests     int64   `json:"requests"`
	Successful   int64   `json:"successful"`
	Errors       int64   `json:"errors"`
	Orders       int64   `json:"orders"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`
}

type ErrorCount struct {
	Error           string    `json:"error"`
	Count           int64     `json:"count"`
	LastOccurrence  time.Time `json:"last_occurrence"`
	AffectedBrokers int64     `json:"affected_brokers"`
}

type IPStats struct {
	IP           string     `json:"ip"`
	Requests     int64      `json:"requests"`
	Errors       int64      `json:"errors"`
	AuthFailures int64      `json:"auth_failures"`
	LastRequest  *time.Time `json:"last_request,omitempty"`
}

type SeriesPoint struct {
	Bucket     time.Time `json:"bucket"`
	Requests   int64     `json:"requests"`
	Successful int64     `json:"successful"`
	Errors     int64     `json:"errors"`
	Orders     int64     `json:"orders"`
	AuthFails  int64     `json:"auth_failures"`
}

type TopBroker struct {
	BrokerID     int64      `json:"broker_id"`
	BrokerName   string     `json:"broker_name"`
	Requests     int64      `json:"requests"`
	Orders       int64      `json:"orders"`
	Errors       int64      `json:"errors"`
	SuccessRate  float64    `json:"success_rate"`
	AvgLatencyMs float64    `json:"avg_latency_ms"`
	LastActivity *time.Time `json:"last_activity,omitempty"`
}

// ConnectedWindow is how long a broker stays "online" after the last API request.
const ConnectedWindow = 2 * time.Minute

type ConnectedBroker struct {
	BrokerID     int64     `json:"broker_id"`
	BrokerCode   string    `json:"broker_code"`
	BrokerName   string    `json:"broker_name"`
	LastActivity time.Time `json:"last_activity"`
	LastIP       string    `json:"last_ip,omitempty"`
	Requests     int64     `json:"requests"`
}

func rate(ok, total int64) float64 {
	if total == 0 {
		return 100
	}
	return float64(ok) * 100 / float64(total)
}

func (s *Store) Overview(ctx context.Context) (OverviewStats, error) {
	var o OverviewStats
	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
			COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE'),
			COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND result='SUCCESS'),
			COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND result='FAILURE'),
			COALESCE(AVG(response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
			COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
			COUNT(*) FILTER (WHERE event_type='ERROR'),
			COUNT(*) FILTER (WHERE severity='CRITICAL'),
			COUNT(*) FILTER (WHERE event_type='AUTH' AND subtype <> 'AUTH_SUCCESS'),
			COUNT(DISTINCT broker_id) FILTER (WHERE broker_id IS NOT NULL)
		FROM obs_events
		WHERE ts >= date_trunc('day', now())`,
	).Scan(&o.RequestsToday, &o.OrdersToday, &o.Successful, &o.Failed,
		&o.AvgLatencyMs, &o.P95LatencyMs, &o.P99LatencyMs,
		&o.ErrorsToday, &o.CriticalToday, &o.AuthFailuresToday, &o.ActiveBrokers)
	if err != nil {
		return o, err
	}
	o.SuccessRate = rate(o.Successful, o.RequestsToday)
	return o, nil
}

func (s *Store) ConnectedBrokers(ctx context.Context, window time.Duration) ([]ConnectedBroker, error) {
	if window <= 0 {
		window = ConnectedWindow
	}
	from := time.Now().Add(-window)
	rows, err := s.pool.Query(ctx, `
		SELECT e.broker_id,
		       COALESCE(MAX(b.code), MAX(e.broker_code), ''),
		       COALESCE(MAX(b.name), MAX(e.broker_name), CAST(e.broker_id AS TEXT)),
		       MAX(e.ts),
		       (ARRAY_AGG(e.client_ip ORDER BY e.ts DESC) FILTER (WHERE e.client_ip IS NOT NULL AND e.client_ip <> ''))[1],
		       COUNT(*)
		FROM obs_events e
		LEFT JOIN brokers b ON b.id = e.broker_id
		WHERE e.ts >= $1 AND e.broker_id IS NOT NULL AND e.event_type = 'API_REQUEST'
		GROUP BY e.broker_id
		ORDER BY MAX(e.ts) DESC`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnectedBroker
	for rows.Next() {
		var c ConnectedBroker
		var ip *string
		if err := rows.Scan(&c.BrokerID, &c.BrokerCode, &c.BrokerName, &c.LastActivity, &ip, &c.Requests); err != nil {
			return nil, err
		}
		if ip != nil {
			c.LastIP = *ip
		}
		out = append(out, c)
	}
	if out == nil {
		out = []ConnectedBroker{}
	}
	return out, rows.Err()
}

func (s *Store) TopBrokers(ctx context.Context, from time.Time, limit int) ([]TopBroker, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT broker_id,
		       COALESCE(MAX(broker_name), ''),
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE'),
		       COUNT(*) FILTER (WHERE event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE')),
		       COALESCE(AVG(response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
		       MAX(ts)
		FROM obs_events
		WHERE ts >= $1 AND broker_id IS NOT NULL
		GROUP BY broker_id
		ORDER BY COUNT(*) FILTER (WHERE event_type='API_REQUEST') DESC
		LIMIT $2`, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopBroker
	for rows.Next() {
		var t TopBroker
		var req, fail int64
		if err := rows.Scan(&t.BrokerID, &t.BrokerName, &req, &t.Orders, &fail, &t.AvgLatencyMs, &t.LastActivity); err != nil {
			return nil, err
		}
		t.Requests = req
		t.Errors = fail
		t.SuccessRate = rate(req-fail, req)
		if t.SuccessRate < 0 {
			t.SuccessRate = 0
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) TopErrors(ctx context.Context, from time.Time, limit int) ([]ErrorCount, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(error_code,''), subtype) AS err,
		       COUNT(*),
		       MAX(ts),
		       COUNT(DISTINCT broker_id)
		FROM obs_events
		WHERE ts >= $1 AND (event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE') OR (event_type='AUTH' AND subtype <> 'AUTH_SUCCESS'))
		  AND COALESCE(NULLIF(error_code,''), subtype) <> ''
		GROUP BY 1
		ORDER BY COUNT(*) DESC
		LIMIT $2`, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErrorCount
	for rows.Next() {
		var e ErrorCount
		if err := rows.Scan(&e.Error, &e.Count, &e.LastOccurrence, &e.AffectedBrokers); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) BrokerStatistics(ctx context.Context, brokerID int64, seriesFrom time.Time, trunc string) (BrokerStats, error) {
	st := BrokerStats{BrokerID: brokerID}
	load := func(from time.Time) (PeriodStats, error) {
		var p PeriodStats
		err := s.pool.QueryRow(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
				COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND result='SUCCESS'),
				COUNT(*) FILTER (WHERE event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE')),
				COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE'),
				COALESCE(AVG(response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
				COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0),
				COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0)
			FROM obs_events
			WHERE broker_id=$1 AND ts >= $2`, brokerID, from,
		).Scan(&p.Requests, &p.Successful, &p.Errors, &p.Orders, &p.AvgLatencyMs, &p.P95LatencyMs, &p.P99LatencyMs)
		p.SuccessRate = rate(p.Successful, p.Requests)
		return p, err
	}
	now := time.Now()
	var err error
	if st.Today, err = load(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())); err != nil {
		return st, err
	}
	if st.Week, err = load(now.Add(-7 * 24 * time.Hour)); err != nil {
		return st, err
	}
	if st.Month, err = load(now.Add(-30 * 24 * time.Hour)); err != nil {
		return st, err
	}
	_ = s.pool.QueryRow(ctx, `SELECT MAX(ts) FROM obs_events WHERE broker_id=$1`, brokerID).Scan(&st.LastActivity)

	errRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(error_code,''), subtype), COUNT(*), MAX(ts), 1
		FROM obs_events
		WHERE broker_id=$1 AND ts >= $2 AND (event_type='ERROR' OR result='FAILURE')
		  AND COALESCE(NULLIF(error_code,''), subtype) <> ''
		GROUP BY 1 ORDER BY COUNT(*) DESC LIMIT 8`, brokerID, now.Add(-30*24*time.Hour))
	if err != nil {
		return st, err
	}
	for errRows.Next() {
		var e ErrorCount
		if err := errRows.Scan(&e.Error, &e.Count, &e.LastOccurrence, &e.AffectedBrokers); err != nil {
			errRows.Close()
			return st, err
		}
		st.TopErrors = append(st.TopErrors, e)
	}
	errRows.Close()

	ipRows, err := s.pool.Query(ctx, `
		SELECT client_ip,
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
		       COUNT(*) FILTER (WHERE event_type='ERROR' OR result='FAILURE'),
		       COUNT(*) FILTER (WHERE event_type='AUTH' AND subtype <> 'AUTH_SUCCESS'),
		       MAX(ts)
		FROM obs_events
		WHERE broker_id=$1 AND client_ip IS NOT NULL AND ts >= $2
		GROUP BY client_ip
		ORDER BY COUNT(*) DESC
		LIMIT 20`, brokerID, now.Add(-30*24*time.Hour))
	if err != nil {
		return st, err
	}
	for ipRows.Next() {
		var ip IPStats
		if err := ipRows.Scan(&ip.IP, &ip.Requests, &ip.Errors, &ip.AuthFailures, &ip.LastRequest); err != nil {
			ipRows.Close()
			return st, err
		}
		st.IPs = append(st.IPs, ip)
	}
	ipRows.Close()

	if trunc == "" {
		trunc = "hour"
	}
	if seriesFrom.IsZero() {
		seriesFrom = now.Add(-24 * time.Hour)
	}
	srows, err := s.pool.Query(ctx, `
		SELECT date_trunc($3, ts) AS bucket,
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND result='SUCCESS'),
		       COUNT(*) FILTER (WHERE event_type='ERROR' OR (event_type='API_REQUEST' AND result='FAILURE')),
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND subtype='ORDER_CREATE'),
		       COUNT(*) FILTER (WHERE event_type='AUTH' AND subtype <> 'AUTH_SUCCESS')
		FROM obs_events
		WHERE broker_id=$1 AND ts >= $2
		GROUP BY 1 ORDER BY 1`, brokerID, seriesFrom, trunc)
	if err != nil {
		return st, err
	}
	defer srows.Close()
	for srows.Next() {
		var p SeriesPoint
		if err := srows.Scan(&p.Bucket, &p.Requests, &p.Successful, &p.Errors, &p.Orders, &p.AuthFails); err != nil {
			return st, err
		}
		st.Series = append(st.Series, p)
	}
	return st, srows.Err()
}

func (s *Store) BrokerErrorRate(ctx context.Context, brokerID int64, window time.Duration) (total, failed int64, avgMs float64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE event_type='API_REQUEST'),
		       COUNT(*) FILTER (WHERE event_type='API_REQUEST' AND result='FAILURE'),
		       COALESCE(AVG(response_time_ms) FILTER (WHERE event_type='API_REQUEST' AND response_time_ms IS NOT NULL), 0)
		FROM obs_events
		WHERE broker_id=$1 AND ts >= now() - ($2 * interval '1 millisecond')`,
		brokerID, window.Milliseconds(),
	).Scan(&total, &failed, &avgMs)
	return
}

func (s *Store) DistinctBrokersSince(ctx context.Context, from time.Time) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT broker_id FROM obs_events WHERE broker_id IS NOT NULL AND ts >= $1`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
