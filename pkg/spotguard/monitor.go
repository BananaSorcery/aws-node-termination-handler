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
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// MonitorConfig holds configuration for the scale-down monitor
type MonitorConfig struct {
	CheckInterval         time.Duration
	SpotStabilityDuration time.Duration
	MaxClusterUtilization float64
	PodEvictionTimeout    time.Duration
	CleanupInterval       time.Duration
	MaxEventAge           time.Duration
}

// DefaultMonitorConfig returns the recommended default configuration
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		CheckInterval:         30 * time.Second,
		SpotStabilityDuration: 2 * time.Minute,
		MaxClusterUtilization: 75.0, // 75%
		PodEvictionTimeout:    5 * time.Minute,
		CleanupInterval:       10 * time.Minute,
		MaxEventAge:           24 * time.Hour,
	}
}

// Monitor continuously monitors fallback events and scales down on-demand when safe
type Monitor struct {
	config            MonitorConfig
	tracker           *FallbackTracker
	healthChecker     *HealthChecker
	safetyChecker     *SafetyChecker
	scaleDownExecutor *ScaleDownExecutor
}

// NewMonitor creates a new scale-down monitor
func NewMonitor(
	config MonitorConfig,
	tracker *FallbackTracker,
	healthChecker *HealthChecker,
	safetyChecker *SafetyChecker,
	scaleDownExecutor *ScaleDownExecutor,
) *Monitor {
	return &Monitor{
		config:            config,
		tracker:           tracker,
		healthChecker:     healthChecker,
		safetyChecker:     safetyChecker,
		scaleDownExecutor: scaleDownExecutor,
	}
}

// Start begins monitoring for scale-down opportunities
func (m *Monitor) Start(ctx context.Context) {
	log.Info().
		Dur("checkInterval", m.config.CheckInterval).
		Dur("stabilityDuration", m.config.SpotStabilityDuration).
		Float64("maxUtilization", m.config.MaxClusterUtilization).
		Msg("Starting on-demand scale-down monitor")

	checkTicker := time.NewTicker(m.config.CheckInterval)
	defer checkTicker.Stop()

	cleanupTicker := time.NewTicker(m.config.CleanupInterval)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Stopping on-demand scale-down monitor")
			return

		case <-checkTicker.C:
			m.checkAndScaleDown(ctx)

		case <-cleanupTicker.C:
			m.cleanup()
		}
	}
}

// checkAndScaleDown checks all active fallback events and scales down if conditions are met
func (m *Monitor) checkAndScaleDown(ctx context.Context) {
	events := m.tracker.GetActiveEvents()
	if len(events) == 0 {
		return
	}

	log.Debug().Int("activeEvents", len(events)).Msg("Checking for scale-down opportunities")

	for _, event := range events {
		// Skip if already processed
		if event.ScaleDownInitiated {
			continue
		}

		// Check all conditions
		if shouldScale, reason := m.shouldScaleDownEvent(ctx, event); shouldScale {
			// All conditions met - scale down!
			if err := m.executeScaleDown(ctx, event); err != nil {
				log.Error().
					Err(err).
					Str("eventID", event.EventID).
					Str("node", event.OnDemandNodeName).
					Msg("Failed to scale down on-demand node")
			}
		} else if reason != "" {
			log.Debug().
				Str("eventID", event.EventID).
				Str("reason", reason).
				Msg("Cannot scale down yet")
		}
	}
}

// shouldScaleDownEvent checks if all conditions are met for scaling down
func (m *Monitor) shouldScaleDownEvent(ctx context.Context, event *FallbackEvent) (bool, string) {
	// 1. Check minimum wait time
	if canScale, reason := m.safetyChecker.CanScaleDownOnDemand(event); !canScale {
		return false, reason
	}

	// 2. Check spot capacity health and stability
	isRestored, newHealthySince, err := m.healthChecker.IsSpotCapacityRestored(
		ctx,
		event.SpotASGName,
		m.config.SpotStabilityDuration,
		event.SpotHealthySince,
	)
	if err != nil {
		log.Warn().Err(err).Str("eventID", event.EventID).Msg("Failed to check spot health")
		return false, "spot health check failed"
	}

	// Update spot healthy timestamp
	if newHealthySince != event.SpotHealthySince {
		m.tracker.UpdateEvent(event.EventID, func(e *FallbackEvent) {
			e.SpotHealthySince = newHealthySince
		})
	}

	if !isRestored {
		return false, "spot capacity not yet stable"
	}

	// 3. Check if on-demand node can be safely drained
	if canDrain, reason := m.safetyChecker.CanSafelyDrainNode(ctx, event.OnDemandNodeName); !canDrain {
		return false, reason
	}

	return true, ""
}

// executeScaleDown performs the scale-down operation
func (m *Monitor) executeScaleDown(ctx context.Context, event *FallbackEvent) error {
	log.Info().
		Str("eventID", event.EventID).
		Str("node", event.OnDemandNodeName).
		Str("spotASG", event.SpotASGName).
		Str("onDemandASG", event.OnDemandASGName).
		Dur("timeSinceFailover", time.Since(event.Timestamp)).
		Msg("Executing on-demand scale-down")

	// Mark as scale-down initiated (prevents duplicate processing)
	m.tracker.UpdateEvent(event.EventID, func(e *FallbackEvent) {
		e.ScaleDownInitiated = true
		e.SpotCapacityRestored = true
	})

	// Execute the scale-down
	if err := m.scaleDownExecutor.ScaleDownOnDemandNode(ctx, event); err != nil {
		// Revert flag on failure
		m.tracker.UpdateEvent(event.EventID, func(e *FallbackEvent) {
			e.ScaleDownInitiated = false
		})
		return err
	}

	log.Info().
		Str("eventID", event.EventID).
		Str("node", event.OnDemandNodeName).
		Dur("totalDuration", time.Since(event.Timestamp)).
		Msg("Successfully completed on-demand scale-down")

	// Emit metrics (placeholder for your metrics implementation)
	m.emitMetrics(event)

	return nil
}

// cleanup removes old processed events
func (m *Monitor) cleanup() {
	m.tracker.CleanupOldEvents(m.config.MaxEventAge)
	log.Debug().Int("trackedEvents", m.tracker.GetEventCount()).Msg("Cleaned up old events")
}

// emitMetrics emits metrics for monitoring (implement based on your metrics system)
func (m *Monitor) emitMetrics(event *FallbackEvent) {
	duration := time.Since(event.Timestamp)

	log.Info().
		Str("metric", "ondemand_runtime_seconds").
		Float64("value", duration.Seconds()).
		Str("spotASG", event.SpotASGName).
		Str("onDemandASG", event.OnDemandASGName).
		Msg("On-demand instance runtime metric")

	// TODO: Integrate with your Prometheus metrics
	// Example:
	// metrics.OnDemandRuntimeSeconds.WithLabelValues(
	//     event.SpotASGName,
	//     event.OnDemandASGName,
	// ).Observe(duration.Seconds())
}
