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
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
)

// This file provides integration examples for using the spot guard package

// InitializeSpotGuard initializes the spot guard system
// Call this from your main() function in cmd/node-termination-handler.go
func InitializeSpotGuard(
	ctx context.Context,
	config Config,
	asgClient *autoscaling.AutoScaling,
	k8sClient kubernetes.Interface,
	nodeHandler node.Node,
) (*Monitor, *FallbackTracker, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid spot guard configuration: %w", err)
	}

	if !config.Enabled {
		log.Info().Msg("Spot guard on-demand scale-down is disabled")
		return nil, nil, nil
	}

	log.Info().
		Str("spotASG", config.SpotASGName).
		Str("onDemandASG", config.OnDemandASGName).
		Dur("minWait", config.MinimumWaitDuration).
		Dur("checkInterval", config.CheckInterval).
		Msg("Initializing spot guard")

	// Create fallback tracker
	tracker := NewFallbackTracker()

	// Create health checker
	healthChecker := NewHealthChecker(asgClient, k8sClient)

	// Create safety checker
	safetyChecker := NewSafetyChecker(k8sClient, config.MaxClusterUtilization)

	// Create scale-down executor
	scaleDownExecutor := NewScaleDownExecutor(
		asgClient,
		k8sClient,
		nodeHandler,
		config.PodEvictionTimeout,
	)

	// Create monitor configuration
	monitorConfig := MonitorConfig{
		CheckInterval:         config.CheckInterval,
		SpotStabilityDuration: config.SpotStabilityDuration,
		MaxClusterUtilization: config.MaxClusterUtilization,
		PodEvictionTimeout:    config.PodEvictionTimeout,
		CleanupInterval:       config.CleanupInterval,
		MaxEventAge:           config.MaxEventAge,
	}

	// Create monitor
	monitor := NewMonitor(
		monitorConfig,
		tracker,
		healthChecker,
		safetyChecker,
		scaleDownExecutor,
	)

	// Start monitoring in background
	go monitor.Start(ctx)

	log.Info().Msg("Spot guard initialized and monitoring started")

	return monitor, tracker, nil
}

// RecordFallbackEvent records a fallback event when on-demand scaling occurs
// Call this from your rebalance handler when spot scale-up fails
func RecordFallbackEvent(
	tracker *FallbackTracker,
	config Config,
	instanceID string,
	nodeName string,
) {
	if tracker == nil || !config.Enabled {
		return
	}

	event := &FallbackEvent{
		EventID:             fmt.Sprintf("fallback-%s-%d", instanceID, time.Now().Unix()),
		Timestamp:           time.Now(),
		SpotASGName:         config.SpotASGName,
		OnDemandASGName:     config.OnDemandASGName,
		OnDemandInstanceID:  instanceID,
		OnDemandNodeName:    nodeName,
		MinimumWaitDuration: config.MinimumWaitDuration,
	}

	tracker.AddEvent(event)

	log.Info().
		Str("eventID", event.EventID).
		Str("instanceID", instanceID).
		Str("nodeName", nodeName).
		Msg("Recorded fallback to on-demand event")
}

// Example integration in your rebalance handler:
//
// func handleRebalanceRecommendation(rebalanceEvent *RebalanceEvent) error {
//     // Step 1: Try to scale up spot
//     err := scaleUpSpotASG(spotASGName, 1)
//     if err != nil {
//         log.Warn().Err(err).Msg("Failed to scale up spot, falling back to on-demand")
//
//         // Step 2: Scale up on-demand as fallback
//         instanceID, nodeName, err := scaleUpOnDemandASG(onDemandASGName, 1)
//         if err != nil {
//             return fmt.Errorf("failed to scale up on-demand: %w", err)
//         }
//
//         // Step 3: Record the fallback event (spot guard will handle scale-down)
//         RecordFallbackEvent(fallbackTracker, spotGuardConfig, instanceID, nodeName)
//     }
//
//     // Step 4: Taint the instance receiving rebalance recommendation
//     return taintNode(rebalanceEvent.NodeName)
// }
