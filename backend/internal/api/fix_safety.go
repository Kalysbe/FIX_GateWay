package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/observability"
)

type fixAlarm struct {
	active   bool
	notified time.Time
}
type fixProblem struct {
	code, message, detail string
	active                bool
}

// Notifications are delivered through Gateway's existing persistent inbox/UI.
// No email, webhook or external recipient is used by this monitor.
func (s *Server) RunFIXSafetyMonitor(ctx context.Context) {
	alarms := map[string]fixAlarm{}
	publish := func(p fixProblem) {
		previous := alarms[p.code]
		now := time.Now()
		if p.active && (!previous.active || now.Sub(previous.notified) >= 5*time.Minute) {
			if s.obs != nil {
				s.obs.Emit(observability.Event{Type: observability.TypeError, Severity: observability.SeverityCritical, Subtype: p.code, ErrorCode: p.code, Message: p.message, ErrorMessage: p.detail})
			}
			previous.notified = now
		} else if !p.active && previous.active {
			if s.obs != nil {
				s.obs.Emit(observability.Event{Type: observability.TypeSystem, Severity: observability.SeverityInfo, Subtype: "FIX_SAFETY_RECOVERED", ErrorCode: p.code, Message: "Контроль FIX восстановлен", ErrorMessage: p.message + ": проблема устранена"})
			}
		}
		previous.active = p.active
		alarms[p.code] = previous
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if s.executionInbox != nil {
			state := s.executionInbox.Status()
			detail := fmt.Sprintf("Ожидает обработки: %d; возраст старейшего: %.0f с. Новые заявки приостановлены.", state.Pending, state.OldestAge.Seconds())
			if b, ok := s.executionInbox.Blocked(); ok {
				detail += fmt.Sprintf(" Отчёт не применяется: %s. Если ошибка не временная, отложите отчёт в карантин на странице «Входящие FIX».", b.Error)
			}
			publish(fixProblem{"EXECUTION_INBOX_BLOCKED", "Задержана обработка входящих FIX-отчётов", detail, !state.Healthy()})
		}
		if s.manager != nil {
			resetting := map[string]bool{}
			for _, id := range s.manager.ResettingVenueSessions() {
				resetting[id] = true
			}
			for code := range alarms {
				if id, ok := strings.CutPrefix(code, "FIX_SEQUENCE_RESET_"); ok && !resetting[id] {
					publish(fixProblem{code: code, message: "Сброс последовательности FIX при входе"})
				}
			}
			for id := range resetting {
				publish(fixProblem{"FIX_SEQUENCE_RESET_" + id, "Сброс последовательности FIX при входе", fmt.Sprintf("Адаптер %s: ResetOnLogon=true. Восстановление пропущенных FIX-сообщений после переподключения требует проверки; отключение сброса необходимо согласовать с ARQA.", id), true})
			}
			for id, usage := range s.manager.AuditUsage() {
				publish(fixProblem{"FIX_AUDIT_CAPACITY_" + id, "Заполняется журнал FIX", fmt.Sprintf("Адаптер %s: занято %d из %d байт. При исчерпании лимита приём будет остановлен; требуется освобождение места с сохранением нужной истории.", id, usage[0], usage[1]), usage[1] > 0 && usage[0] >= usage[1]*7/10})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
