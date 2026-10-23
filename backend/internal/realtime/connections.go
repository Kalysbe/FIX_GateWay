package realtime

import (
	"sort"
	"strings"
	"time"
)

// ConnectionInfo contains no credentials. Identity comes from authenticated claims.
type ConnectionInfo struct {
	OrderFirmID string    `json:"-"`
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	BrokerID    int64     `json:"broker_id,omitempty"`
	BrokerCode  string    `json:"broker_code,omitempty"`
	BrokerName  string    `json:"broker_name,omitempty"`
	Email       string    `json:"email"`
	Peer        string    `json:"peer"`
	Transport   string    `json:"transport"`
	ConnectedAt time.Time `json:"connected_at"`
}
type Connection struct {
	ConnectionInfo
	Role   string   `json:"role"`
	Topics []string `json:"topics"`
	Paths  []string `json:"paths"`
}
type ConnectionsSnapshot struct {
	InstanceID  string       `json:"instance_id"`
	Revision    uint64       `json:"revision"`
	Connections []Connection `json:"connections"`
}

func (h *Hub) Connections() ConnectionsSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := ConnectionsSnapshot{InstanceID: h.instanceID, Revision: h.revision, Connections: make([]Connection, 0, len(h.clients))}
	for s := range h.clients {
		c := Connection{ConnectionInfo: s.info, Role: s.role, Topics: make([]string, 0, len(s.topics)), Paths: []string{}}
		for topic := range s.topics {
			c.Topics = append(c.Topics, topic)
		}
		sort.Strings(c.Topics)
		paths := map[string]bool{}
		for _, topic := range c.Topics {
			for _, path := range topicPaths[topic] {
				if c.BrokerID > 0 {
					path = strings.Replace(path, "/admin/", "/", 1)
					if topic == "marketdata.tick" {
						path = "/api/v1/orderbook"
						paths["/api/v1/market-data"] = true
					}
				}
				paths[path] = true
			}
		}
		for path := range paths {
			c.Paths = append(c.Paths, path)
		}
		sort.Strings(c.Paths)
		out.Connections = append(out.Connections, c)
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].ID < out.Connections[j].ID })
	return out
}
func (h *Hub) notifySubscriptions() { h.Broadcast("subscriptions.updated", h.Connections()) }

// Paths describe the data represented by a topic; the actual RPC is always Stream.
var topicPaths = map[string][]string{
	"order.updated":          {"/api/v1/admin/orders"},
	"trade.created":          {"/api/v1/admin/trades"},
	"positions.updated":      {"/api/v1/admin/positions"},
	"marketdata.tick":        {"/api/v1/admin/market-data"},
	"adapter.status_changed": {"/api/v1/admin/adapters"},
	"notification.new":       {"/api/v1/admin/notifications"},
	"obs.event":              {"/api/v1/admin/logs"},
	"subscriptions.updated":  {"/api/v1/admin/grpc/connections"},
}
