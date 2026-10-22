package arqa

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func buildSecurityDefinitionRequest(reqID, board string) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_SECURITY_DEFINITION_REQUEST))
	msg.Body.SetString(tag.SecurityReqID, reqID)
	// ARQA HowTo / FIX: 3 = Request List Securities
	msg.Body.SetString(tag.SecurityRequestType, string(enum.SecurityRequestType_REQUEST_LIST_SECURITIES))
	if b := strings.TrimSpace(board); b != "" {
		msg.Body.SetString(tag.ExDestination, b)
	}
	return msg
}

func parseSecurityDefinitionMessage(msg *quickfix.Message) (reqID string, totalHint int, items []domain.SecurityRef) {
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return decodeFIXText(v)
	}
	reqID = get(tag.SecurityReqID)
	if n := get(tag.TotalNumSecurities); n != "" {
		totalHint, _ = strconv.Atoi(n)
	}
	items = parseRelatedSymFromRaw(msg.String())
	// Fallback: single-instrument message without repeating group
	if len(items) == 0 {
		sym := firstNonEmpty(get(tag.UnderlyingSecurityID), get(tag.UnderlyingSymbol), get(tag.SecurityID), get(tag.Symbol))
		board := firstNonEmpty(get(tag.UnderlyingSecurityExchange), get(tag.SecurityExchange), get(tag.ExDestination))
		if sym != "" {
			items = append(items, domain.SecurityRef{
				Symbol:       firstNonEmpty(get(tag.UnderlyingSymbol), sym),
				SecurityID:   firstNonEmpty(get(tag.UnderlyingSecurityID), get(tag.SecurityID), sym),
				Board:        board,
				Currency:     firstNonEmpty(get(tag.UnderlyingCurrency), get(tag.Currency)),
				SecurityType: firstNonEmpty(get(tag.UnderlyingSecurityType), get(tag.SecurityType)),
				Name:         firstNonEmpty(get(tag.SecurityDesc), get(tag.UnderlyingSecurityDesc)),
			})
		}
	}
	return reqID, totalHint, items
}

// parseRelatedSymFromRaw walks the raw FIX string because UseDataDictionary=N
// collapses repeating-group tags in the FieldMap.
func parseRelatedSymFromRaw(raw string) []domain.SecurityRef {
	parts := strings.Split(raw, "\x01")
	var out []domain.SecurityRef
	var cur *domain.SecurityRef
	inGroup := false
	delimiter := ""

	flush := func() {
		if cur == nil {
			return
		}
		sym := firstNonEmpty(cur.SecurityID, cur.Symbol)
		if sym == "" {
			cur = nil
			return
		}
		if cur.Symbol == "" {
			cur.Symbol = sym
		}
		if cur.SecurityID == "" {
			cur.SecurityID = sym
		}
		out = append(out, *cur)
		cur = nil
	}

	ensure := func() *domain.SecurityRef {
		if cur == nil {
			cur = &domain.SecurityRef{}
		}
		return cur
	}

	for _, p := range parts {
		eq := strings.IndexByte(p, '=')
		if eq <= 0 {
			continue
		}
		t, v := p[:eq], decodeFIXText(p[eq+1:])
		switch t {
		case "146": // NoRelatedSym
			inGroup = true
			flush()
			delimiter = ""
		case "311": // UnderlyingSymbol — group delimiter
			if !inGroup {
				inGroup = true
			}
			if cur != nil && (cur.Symbol != "" || cur.SecurityID != "") {
				flush()
			}
			ensure().Symbol = v
			delimiter = "311"
		case "55": // Symbol
			if inGroup {
				// ARQA includes both 311 and 55 in the same underlying group.
				// Only 311 starts the next group in that format; splitting at 55
				// invented a second instrument with its name as SecurityID.
				if delimiter != "311" && cur != nil && cur.Symbol != "" {
					flush()
				}
				ensure().Symbol = v
				if delimiter != "311" {
					delimiter = "55"
				}
			}
		case "309": // UnderlyingSecurityID
			if inGroup {
				ensure().SecurityID = v
				if cur.Symbol == "" {
					cur.Symbol = v
				}
			}
		case "48": // SecurityID
			if inGroup {
				ensure().SecurityID = v
				if cur.Symbol == "" {
					cur.Symbol = v
				}
			}
		case "308", "207", "100": // exchange / class
			if inGroup && cur != nil && v != "" {
				cur.Board = v
			}
		case "318", "15":
			if inGroup && cur != nil && v != "" {
				cur.Currency = v
			}
		case "310", "167":
			if inGroup && cur != nil && v != "" {
				cur.SecurityType = v
			}
		case "307", "107", "5089":
			if inGroup && cur != nil && v != "" {
				cur.Name = v
			}
		}
	}
	flush()
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func newSecurityReqID() string {
	return fmt.Sprintf("SD-%d", time.Now().UnixNano())
}
