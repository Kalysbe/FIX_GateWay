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
	// notice is a configuration warning: reported once when it appears
	// (and again after it was cleared), never repeated every five minutes.
	notice bool
}

// fixAlarmDue: an active problem is reported when it appears; a failure is
// repeated every five minutes while it lasts, a notice is not.
func fixAlarmDue(previous fixAlarm, p fixProblem, now time.Time) bool {
	if !p.active {
		return false
	}
	return !previous.active || (!p.notice && now.Sub(previous.notified) >= 5*time.Minute)
}

// Notifications are delivered through Gateway's existing persistent inbox/UI.
// No email, webhook or external recipient is used by this monitor.
func (s *Server) RunFIXSafetyMonitor(ctx context.Context) {
	alarms := map[string]fixAlarm{}
	publish := func(p fixProblem) {
		previous := alarms[p.code]
		now := time.Now()
		if fixAlarmDue(previous, p, now) {
			if s.obs != nil {
				event := observability.Event{Type: observability.TypeError, Severity: observability.SeverityCritical, Subtype: p.code, ErrorCode: p.code, Message: p.message, ErrorMessage: p.detail}
				if p.notice {
					event.Type, event.Severity = observability.TypeSystem, observability.SeverityWarning
				}
				s.obs.Emit(event)
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
			publish(fixProblem{code: "EXECUTION_INBOX_BLOCKED", message: "Задержана обработка входящих FIX-отчётов", detail: detail, active: !state.Healthy()})
		}
		if s.manager != nil {
			resetting := map[string]bool{}
			for _, id := range s.manager.ResettingVenueSessions() {
				resetting[id] = true
			}
			for code := range alarms {
				if id, ok := strings.CutPrefix(code, "FIX_SEQUENCE_RESET_"); ok && !resetting[id] {
					publish(fixProblem{code: code, message: "Сброс последовательности FIX при входе", notice: true})
				}
			}
			for id := range resetting {
				publish(fixProblem{code: "FIX_SEQUENCE_RESET_" + id, message: "Сброс последовательности FIX при входе", active: true, notice: true,
					detail: fmt.Sprintf("Адаптер %s: ResetOnLogon=true. При каждом входе номера FIX начинаются с 1 и QUIK очищает архив отправленных сообщений, поэтому сообщения за время разрыва связи не досылаются.", id)})
			}
			for id, usage := range s.manager.AuditUsage() {
				publish(fixProblem{code: "FIX_AUDIT_CAPACITY_" + id, message: "Заполняется журнал FIX", active: usage[1] > 0 && usage[0] >= usage[1]*7/10,
					detail: fmt.Sprintf("Адаптер %s: занято %d из %d байт. При исчерпании лимита приём будет остановлен; требуется освобождение места с сохранением нужной истории.", id, usage[0], usage[1])})
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
