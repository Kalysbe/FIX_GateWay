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

// GRPCCertWarnBefore is how early an expiring gRPC server certificate is reported.
const GRPCCertWarnBefore = 30 * 24 * time.Hour

// grpcCertProblems reports the served gRPC certificate: a warning once from 30
// days before expiry, a repeated critical alarm once it has expired. Clients
// that verify TLS, and the admin console's own event stream, fail after that.
func grpcCertProblems(notAfter, now time.Time) []fixProblem {
	if notAfter.IsZero() {
		return nil
	}
	until := notAfter.UTC().Format("02.01.2006 15:04 UTC")
	expired := !now.Before(notAfter)
	return []fixProblem{
		{code: "GRPC_CERT_EXPIRING", message: "Скоро истекает сертификат gRPC", notice: true,
			active: !expired && notAfter.Sub(now) <= GRPCCertWarnBefore,
			detail: fmt.Sprintf("Сертификат gRPC-сервера действует до %s (осталось %d дн.). Выпустите новый и перезапустите шлюз заранее; если меняется CA, раздайте брокерам новый ca.crt.", until, int(notAfter.Sub(now).Hours()/24))},
		{code: "GRPC_CERT_EXPIRED", message: "Истёк сертификат gRPC", active: expired,
			detail: fmt.Sprintf("Сертификат gRPC-сервера истёк %s. Брокеры, проверяющие TLS, не подключаются к gRPC, а поток событий консоли не работает (страницы обновляются опросом). Выпустите новый сертификат и перезапустите шлюз.", until)},
	}
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
		// During the scheduled pause no adapter runs: that is not a cleared
		// setting, so the adapter alarms keep their state until 09:05.
		if s.manager != nil && !s.manager.Paused() {
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
		if at := s.grpcCertNotAfter.Load(); at != 0 {
			for _, p := range grpcCertProblems(time.Unix(at, 0), time.Now()) {
				publish(p)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
