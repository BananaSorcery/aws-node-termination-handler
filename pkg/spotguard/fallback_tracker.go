// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package spotguard

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// FallbackEvent represents a fallback to on-demand instance when spot capacity is unavailable
type FallbackEvent struct {
	EventID              string
	Timestamp            time.Time
	SpotASGName          string
	OnDemandASGName      string
	OnDemandInstanceID   string
	OnDemandNodeName     string
	SpotCapacityRestored bool
	ScaleDownInitiated   bool
	MinimumWaitDuration  time.Duration
	SpotHealthySince     *time.Time // When spot became healthy
}

// FallbackTracker manages multiple fallback events
type FallbackTracker struct {
	events map[string]*FallbackEvent
	mutex  sync.RWMutex
}

// NewFallbackTracker creates a new fallback tracker
func NewFallbackTracker() *FallbackTracker {
	return &FallbackTracker{
		events: make(map[string]*FallbackEvent),
	}
}

// AddEvent adds a new fallback event to track
func (ft *FallbackTracker) AddEvent(event *FallbackEvent) {
	ft.mutex.Lock()
	defer ft.mutex.Unlock()

	ft.events[event.EventID] = event
	log.Info().
		Str("eventID", event.EventID).
		Str("onDemandASG", event.OnDemandASGName).
		Str("spotASG", event.SpotASGName).
		Str("onDemandNode", event.OnDemandNodeName).
		Msg("Tracking new fallback event")
}

// GetEvent retrieves a specific fallback event
func (ft *FallbackTracker) GetEvent(eventID string) (*FallbackEvent, bool) {
	ft.mutex.RLock()
	defer ft.mutex.RUnlock()

	event, exists := ft.events[eventID]
	return event, exists
}

// GetActiveEvents returns all events that are not yet processed
func (ft *FallbackTracker) GetActiveEvents() []*FallbackEvent {
	ft.mutex.RLock()
	defer ft.mutex.RUnlock()

	active := make([]*FallbackEvent, 0)
	for _, event := range ft.events {
		if !event.ScaleDownInitiated {
			active = append(active, event)
		}
	}
	return active
}

// UpdateEvent updates an existing event
func (ft *FallbackTracker) UpdateEvent(eventID string, updateFunc func(*FallbackEvent)) {
	ft.mutex.Lock()
	defer ft.mutex.Unlock()

	if event, exists := ft.events[eventID]; exists {
		updateFunc(event)
	}
}

// RemoveEvent removes a fallback event from tracking
func (ft *FallbackTracker) RemoveEvent(eventID string) {
	ft.mutex.Lock()
	defer ft.mutex.Unlock()

	delete(ft.events, eventID)
	log.Info().Str("eventID", eventID).Msg("Removed fallback event from tracking")
}

// CleanupOldEvents removes events older than the specified duration
func (ft *FallbackTracker) CleanupOldEvents(maxAge time.Duration) {
	ft.mutex.Lock()
	defer ft.mutex.Unlock()

	now := time.Now()
	for eventID, event := range ft.events {
		if event.ScaleDownInitiated && now.Sub(event.Timestamp) > maxAge {
			delete(ft.events, eventID)
			log.Debug().Str("eventID", eventID).Msg("Cleaned up old fallback event")
		}
	}
}

// GetEventCount returns the number of tracked events
func (ft *FallbackTracker) GetEventCount() int {
	ft.mutex.RLock()
	defer ft.mutex.RUnlock()
	return len(ft.events)
}
