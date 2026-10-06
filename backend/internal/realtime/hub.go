package realtime

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPermission = errors.New("event not permitted")
var ErrCapacity = errors.New("stream capacity exceeded")
var ErrBackpressure = errors.New("consumer queue full")

var EventTypes = []string{"obs.event", "notification.new", "order.updated", "trade.created", "positions.updated", "marketdata.tick", "adapter.status_changed"}

func EventAllowed(role, event string) bool {
	switch event {
	case "obs.event":
		return auth.HasAnyRole(role, auth.RolesAudit...)
	case "notification.new", "order.updated", "trade.created", "positions.updated", "marketdata.tick", "adapter.status_changed":
		return auth.HasAnyRole(role, auth.RolesAll...)
	default:
		return false
	}
}

type Subscription struct {
	Events chan *pb.Event
	Done   chan struct{}
	hub    *Hub
	role   string
	topics map[string]bool
	err    error
}

func (s *Subscription) Close()     { s.hub.remove(s, nil) }
func (s *Subscription) Err() error { s.hub.mu.RLock(); defer s.hub.mu.RUnlock(); return s.err }
func (s *Subscription) SetTopics(types []string) error {
	topics, err := allowedTopics(s.role, types)
	if err != nil {
		return err
	}
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.topics = topics
	s.hub.enqueueSnapshot(s)
	return nil
}

type Hub struct {
	// Set before serving subscriptions. Returns the current full quote snapshot.
	MarketDataSnapshot func() any
	mu                 sync.RWMutex
	clients            map[*Subscription]bool
	sequence           atomic.Uint64
}

func NewHub() *Hub { return &Hub{clients: make(map[*Subscription]bool)} }
func allowedTopics(role string, types []string) (map[string]bool, error) {
	if !auth.HasAnyRole(role, auth.RolesAll...) {
		return nil, ErrPermission
	}
	topics := make(map[string]bool)
	if len(types) == 0 {
		for _, event := range EventTypes {
			if EventAllowed(role, event) {
				topics[event] = true
			}
		}
		return topics, nil
	}
	if len(types) > len(EventTypes) {
		return nil, ErrPermission
	}
	for _, event := range types {
		if !EventAllowed(role, event) {
			return nil, ErrPermission
		}
		topics[event] = true
	}
	return topics, nil
}
func (h *Hub) Subscribe(role string, types []string) (*Subscription, error) {
	topics, err := allowedTopics(role, types)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= 1024 {
		return nil, ErrCapacity
	}
	s := &Subscription{Events: make(chan *pb.Event, 64), Done: make(chan struct{}), hub: h, role: role, topics: topics}
	h.clients[s] = true
	h.enqueueSnapshot(s)
	return s, nil
}
func (h *Hub) enqueueSnapshot(s *Subscription) {
	if !s.topics["marketdata.tick"] || h.MarketDataSnapshot == nil {
		return
	}
	data := h.MarketDataSnapshot()
	if data == nil {
		return
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload := new(structpb.Struct)
	if protojson.Unmarshal(encoded, payload) != nil {
		return
	}
	event := &pb.Event{Sequence: h.sequence.Add(1), Type: "marketdata.tick", Data: payload, OccurredAt: timestamppb.Now()}
	select {
	case s.Events <- event:
	default:
	}
}
func (h *Hub) remove(s *Subscription, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[s] {
		delete(h.clients, s)
		s.err = err
		close(s.Done)
	}
}
func (h *Hub) Broadcast(eventType string, data any) {
	payload := &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	if data != nil {
		encoded, err := json.Marshal(data)
		if err != nil {
			return
		}
		if protojson.Unmarshal(encoded, payload) != nil {
			return
		}
	}
	var slow []*Subscription
	h.mu.Lock()
	event := &pb.Event{Sequence: h.sequence.Add(1), Type: eventType, Data: payload, OccurredAt: timestamppb.Now()}
	for s := range h.clients {
		if !s.topics[eventType] || !EventAllowed(s.role, eventType) {
			continue
		}
		select {
		case s.Events <- event:
		default:
			slow = append(slow, s)
		}
	}
	h.mu.Unlock()
	for _, s := range slow {
		h.remove(s, ErrBackpressure)
	}
}
