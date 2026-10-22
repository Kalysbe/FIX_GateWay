package realtime

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var ErrPermission = errors.New("event not permitted")
var ErrCapacity = errors.New("stream capacity exceeded")
var ErrBackpressure = errors.New("consumer queue full")

var EventTypes = []string{"obs.event", "notification.new", "order.updated", "trade.created", "positions.updated", "marketdata.tick", "adapter.status_changed", "subscriptions.updated"}

func EventAllowed(role, event string) bool {
	switch event {
	case "obs.event", "subscriptions.updated":
		return auth.HasAnyRole(role, auth.RolesAudit...)
	case "notification.new", "order.updated", "trade.created", "positions.updated", "marketdata.tick", "adapter.status_changed":
		return auth.HasAnyRole(role, auth.RolesAll...)
	default:
		return false
	}
}

type Subscription struct {
	Events      chan *pb.Event
	Done        chan struct{}
	hub         *Hub
	role        string
	topics      map[string]bool
	err         error
	info        ConnectionInfo
	permissions []string
}

func (s *Subscription) Close()     { s.hub.remove(s, nil) }
func (s *Subscription) Err() error { s.hub.mu.RLock(); defer s.hub.mu.RUnlock(); return s.err }
func (s *Subscription) SetTopics(types []string) error {
	topics, err := allowedTopics(s.role, types)
	if s.info.BrokerID > 0 {
		topics, err = allowedBrokerTopics(s.permissions, types)
	}
	if err != nil {
		return err
	}
	s.hub.mu.Lock()
	if !s.hub.clients[s] {
		s.hub.mu.Unlock()
		return ErrBackpressure
	}
	s.topics = topics
	s.hub.enqueueSnapshot(s)
	s.hub.revision++
	s.hub.mu.Unlock()
	s.hub.notifySubscriptions()
	return nil
}
func (s *Subscription) Topics() []string {
	s.hub.mu.RLock()
	defer s.hub.mu.RUnlock()
	topics := make([]string, 0, len(s.topics))
	for topic := range s.topics {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	return topics
}

type Hub struct {
	// Set before serving subscriptions. Returns the current full quote snapshot.
	MarketDataSnapshot func() any
	mu                 sync.RWMutex
	clients            map[*Subscription]bool
	sequence           atomic.Uint64
	revision           uint64
	instanceID         string
}

func NewHub() *Hub { return &Hub{clients: make(map[*Subscription]bool), instanceID: uuid.NewString()} }
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
	return h.SubscribeIdentified(role, types, ConnectionInfo{})
}
func (h *Hub) SubscribeIdentified(role string, types []string, info ConnectionInfo) (*Subscription, error) {
	return h.subscribe(role, types, info, nil)
}
func (h *Hub) SubscribeBroker(types []string, info ConnectionInfo, permissions []string) (*Subscription, error) {
	if info.BrokerID <= 0 {
		return nil, ErrPermission
	}
	return h.subscribe("BROKER", types, info, permissions)
}
func (h *Hub) subscribe(role string, types []string, info ConnectionInfo, permissions []string) (*Subscription, error) {
	topics, err := allowedTopics(role, types)
	if info.BrokerID > 0 {
		topics, err = allowedBrokerTopics(permissions, types)
	}
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if len(h.clients) >= 1024 {
		h.mu.Unlock()
		return nil, ErrCapacity
	}
	info.ID = uuid.NewString()
	info.ConnectedAt = time.Now().UTC()
	s := &Subscription{Events: make(chan *pb.Event, 64), Done: make(chan struct{}), hub: h, role: role, topics: topics, info: info}
	s.permissions = append([]string(nil), permissions...)
	h.clients[s] = true
	h.enqueueSnapshot(s)
	h.revision++
	h.mu.Unlock()
	h.notifySubscriptions()
	return s, nil
}
func (h *Hub) enqueueSnapshot(s *Subscription) {
	if s.info.BrokerID > 0 && s.topics["positions.updated"] {
		select {
		case s.Events <- &pb.Event{Sequence: h.sequence.Add(1), Type: "positions.updated", OccurredAt: timestamppb.Now()}:
		default:
		}
	}
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
	if s.info.BrokerID > 0 {
		payload = brokerMarketDataPayload(payload)
	}
	event := &pb.Event{Sequence: h.sequence.Add(1), Type: "marketdata.tick", Data: payload, OccurredAt: timestamppb.Now()}
	select {
	case s.Events <- event:
	default:
	}
}
func (h *Hub) remove(s *Subscription, err error) {
	h.mu.Lock()
	changed := false
	if h.clients[s] {
		delete(h.clients, s)
		s.err = err
		close(s.Done)
		h.revision++
		changed = true
	}
	h.mu.Unlock()
	if changed {
		h.notifySubscriptions()
	}
}
func (h *Hub) Broadcast(eventType string, data any) {
	h.BroadcastForBroker(eventType, data, 0)
}
func (h *Hub) BroadcastForBroker(eventType string, data any, brokerID int64) {
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
	var brokerMarketEvent *pb.Event
	for s := range h.clients {
		if !s.topics[eventType] {
			continue
		}
		if s.info.BrokerID > 0 {
			if eventType != "marketdata.tick" && eventType != "positions.updated" && s.info.BrokerID != brokerID {
				continue
			}
		} else if !EventAllowed(s.role, eventType) {
			continue
		}
		delivery := event
		if s.info.BrokerID > 0 && eventType == "marketdata.tick" {
			if brokerMarketEvent == nil {
				brokerMarketEvent = &pb.Event{Sequence: event.Sequence, Type: event.Type, Data: brokerMarketDataPayload(payload), OccurredAt: event.OccurredAt}
			}
			delivery = brokerMarketEvent
		}
		if s.info.BrokerID > 0 && eventType == "positions.updated" {
			// Only a trigger reaches broker streams. The service replaces it with
			// a snapshot fetched using the authenticated token's firm scope.
			delivery = &pb.Event{Sequence: event.Sequence, Type: event.Type, OccurredAt: event.OccurredAt}
		}
		select {
		case s.Events <- delivery:
		default:
			slow = append(slow, s)
		}
	}
	h.mu.Unlock()
	for _, s := range slow {
		h.remove(s, ErrBackpressure)
	}
}
