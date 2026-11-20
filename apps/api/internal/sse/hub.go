package sse

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Event represents a Server-Sent Event message.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Hub manages client SSE connections for job progress and live logs.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[chan Event]struct{}
}

// NewHub creates a new SSE Hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]map[chan Event]struct{}),
	}
}

// Subscribe adds a channel for a specific job ID.
func (h *Hub) Subscribe(jobID string) chan Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan Event, 64)
	if _, ok := h.clients[jobID]; !ok {
		h.clients[jobID] = make(map[chan Event]struct{})
	}
	h.clients[jobID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a channel for a job ID.
func (h *Hub) Unsubscribe(jobID string, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.clients[jobID]; ok {
		delete(subs, ch)
		close(ch)
		if len(subs) == 0 {
			delete(h.clients, jobID)
		}
	}
}

// Broadcast sends an event to all subscribers of a job ID.
func (h *Hub) Broadcast(jobID string, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	subs, ok := h.clients[jobID]
	if !ok {
		return
	}

	for ch := range subs {
		select {
		case ch <- ev:
		default:
			// Buffer full, drop event to prevent blocking orchestrator
		}
	}
}

// BroadcastStage emits a stage transition event.
func (h *Hub) BroadcastStage(jobID, stage string, progress int) {
	h.Broadcast(jobID, Event{
		Type: "stage",
		Data: map[string]interface{}{
			"stage":     stage,
			"progress":  progress,
			"timestamp": time.Now().Format(time.RFC3339),
		},
	})
	h.BroadcastProgress(jobID, progress, stage)
}

// BroadcastProgress emits a progress percentage event.
func (h *Hub) BroadcastProgress(jobID string, progress int, stage string) {
	h.Broadcast(jobID, Event{
		Type: "progress",
		Data: map[string]interface{}{
			"progress": progress,
			"stage":    stage,
		},
	})
}

// BroadcastStepProgress emits granular sub-progress for an individual stage.
func (h *Hub) BroadcastStepProgress(jobID, stage string, percent int, detail string) {
	h.Broadcast(jobID, Event{
		Type: "step_progress",
		Data: map[string]interface{}{
			"stage":   stage,
			"percent": percent,
			"detail":  detail,
		},
	})
}

// BroadcastLog emits a structured log message.
func (h *Hub) BroadcastLog(jobID, stage, level, message string) {
	formatted := fmt.Sprintf("[%s] [job=%s] [stage=%s] %s", level, jobID, stage, message)
	h.Broadcast(jobID, Event{
		Type: "log",
		Data: map[string]interface{}{
			"stage":     stage,
			"level":     level,
			"message":   message,
			"formatted": formatted,
			"timestamp": time.Now().Format("15:04:05"),
		},
	})
}

// BroadcastError emits an error event.
func (h *Hub) BroadcastError(jobID, code, message string) {
	h.Broadcast(jobID, Event{
		Type: "error",
		Data: map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}

// BroadcastCompleted emits a completion event.
func (h *Hub) BroadcastCompleted(jobID string) {
	h.Broadcast(jobID, Event{
		Type: "completed",
		Data: map[string]interface{}{
			"job_id":    jobID,
			"timestamp": time.Now().Format(time.RFC3339),
		},
	})
}

// FormatSSE formats an event into standard SSE byte stream.
func FormatSSE(ev Event) ([]byte, error) {
	dataBytes, err := json.Marshal(ev.Data)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Type, string(dataBytes))), nil
}
