package api

import (
	"strconv"

	"github.com/getkin/kin-openapi/openapi3"
)

// envelopeCodes documents every "code" a broker reply can carry.
const envelopeCodes = "`OK` — успех. Ошибки: `VALIDATION_ERROR` (400), `UNAUTHORIZED` (401), `FORBIDDEN` (403), " +
	"`NOT_FOUND` (404), `CONFLICT` / `IDEMPOTENCY_CONFLICT` / `CANCEL_REJECTED` (409), " +
	"`ORDER_REJECTED` / `TRADING_PAUSED` / `MAX_ORDER_QUANTITY` / `MAX_OPEN_ORDERS` (422), " +
	"`RATE_LIMITED` / `RESOURCE_EXHAUSTED` (429), `INTERNAL_ERROR` (500), `ORDER_FAILED` (502), " +
	"`SERVICE_UNAVAILABLE` (503), `QUIK_ANSWER_PENDING` / `QUIK_ANSWER_UNKNOWN` (202)."

// wrapBrokerResponses documents the reply envelope that brokerEnvelope puts
// around every broker response, so the published schema is what is sent.
func wrapBrokerResponses(doc *openapi3.T) {
	for path, item := range doc.Paths.Map() {
		if !brokerPath(path) {
			continue
		}
		for _, op := range item.Operations() {
			if op.Responses == nil {
				continue
			}
			for status, ref := range op.Responses.Map() {
				if ref == nil || ref.Value == nil {
					continue
				}
				n, _ := strconv.Atoi(status)
				success := n == 200 || n == 201
				var data *openapi3.SchemaRef
				var example any
				if mt := ref.Value.Content.Get("application/json"); mt != nil {
					example = mt.Example
					// A failure carries data only when it is an order (QUIK reject, pending answer).
					if success || (mt.Schema != nil && mt.Schema.Ref == "#/components/schemas/Order") {
						data = mt.Schema
					}
				}
				_, paged := ref.Value.Headers["X-Has-More"]
				mt := openapi3.NewMediaType().WithSchema(envelopeSchema(success, data, paged && success))
				if example != nil {
					mt.Example = envelopeExample(success, example, data != nil)
				}
				ref.Value.Content = openapi3.Content{"application/json": mt}
			}
		}
	}
}

func envelopeSchema(success bool, data *openapi3.SchemaRef, paged bool) *openapi3.Schema {
	ok := openapi3.NewBoolSchema()
	ok.Description = "true — запрос выполнен; false — ошибка или QUIK ещё не ответил"
	code := openapi3.NewStringSchema()
	code.Description = envelopeCodes
	if success {
		code.Enum = []any{"OK"}
	}
	message := openapi3.NewStringSchema()
	message.Nullable = true
	message.Description = "Причина ошибки; null при успехе"
	s := openapi3.NewObjectSchema().
		WithProperty("success", ok).
		WithProperty("code", code).
		WithProperty("message", message)
	if data != nil {
		s = s.WithPropertyRef("data", data)
	} else {
		none := &openapi3.Schema{Nullable: true, Description: "null"}
		s = s.WithProperty("data", none)
	}
	if paged {
		more := openapi3.NewBoolSchema()
		more.Description = "Есть ли следующая страница (как заголовок X-Has-More)"
		next := openapi3.NewIntegerSchema()
		next.Description = "offset следующей страницы (как заголовок X-Next-Offset)"
		s = s.WithProperty("has_more", more).WithProperty("next_offset", next)
	}
	s.Required = []string{"success", "code", "message", "data"}
	return s
}

func envelopeExample(success bool, example any, keepData bool) any {
	if success {
		return map[string]any{"success": true, "code": "OK", "message": nil, "data": example}
	}
	out := map[string]any{"success": false, "code": "ERROR", "message": nil, "data": nil}
	fields, ok := example.(map[string]any)
	if !ok {
		return out
	}
	rest := map[string]any{}
	for k, v := range fields {
		switch k {
		case "code":
			out["code"] = v
		case "error":
			out["message"] = v
		default:
			rest[k] = v
		}
	}
	if keepData && len(rest) > 0 {
		out["data"] = rest
	}
	return out
}
