// Package bus is the simulator's central state store and message hub. Subsystems publish the latest state of a
// topic, emit log events, and register command handlers; GUI clients subscribe and receive everything as JSON.
package bus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	eventHistorySize = 2000
	subQueueSize     = 1024
)

// Event is a timestamped log line shown in the GUI event log.
type Event struct {
	Seq    int64     `json:"seq"`
	Time   time.Time `json:"time"`
	Level  string    `json:"level"`
	Source string    `json:"source"`
	Msg    string    `json:"msg"`
}

// Handler services a command sent from a GUI client or scenario.
type Handler func(args []byte) (any, error)

// Sub is a subscription that receives encoded outbound messages.
type Sub struct {
	C      chan []byte
	closed bool
}

type Hub struct {
	mu        sync.RWMutex
	topics    map[string]json.RawMessage
	subs      map[*Sub]struct{}
	handlers  map[string]Handler
	events    []Event
	eventSeq  int64
	listeners []func(Event)
	// topicListeners see every changed topic value, called outside the hub lock.
	topicListeners []func(topic string, data json.RawMessage)
}

func New() *Hub {
	return &Hub{
		topics:   make(map[string]json.RawMessage),
		subs:     make(map[*Sub]struct{}),
		handlers: make(map[string]Handler),
	}
}

type topicMessage struct {
	Type  string          `json:"type"`
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}

type eventMessage struct {
	Type string `json:"type"`
	Data Event  `json:"data"`
}

// Publish stores the latest value of a topic and broadcasts it if it changed.
func (hub *Hub) Publish(topic string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		log.Printf("bus: failed to marshal topic %s: %v", topic, err)
		return
	}
	hub.mu.Lock()
	if previous, ok := hub.topics[topic]; ok && bytes.Equal(previous, data) {
		hub.mu.Unlock()
		return
	}
	hub.topics[topic] = data
	message, _ := json.Marshal(topicMessage{Type: "topic", Topic: topic, Data: data})
	hub.broadcastLocked(message)
	listeners := hub.topicListeners
	hub.mu.Unlock()
	for _, listener := range listeners {
		callListener(topic, func() { listener(topic, data) })
	}
}

// Delete removes a topic; clients receive it with null data and drop their copy.
func (hub *Hub) Delete(topic string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if _, ok := hub.topics[topic]; !ok {
		return
	}
	delete(hub.topics, topic)
	message, _ := json.Marshal(topicMessage{Type: "topic", Topic: topic, Data: json.RawMessage("null")})
	hub.broadcastLocked(message)
}

// Topics returns a copy of every topic's latest value.
func (hub *Hub) Topics() map[string]json.RawMessage {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	topics := make(map[string]json.RawMessage, len(hub.topics))
	for name, data := range hub.topics {
		topics[name] = data
	}
	return topics
}

// OnTopic registers an in-process listener for every changed topic value.
func (hub *Hub) OnTopic(listener func(topic string, data json.RawMessage)) {
	hub.mu.Lock()
	hub.topicListeners = append(hub.topicListeners, listener)
	hub.mu.Unlock()
}

// Get returns the latest raw JSON for a topic.
func (hub *Hub) Get(topic string) json.RawMessage {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return hub.topics[topic]
}

// Logf records an informational event.
func (hub *Hub) Logf(source, format string, args ...any) {
	hub.Emit("info", source, fmt.Sprintf(format, args...))
}

// Warnf records a warning event.
func (hub *Hub) Warnf(source, format string, args ...any) {
	hub.Emit("warn", source, fmt.Sprintf(format, args...))
}

// Errorf records an error event.
func (hub *Hub) Errorf(source, format string, args ...any) {
	hub.Emit("error", source, fmt.Sprintf(format, args...))
}

// Emit records an event at the given level and broadcasts it.
func (hub *Hub) Emit(level, source, msg string) {
	hub.mu.Lock()
	hub.eventSeq++
	event := Event{Seq: hub.eventSeq, Time: time.Now(), Level: level, Source: source, Msg: msg}
	hub.events = append(hub.events, event)
	if len(hub.events) > eventHistorySize {
		hub.events = hub.events[len(hub.events)-eventHistorySize:]
	}
	message, _ := json.Marshal(eventMessage{Type: "event", Data: event})
	hub.broadcastLocked(message)
	listeners := hub.listeners
	hub.mu.Unlock()

	log.Printf("[%s] %s: %s", strings.ToUpper(level), source, msg)
	for _, listener := range listeners {
		callListener("event", func() { listener(event) })
	}
}

// Events returns a copy of the recent event history.
func (hub *Hub) Events() []Event {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return append([]Event(nil), hub.events...)
}

// OnEvent registers an in-process listener for every event.
func (hub *Hub) OnEvent(listener func(Event)) {
	hub.mu.Lock()
	hub.listeners = append(hub.listeners, listener)
	hub.mu.Unlock()
}

// Handle registers a command handler.
func (hub *Hub) Handle(command string, handler Handler) {
	hub.mu.Lock()
	hub.handlers[command] = handler
	hub.mu.Unlock()
}

// Dispatch runs a command handler. A panicking handler is reported as a failed command (with its stack in the log)
// instead of taking the simulator down.
func (hub *Hub) Dispatch(command string, args json.RawMessage) (result any, err error) {
	hub.mu.RLock()
	handler, ok := hub.handlers[command]
	hub.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown command %q", command)
	}
	defer func() {
		if value := recover(); value != nil {
			log.Printf("panic in command %s: %v\n%s", command, value, debug.Stack())
			result, err = nil, fmt.Errorf("command %s crashed: %v", command, value)
			hub.Errorf("bus", "Command %s crashed: %v (stack in the log file)", command, value)
		}
	}()
	return handler(args)
}

// Commands lists the registered command names.
func (hub *Hub) Commands() []string {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	names := make([]string, 0, len(hub.handlers))
	for name := range hub.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Subscribe creates a subscription primed with every topic's latest value and the recent event history.
func (hub *Hub) Subscribe() *Sub {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	sub := &Sub{C: make(chan []byte, subQueueSize+len(hub.topics)+eventHistorySize)}
	names := make([]string, 0, len(hub.topics))
	for name := range hub.topics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		message, _ := json.Marshal(topicMessage{Type: "topic", Topic: name, Data: hub.topics[name]})
		sub.C <- message
	}
	for _, event := range hub.events {
		message, _ := json.Marshal(eventMessage{Type: "event", Data: event})
		sub.C <- message
	}
	hub.subs[sub] = struct{}{}
	return sub
}

// Unsubscribe removes and closes a subscription.
func (hub *Hub) Unsubscribe(sub *Sub) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if _, ok := hub.subs[sub]; ok {
		delete(hub.subs, sub)
		if !sub.closed {
			sub.closed = true
			close(sub.C)
		}
	}
}

func (hub *Hub) broadcastLocked(message []byte) {
	for sub := range hub.subs {
		select {
		case sub.C <- message:
		default:
			// The client is too far behind; drop it so it reconnects and receives a fresh snapshot.
			delete(hub.subs, sub)
			if !sub.closed {
				sub.closed = true
				close(sub.C)
			}
		}
	}
}

// callListener runs one in-process listener; a panic is logged (not emitted as an event, which could loop back into a
// failing event listener) so one subsystem's bug cannot break publishing for every other one.
func callListener(what string, fn func()) {
	defer func() {
		if value := recover(); value != nil {
			log.Printf("[ERROR] bus: listener for %s crashed: %v\n%s", what, value, debug.Stack())
		}
	}()
	fn()
}
