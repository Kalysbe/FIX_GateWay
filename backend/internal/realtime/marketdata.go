package realtime

import (
	"reflect"
	"strings"

	"github.com/fix-gateway/backend/internal/brokerdata"
	"google.golang.org/protobuf/types/known/structpb"
)

var brokerQuoteFields = func() []string {
	typ := reflect.TypeOf(brokerdata.MarketQuote{})
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	return fields
}()

// Project the shared event onto the REST quote contract once per broadcast.
// The admin payload remains intact; protobuf values and decimal strings are
// copied without converting them through floating-point numbers.
func brokerMarketDataPayload(payload *structpb.Struct) *structpb.Struct {
	quotes := &structpb.ListValue{}
	for _, item := range payload.GetFields()["quotes"].GetListValue().GetValues() {
		quote := item.GetStructValue()
		if quote == nil {
			continue
		}
		fields := make(map[string]*structpb.Value)
		for _, name := range brokerQuoteFields {
			if value, ok := quote.Fields[name]; ok {
				if name == "bids" || name == "offers" {
					levels := &structpb.ListValue{}
					for _, level := range value.GetListValue().GetValues() {
						if row := level.GetStructValue(); row != nil {
							values := make(map[string]*structpb.Value)
							for _, field := range []string{"price", "size"} {
								if v, exists := row.Fields[field]; exists {
									values[field] = v
								}
							}
							levels.Values = append(levels.Values, structpb.NewStructValue(&structpb.Struct{Fields: values}))
						}
					}
					value = structpb.NewListValue(levels)
				}
				fields[name] = value
			}
		}
		quotes.Values = append(quotes.Values, structpb.NewStructValue(&structpb.Struct{Fields: fields}))
	}
	fields := map[string]*structpb.Value{"quotes": structpb.NewListValue(quotes)}
	if replace, ok := payload.GetFields()["replace"]; ok {
		fields["replace"] = replace
	}
	if subscription := payload.GetFields()["subscription"].GetStructValue(); subscription != nil {
		status := subscription.Fields["status"].GetStringValue()
		fields["live"] = structpb.NewBoolValue(len(quotes.Values) > 0 && (status == "ACTIVE" || status == "PARTIAL"))
	}
	return &structpb.Struct{Fields: fields}
}
