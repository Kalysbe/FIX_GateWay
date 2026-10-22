package orders

import "strings"

type executionMatch struct{ ID, Broker, Trader string }

// These are distinct from the per-order ExecID: 880 identifies the matched trade,
// 375 identifies the contra broker, and 337 identifies its trader (not a client).
func executionMatchFromFIX(raw string) executionMatch {
	values := map[string]string{}
	conflicting := map[string]bool{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == '\x01' }) {
		tag, value, ok := strings.Cut(field, "=")
		if !ok || (tag != "880" && tag != "375" && tag != "337") || value == "" {
			continue
		}
		if previous := values[tag]; previous != "" && previous != value {
			conflicting[tag] = true
		}
		values[tag] = value
	}
	for tag := range conflicting {
		delete(values, tag)
	}
	return executionMatch{ID: values["880"], Broker: values["375"], Trader: values["337"]}
}
