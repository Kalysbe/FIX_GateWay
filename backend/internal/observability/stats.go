package observability

import (
	"context"
	"math"
	"sort"
	"strconv"
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

func isAPI(e *Event) bool         { return e.Type == TypeAPI }
func isOrderCreate(e *Event) bool { return e.Type == TypeAPI && e.Subtype == SubOrderCreate }
func isAuthFailure(e *Event) bool { return e.Type == TypeAuth && e.Subtype != AuthSuccess }
func isFailure(e *Event) bool {
	return e.Type == TypeError || (e.Type == TypeAPI && e.Result == ResultFailure)
}
func errorLabel(e *Event) string {
	if e.ErrorCode != "" {
		return e.ErrorCode
	}
	return e.Subtype
}

// percentile is PostgreSQL percentile_cont over sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

type latency struct{ values []float64 }

func (l *latency) add(e *Event) {
	if e.Type == TypeAPI && e.ResponseTimeMs != nil {
		l.values = append(l.values, float64(*e.ResponseTimeMs))
	}
}

func (l *latency) stats() (avg, p95, p99 float64) {
	if len(l.values) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(l.values)
	var sum float64
	for _, v := range l.values {
		sum += v
	}
	return sum / float64(len(l.values)), percentile(l.values, 0.95), percentile(l.values, 0.99)
}

// StartOfDay is the start of the current Bishkek day, the day of today's log file.
func StartOfDay(now time.Time) time.Time {
	t := now.In(LogZone)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, LogZone)
}

func (s *Store) since(from time.Time, fn func(*Event)) error {
	return s.scan(from, time.Time{}, func(e *Event) bool {
		if !e.Timestamp.Before(from) {
			fn(e)
		}
		return true
	})
}

func (s *Store) Overview(ctx context.Context) (OverviewStats, error) {
	var o OverviewStats
	var lat latency
	brokers := map[int64]bool{}
	err := s.since(StartOfDay(time.Now()), func(e *Event) {
		if isAPI(e) {
			o.RequestsToday++
			if e.Result == ResultSuccess {
				o.Successful++
			}
			if e.Result == ResultFailure {
				o.Failed++
			}
		}
		if isOrderCreate(e) {
			o.OrdersToday++
		}
		lat.add(e)
		if e.Type == TypeError {
			o.ErrorsToday++
		}
		if e.Severity == SeverityCritical {
			o.CriticalToday++
		}
		if isAuthFailure(e) {
			o.AuthFailuresToday++
		}
		if e.BrokerID != nil {
			brokers[*e.BrokerID] = true
		}
	})
	o.AvgLatencyMs, o.P95LatencyMs, o.P99LatencyMs = lat.stats()
	o.ActiveBrokers = int64(len(brokers))
	o.SuccessRate = rate(o.Successful, o.RequestsToday)
	return o, err
}

// RecentAPIBrokers summarises API requests of the last window per broker.
func (s *Store) RecentAPIBrokers(window time.Duration) (map[int64]ConnectedBroker, error) {
	if window <= 0 {
		window = ConnectedWindow
	}
	out := map[int64]ConnectedBroker{}
	err := s.since(time.Now().Add(-window), func(e *Event) {
		if e.BrokerID == nil || e.Type != TypeAPI {
			return
		}
		c := out[*e.BrokerID]
		c.BrokerID = *e.BrokerID
		c.Requests++
		if e.BrokerCode != "" && c.BrokerCode == "" {
			c.BrokerCode = e.BrokerCode
		}
		if e.BrokerName != "" && c.BrokerName == "" {
			c.BrokerName = e.BrokerName
		}
		if e.Timestamp.After(c.LastActivity) {
			c.LastActivity = e.Timestamp
			if e.ClientIP != "" {
				c.LastIP = e.ClientIP
			}
		}
		out[c.BrokerID] = c
	})
	return out, err
}

