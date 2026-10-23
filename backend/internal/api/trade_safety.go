package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/tradeledger"
)

type tradeAlarm struct {
	active   bool
	notified time.Time
}
type tradeProblem struct {
	code, message, detail string
	active                bool
}

func tradeProblems(h tradeledger.Health, now, started time.Time) []tradeProblem {
	stale := now.Sub(started) > 5*time.Minute
	if h.LastSuccess != nil {
		stale = now.Sub(*h.LastSuccess) > 5*time.Minute
	}
	return []tradeProblem{
		{"TRADE_PAIR_INCOMPLETE", "Не сохранены обе стороны сделки", fmt.Sprintf("Сделок без обеих реальных сторон дольше двух минут: %d", h.Incomplete), h.Incomplete > 0},
		{"TRADE_SOURCE_ERROR", "Ошибка обработки исходных данных сделки", fmt.Sprintf("Исходных записей с ошибкой: %d. Подробности сохранены в trade_reports.", h.ReportErrors), h.ReportErrors > 0},
		{"TRADE_SYNC_FAILED", "Сверка сделок с QUIK завершилась ошибкой", h.LatestError, h.LatestError != ""},
		{"TRADE_SYNC_STALE", "Полнота загрузки сделок давно не подтверждалась", "Нет успешной полной сверки QUIK более пяти минут.", stale},
	}
}

// Notifications are delivered through Gateway's existing persistent inbox/UI.
// No email, webhook or external recipient is used by this monitor.
func (s *Server) runTradeSafetyMonitor(ctx context.Context) {
	started := time.Now()
	alarms := map[string]tradeAlarm{}
	publish := func(p tradeProblem) {
		previous := alarms[p.code]
		now := time.Now()
		if p.active && (!previous.active || now.Sub(previous.notified) >= 5*time.Minute) {
			if s.obs != nil {
				s.obs.Emit(observability.Event{Type: observability.TypeError, Severity: observability.SeverityCritical, Subtype: p.code, ErrorCode: p.code, Message: p.message, ErrorMessage: p.detail})
			}
			previous.notified = now
		} else if !p.active && previous.active {
			if s.obs != nil {
				s.obs.Emit(observability.Event{Type: observability.TypeSystem, Severity: observability.SeverityInfo, Subtype: "TRADE_SAFETY_RECOVERED", ErrorCode: p.code, Message: "Контроль сделок восстановлен", ErrorMessage: p.message + ": проблема устранена"})
			}
		}
		previous.active = p.active
		alarms[p.code] = previous
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		h, err := s.tradeLedger.Health(work)
		cancel()
		if err != nil {
			metrics.TradeMonitorFailed.Set(1)
			publish(tradeProblem{"TRADE_MONITOR_UNAVAILABLE", "Контроль полноты сделок недоступен", err.Error(), true})
		} else {
			metrics.TradeMonitorFailed.Set(0)
			publish(tradeProblem{code: "TRADE_MONITOR_UNAVAILABLE", message: "Контроль полноты сделок недоступен"})
			metrics.TradeIncomplete.Set(float64(h.Incomplete))
			metrics.TradeReportErrors.Set(float64(h.ReportErrors))
			if h.LastSuccess != nil {
				metrics.TradeLastSuccess.Set(float64(h.LastSuccess.Unix()))
			} else {
				metrics.TradeLastSuccess.Set(0)
			}
			if h.LatestError != "" {
				metrics.TradeSyncFailed.Set(1)
			} else {
				metrics.TradeSyncFailed.Set(0)
			}
			for _, p := range tradeProblems(h, time.Now(), started) {
				publish(p)
			}
		}
		if s.executionInbox != nil {
			state := s.executionInbox.Status()
			detail := fmt.Sprintf("Ожидает обработки: %d; возраст старейшего: %.0f с. Новые заявки приостановлены.", state.Pending, state.OldestAge.Seconds())
			if b, ok := s.executionInbox.Blocked(); ok {
				detail += fmt.Sprintf(" Отчёт не применяется: %s. Если ошибка не временная, отложите отчёт в карантин на странице «Входящие FIX».", b.Error)
			}
			publish(tradeProblem{"EXECUTION_INBOX_BLOCKED", "Задержана обработка входящих FIX-отчётов", detail, !state.Healthy()})
		}
		if s.manager != nil {
			resetting := map[string]bool{}
			for _, id := range s.manager.ResettingVenueSessions() {
				resetting[id] = true
			}
			for code := range alarms {
				if id, ok := strings.CutPrefix(code, "FIX_SEQUENCE_RESET_"); ok && !resetting[id] {
					publish(tradeProblem{code: code, message: "Сброс последовательности FIX при входе"})
				}
			}
			for id := range resetting {
				publish(tradeProblem{"FIX_SEQUENCE_RESET_" + id, "Сброс последовательности FIX при входе", fmt.Sprintf("Адаптер %s: ResetOnLogon=true. Восстановление пропущенных FIX-сообщений после переподключения требует проверки; отключение сброса необходимо согласовать с ARQA. Сверка истории QUIK остаётся обязательной.", id), true})
			}
			for id, usage := range s.manager.AuditUsage() {
				publish(tradeProblem{"FIX_AUDIT_CAPACITY_" + id, "Заполняется журнал FIX", fmt.Sprintf("Адаптер %s: занято %d из %d байт. При исчерпании лимита приём будет остановлен; требуется освобождение места с сохранением нужной истории.", id, usage[0], usage[1]), usage[1] > 0 && usage[0] >= usage[1]*7/10})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
