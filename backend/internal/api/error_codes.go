package api

import "net/http"

// gatewayCode is a value of the reply's "code" field.
type gatewayCode struct {
	Code        string `json:"code"`
	HTTPStatus  int    `json:"http_status"`
	Description string `json:"description"`
}

// quikErrorCode is a value of the reply's "error_code" field: QUIK's own reason
// (FIX 103 OrdRejReason on an order, FIX 102 CxlRejReason on a cancel).
type quikErrorCode struct {
	ErrorCode   int    `json:"error_code"`
	Operation   string `json:"operation"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

var gatewayCodes = []gatewayCode{
	{"OK", 200, "Запрос выполнен (201 — заявка принята QUIK)"},
	{"QUIK_ANSWER_PENDING", 202, "QUIK ещё не ответил; заявка не отменена, проверьте GET /api/v1/orders/{id}"},
	{"QUIK_ANSWER_UNKNOWN", 202, "Заявка сохранена, её состояние в QUIK прочитать не удалось"},
	{"VALIDATION_ERROR", 400, "Неверный запрос: поле, значение, счёт или инструмент"},
	{"UNAUTHORIZED", 401, "Нет токена или токен недействителен"},
	{"FORBIDDEN", 403, "Нет права, IP запрещён или указана чужая фирма"},
	{"NOT_FOUND", 404, "Объект не найден или принадлежит другой фирме"},
	{"METHOD_NOT_ALLOWED", 405, "Метод не поддерживается для этого адреса"},
	{"CONFLICT", 409, "Действие невозможно в текущем состоянии, например заявка не активна"},
	{"CANCEL_REJECTED", 409, "QUIK не снял заявку; причина — message и error_code"},
	{"PAYLOAD_TOO_LARGE", 413, "Слишком большое тело запроса"},
	{"ORDER_REJECTED", 422, "QUIK отклонил заявку; причина — message и error_code"},
	{"TRADING_PAUSED", 422, "Приём новых заявок приостановлен оператором шлюза"},
	{"MAX_ORDER_QUANTITY", 422, "Количество больше лимита, заданного оператором шлюза"},
	{"UNPROCESSABLE", 422, "Запрос понят, но не может быть выполнен"},
	{"MAX_OPEN_ORDERS", 422, "Достигнут лимит активных заявок, заданный оператором шлюза"},
	{"RATE_LIMITED", 429, "Превышен лимит запросов; пауза — в заголовке Retry-After"},
	{"RESOURCE_EXHAUSTED", 429, "Превышен лимит операции; пауза — в заголовке Retry-After"},
	{"INTERNAL_ERROR", 500, "Внутренняя ошибка шлюза"},
	{"ORDER_FAILED", 502, "Заявку не удалось передать в QUIK"},
	{"BAD_GATEWAY", 502, "Ошибка FIX / QUIK"},
	{"SERVICE_UNAVAILABLE", 503, "QUIK или шлюз временно недоступны; повторите позже"},
	{"TIMEOUT", 504, "Истекло время ожидания ответа"},
}

const (
	sourceExchange = "Торговая система (QME)"
	sourceServer   = "Сервер QUIK"
	sourceAdapter  = "FIX adapter QUIK"
	sourceRisk     = "Дилерская библиотека QUIK (проверка лимитов и прав)"
	opOrder        = "order"
	opCancel       = "cancel"
)

// quikErrorCodes are the codes QUIK has returned (TransResult.ErrorCode, by
// ErrorSource) and the FIX 102 cancel-reject reasons. QUIK may send codes not
// listed here; message always carries the text.
var quikErrorCodes = []quikErrorCode{
	{1, opOrder, sourceExchange, "Операции в данный момент запрещены: некорректный период сессии"},
	{2, opOrder, sourceExchange, "Неверно указан участник торгов"},
	{3, opOrder, sourceExchange, "Неверно указан торговый счёт или счёт не поддерживается для класса"},
	{5, opOrder, sourceExchange, "Неверно указана цена"},
	{13, opOrder, sourceExchange, "Неверный инструмент"},
	{103, opOrder, sourceServer, "Нет связи со шлюзом торговой системы"},
	{8635226, opOrder, sourceRisk, "Не указан код клиента"},
	{8636237, opOrder, sourceServer, "Неверный формат примечания: недопустимый символ"},
	{8637216, opOrder, sourceRisk, "Работа по данному инструменту запрещена"},
	{8637217, opOrder, sourceRisk, "Цена заявки должна быть положительной"},
	{8637218, opOrder, sourceRisk, "Цена заявки вне установленного диапазона"},
	{8637220, opOrder, sourceRisk, "Превышена позиция по инструменту"},
	{8637222, opOrder, sourceRisk, "Неверно указан финансовый инструмент"},
	{8637223, opOrder, sourceRisk, "Рыночная заявка для клиентского счёта запрещена"},
	{8637225, opOrder, sourceRisk, "Работа по данному торговому счёту запрещена"},
	{8637226, opOrder, sourceRisk, "Превышена позиция по деньгам"},
	{8637233, opOrder, sourceRisk, "Количество в заявке должно быть положительным"},
	{8637240, opOrder, sourceRisk, "Инструмент запрещён для операции шорт"},
	{8637267, opOrder, sourceRisk, "Скорректированное значение НПР1 меньше 0: непокрытая позиция по неликвидному инструменту"},
	{0, opCancel, sourceAdapter, "Слишком поздно снимать: заявка уже исполнена или снята"},
	{1, opCancel, sourceAdapter, "Заявка неизвестна"},
	{2, opCancel, sourceAdapter, "Снятие отклонено QUIK"},
	{3, opCancel, sourceAdapter, "Заявка уже в процессе снятия или замены"},
}

// listErrorCodes is GET /api/v1/errors: every "code" and known "error_code".
func (s *Server) listErrorCodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"codes": gatewayCodes, "error_codes": quikErrorCodes})
}