func (s *Store) ConnectedBrokers(ctx context.Context, window time.Duration) ([]ConnectedBroker, error) {
	if window <= 0 {
		window = ConnectedWindow
	}
	api, err := s.RecentAPIBrokers(window)
	if err != nil {
		return nil, err
	}
	// keepEvent does not log successful GETs, so a broker that only reads
	// (market data, positions, trades) never has API_REQUEST lines. Every
	// authenticated REST or gRPC call refreshes api_tokens.last_used_at (at most
	// every 45s), which is therefore the presence signal; API lines add counts.
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.code, b.name, t.last_activity, t.last_ip
		FROM brokers b
		LEFT JOIN LATERAL (
			SELECT MAX(last_used_at) AS last_activity,
			       (ARRAY_AGG(last_used_ip ORDER BY last_used_at DESC) FILTER (WHERE last_used_ip IS NOT NULL AND last_used_ip <> ''))[1] AS last_ip
			FROM api_tokens WHERE broker_id = b.id AND last_used_at >= $1 AND status = 'ACTIVE'
		) t ON true
		WHERE t.last_activity IS NOT NULL OR b.id = ANY($2)`, time.Now().Add(-window), brokerIDs(api))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectedBroker{}
	for rows.Next() {
		var c ConnectedBroker
		var tokenAt *time.Time
		var tokenIP *string
		if err := rows.Scan(&c.BrokerID, &c.BrokerCode, &c.BrokerName, &tokenAt, &tokenIP); err != nil {
			return nil, err
		}
		a, logged := api[c.BrokerID]
		delete(api, c.BrokerID)
		c.Requests = a.Requests
		c.LastActivity, c.LastIP = a.LastActivity, a.LastIP
		if tokenAt != nil && (!logged || tokenAt.After(a.LastActivity)) {
			c.LastActivity = *tokenAt
			if tokenIP != nil {
				c.LastIP = *tokenIP
			}
		}
		if c.LastIP == "" && tokenIP != nil {
			c.LastIP = *tokenIP
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Brokers deleted since keep their logged name.
	for _, a := range api {
		if a.BrokerName == "" {
			a.BrokerName = fmtID(a.BrokerID)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastActivity.Equal(out[j].LastActivity) {
			return out[i].LastActivity.After(out[j].LastActivity)
		}
		return out[i].BrokerID < out[j].BrokerID
	})
	return out, nil
}

func brokerIDs(m map[int64]ConnectedBroker) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	return ids
}

type brokerAgg struct {
	t   TopBroker
	ok  int64
	lat latency
}

func (s *Store) TopBrokers(ctx context.Context, from time.Time, limit int) ([]TopBroker, error) {
	if limit <= 0 {
		limit = 10
	}
	agg := map[int64]*brokerAgg{}
	err := s.since(from, func(e *Event) {
		if e.BrokerID == nil {
			return
		}
		a := agg[*e.BrokerID]
		if a == nil {
			a = &brokerAgg{t: TopBroker{BrokerID: *e.BrokerID}}
			agg[*e.BrokerID] = a
		}
		if a.t.BrokerName == "" {
			a.t.BrokerName = e.BrokerName
		}
		if isAPI(e) {
			a.t.Requests++
			if e.Result == ResultSuccess {
				a.ok++
			}
		}
		if isOrderCreate(e) {
			a.t.Orders++
		}
		if isFailure(e) {
			a.t.Errors++
		}
		a.lat.add(e)
		if a.t.LastActivity == nil || e.Timestamp.After(*a.t.LastActivity) {
			ts := e.Timestamp
			a.t.LastActivity = &ts
		}
	})
	if err != nil {
		return nil, err
	}
	out := make([]TopBroker, 0, len(agg))
	for _, a := range agg {
		a.t.AvgLatencyMs, _, _ = a.lat.stats()
		a.t.SuccessRate = rate(a.ok, a.t.Requests)
		out = append(out, a.t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].BrokerID < out[j].BrokerID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type errorAgg struct {
	c       ErrorCount
	brokers map[int64]bool
}

func collectErrors(m map[string]*errorAgg, e *Event) {
	label := errorLabel(e)
	if label == "" {
		return
	}
	a := m[label]
	if a == nil {
		a = &errorAgg{c: ErrorCount{Error: label}, brokers: map[int64]bool{}}
		m[label] = a
	}
	a.c.Count++
	if e.Timestamp.After(a.c.LastOccurrence) {
		a.c.LastOccurrence = e.Timestamp
	}
	if e.BrokerID != nil {
		a.brokers[*e.BrokerID] = true
	}
}

func topErrors(m map[string]*errorAgg, limit int) []ErrorCount {
	out := make([]ErrorCount, 0, len(m))
	for _, a := range m {
		a.c.AffectedBrokers = int64(len(a.brokers))
		out = append(out, a.c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Error < out[j].Error
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) TopErrors(ctx context.Context, from time.Time, limit int) ([]ErrorCount, error) {
	if limit <= 0 {
		limit = 10
	}
	m := map[string]*errorAgg{}
	err := s.since(from, func(e *Event) {
		if isFailure(e) || isAuthFailure(e) {
			collectErrors(m, e)
		}
	})
	return topErrors(m, limit), err
}

type periodAgg struct {
	p   PeriodStats
	lat latency
}

func (a *periodAgg) add(e *Event) {
	if isAPI(e) {
		a.p.Requests++
		if e.Result == ResultSuccess {
			a.p.Successful++
		}
	}
	if isFailure(e) {
		a.p.Errors++
	}
	if isOrderCreate(e) {
		a.p.Orders++
	}
	a.lat.add(e)
}

func (a *periodAgg) result() PeriodStats {
	a.p.AvgLatencyMs, a.p.P95LatencyMs, a.p.P99LatencyMs = a.lat.stats()
	a.p.SuccessRate = rate(a.p.Successful, a.p.Requests)
	return a.p
}

func truncate(t time.Time, unit string) time.Time {
	t = t.In(LogZone)
	switch unit {
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, LogZone)
	case "minute":
		return t.Truncate(time.Minute)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, LogZone)
	}
}

// BrokerStatistics covers at most the LogKeepDays days still on disk: "month"
// is the same as the whole log.
func (s *Store) BrokerStatistics(ctx context.Context, brokerID int64, seriesFrom time.Time, trunc string) (BrokerStats, error) {
	st := BrokerStats{BrokerID: brokerID}
	now := time.Now()
	today, week, month := StartOfDay(now), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)
	if trunc == "" {
		trunc = "hour"
	}
	if seriesFrom.IsZero() {
		seriesFrom = now.Add(-24 * time.Hour)
	}
	var dayAgg, weekAgg, monthAgg periodAgg
	errs := map[string]*errorAgg{}
	ips := map[string]*IPStats{}
	series := map[time.Time]*SeriesPoint{}
	err := s.since(month, func(e *Event) {
		if e.BrokerID == nil || *e.BrokerID != brokerID {
			return
		}
		if st.LastActivity == nil || e.Timestamp.After(*st.LastActivity) {
			ts := e.Timestamp
			st.LastActivity = &ts
		}
		monthAgg.add(e)
		if !e.Timestamp.Before(week) {
			weekAgg.add(e)
		}
		if !e.Timestamp.Before(today) {
			dayAgg.add(e)
		}
		if e.Type == TypeError || e.Result == ResultFailure {
			collectErrors(errs, e)
		}
		if e.ClientIP != "" {
			ip := ips[e.ClientIP]
			if ip == nil {
				ip = &IPStats{IP: e.ClientIP}
				ips[e.ClientIP] = ip
			}
			if isAPI(e) {
				ip.Requests++
			}
			if e.Type == TypeError || e.Result == ResultFailure {
				ip.Errors++
			}
			if isAuthFailure(e) {
				ip.AuthFailures++
			}
			if ip.LastRequest == nil || e.Timestamp.After(*ip.LastRequest) {
				ts := e.Timestamp
				ip.LastRequest = &ts
			}
		}
		if !e.Timestamp.Before(seriesFrom) {
			b := truncate(e.Timestamp, trunc)
			p := series[b]
			if p == nil {
				p = &SeriesPoint{Bucket: b}
				series[b] = p
			}
			if isAPI(e) {
				p.Requests++
				if e.Result == ResultSuccess {
					p.Successful++
				}
			}
			if isFailure(e) {
				p.Errors++
			}
			if isOrderCreate(e) {
				p.Orders++
			}
			if isAuthFailure(e) {
				p.AuthFails++
			}
		}
	})
	if err != nil {
		return st, err
	}
	st.Today, st.Week, st.Month = dayAgg.result(), weekAgg.result(), monthAgg.result()
	st.TopErrors = topErrors(errs, 8)
	for i := range st.TopErrors {
		st.TopErrors[i].AffectedBrokers = 1
	}
	for _, ip := range ips {
		st.IPs = append(st.IPs, *ip)
	}
	sort.Slice(st.IPs, func(i, j int) bool { return st.IPs[i].Requests > st.IPs[j].Requests })
	if len(st.IPs) > 20 {
		st.IPs = st.IPs[:20]
	}
	for _, p := range series {
		st.Series = append(st.Series, *p)
	}
	sort.Slice(st.Series, func(i, j int) bool { return st.Series[i].Bucket.Before(st.Series[j].Bucket) })
	return st, nil
}

// BrokerErrorRates returns API request totals per broker over the window.
func (s *Store) BrokerErrorRates(window time.Duration) (map[int64][3]float64, error) {
	type agg struct {
		total, failed int64
		lat           latency
	}
	m := map[int64]*agg{}
	err := s.since(time.Now().Add(-window), func(e *Event) {
		if e.BrokerID == nil || e.Type != TypeAPI {
			return
		}
		a := m[*e.BrokerID]
		if a == nil {
			a = &agg{}
			m[*e.BrokerID] = a
		}
		a.total++
		if e.Result == ResultFailure {
			a.failed++
		}
		a.lat.add(e)
	})
	out := map[int64][3]float64{}
	for id, a := range m {
		avg, _, _ := a.lat.stats()
		out[id] = [3]float64{float64(a.total), float64(a.failed), avg}
	}
	return out, err
}

func fmtID(id int64) string { return strconv.FormatInt(id, 10) }
