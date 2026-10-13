package realtime

import "github.com/fix-gateway/backend/internal/auth"

var brokerPermissions = map[string]string{
	"order.updated":     auth.PermOrdersRead,
	"trade.created":     auth.PermTradesRead,
	"marketdata.tick":   auth.PermOrdersRead,
	"positions.updated": auth.PermPositionsRead,
}

func allowedBrokerTopics(permissions, types []string) (map[string]bool, error) {
	topics := map[string]bool{}
	if len(types) == 0 {
		for topic, permission := range brokerPermissions {
			if auth.HasPermission(permissions, permission) {
				topics[topic] = true
			}
		}
		return topics, nil
	}
	if len(types) > len(brokerPermissions) {
		return nil, ErrPermission
	}
	for _, topic := range types {
		permission, ok := brokerPermissions[topic]
		if !ok || !auth.HasPermission(permissions, permission) {
			return nil, ErrPermission
		}
		topics[topic] = true
	}
	return topics, nil
}
