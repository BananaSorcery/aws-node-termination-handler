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

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Node annotation keys
	AnnotationStartTime     = "spot-guard.aws.amazon.com/on-demand-start-time"
	AnnotationSpotASG       = "spot-guard.aws.amazon.com/spot-asg-name"
	AnnotationOnDemandASG   = "spot-guard.aws.amazon.com/on-demand-asg-name"
	AnnotationScaleDownDone = "spot-guard.aws.amazon.com/scale-down-completed"
)

// SelfMonitor monitors its own node and scales it down when spot capacity is restored
type SelfMonitor struct {
	config            config.Config
	healthChecker     *HealthChecker
	safetyChecker     *SafetyChecker
	scaleDownExecutor *ScaleDownExecutor
	clientset         kubernetes.Interface
	startTime         time.Time
	healthySince      *time.Time
	nodeName          string
	spotASGName       string
	onDemandASGName   string
	instanceID        string
}

// NewSelfMonitor creates a new self-monitor for the current on-demand node
func NewSelfMonitor(
	asgClient autoscalingiface.AutoScalingAPI,
	clientset kubernetes.Interface,
	nodeHandler node.Node,
	nthConfig config.Config,
) *SelfMonitor {
	healthChecker := NewHealthChecker(asgClient, clientset)
	safetyChecker := NewSafetyChecker(clientset, float64(nthConfig.SpotGuardMaxClusterUtilization))
	scaleDownExecutor := NewScaleDownExecutor(
		asgClient,
		clientset,
		nodeHandler,
		time.Duration(nthConfig.SpotGuardPodEvictionTimeout)*time.Second,
	)

	sm := &SelfMonitor{
		config:            nthConfig,
		healthChecker:     healthChecker,
		safetyChecker:     safetyChecker,
		scaleDownExecutor: scaleDownExecutor,
		clientset:         clientset,
		nodeName:          nthConfig.NodeName,
		spotASGName:       nthConfig.SpotAsgName,
		onDemandASGName:   nthConfig.OnDemandAsgName,
	}

	// Get instance ID from node
	sm.instanceID = sm.getInstanceID()

	// Load or create start time from node annotation
	sm.startTime = sm.getOrCreateStartTime()

	return sm
}

// Start begins monitoring this node for scale-down
func (sm *SelfMonitor) Start(ctx context.Context) {
	checkInterval := time.Duration(sm.config.SpotGuardCheckInterval) * time.Second
	minimumWaitDuration := time.Duration(sm.config.SpotGuardMinimumWaitDuration) * time.Second

	// Add jitter to prevent thundering herd (all pods checking at the same time)
	// Jitter spreads API calls over a 10-second window instead of all at once
	jitter := time.Duration(time.Now().UnixNano()%10) * time.Second
	actualCheckInterval := checkInterval + jitter

	log.Info().
		Msg("ON-DEMAND NODE DETECTED - Starting Spot Guard Self-Monitor")
	log.Info().
		Str("nodeName", sm.nodeName).
		Str("instanceID", sm.instanceID).
		Str("onDemandASG", sm.onDemandASGName)
	log.Info().
		Time("monitorStartTime", sm.startTime).
		Int("minimumWaitSeconds", sm.config.SpotGuardMinimumWaitDuration).
		Msg("On-demand node will run for at least this duration before scale-down")
	log.Info().
		Dur("checkInterval", checkInterval).
		Dur("jitter", jitter).
		Dur("actualCheckInterval", actualCheckInterval).
		Msg("Health check configuration")
	log.Info().
		Msg("Self-monitor active - will automatically scale down this on-demand node when spot capacity is healthy")

	ticker := time.NewTicker(actualCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("nodeName", sm.nodeName).Msg("Self-monitor stopped")
			return
		case <-ticker.C:
			if sm.checkAndScaleDown(ctx, minimumWaitDuration) {
				// Scale-down initiated successfully, monitor can exit
				log.Info().
					Str("nodeName", sm.nodeName).
					Msg("Scale-down completed, self-monitor exiting")
				return
			}
		}
	}
}

// checkAndScaleDown checks if this node should be scaled down
// Returns true if scale-down was initiated
func (sm *SelfMonitor) checkAndScaleDown(ctx context.Context, minimumWaitDuration time.Duration) bool {
	// Check if scale-down already completed (in case of pod restart after scale-down initiated)
	if sm.isScaleDownCompleted() {
		log.Info().Str("nodeName", sm.nodeName).Msg("Scale-down already completed, monitor exiting")
		return true
	}

	// Step 1: Check minimum wait time
	elapsed := time.Since(sm.startTime)
	if elapsed < minimumWaitDuration {
		remaining := minimumWaitDuration - elapsed
		log.Debug().
			Str("nodeName", sm.nodeName).
			Dur("elapsed", elapsed).
			Dur("remaining", remaining).
			Msg("Minimum wait time not met yet")
		return false
	}

	// Step 2: Check if spot ASG is healthy
	isHealthy, err := sm.healthChecker.IsSpotASGHealthy(ctx, sm.spotASGName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("spotASG", sm.spotASGName).
			Msg("Failed to check spot ASG health")
		return false
	}
	if !isHealthy {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Msg("Spot ASG not yet healthy")
		return false
	}

	// Step 3: Check spot nodes readiness in Kubernetes
	areNodesReady, err := sm.healthChecker.AreSpotNodesReady(ctx, sm.spotASGName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("spotASG", sm.spotASGName).
			Msg("Failed to check spot nodes readiness")
		return false
	}
	if !areNodesReady {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Msg("Spot nodes not yet ready in Kubernetes")
		return false
	}

	// Step 4: Check spot stability
	stabilityDuration := time.Duration(sm.config.SpotGuardSpotStabilityDuration) * time.Second
	isStable, newHealthySince, err := sm.healthChecker.IsSpotCapacityStable(ctx, sm.spotASGName, stabilityDuration, sm.healthySince)
	if err != nil {
		log.Warn().
			Err(err).
			Str("spotASG", sm.spotASGName).
			Msg("Failed to check spot stability")
		return false
	}
	sm.healthySince = newHealthySince // Update the healthy since time
	if !isStable {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Dur("requiredStability", stabilityDuration).
			Msg("Spot capacity not yet stable")
		return false
	}

	// Step 5: Check if this node can be safely drained
	canDrain, reason := sm.safetyChecker.CanSafelyDrainNode(ctx, sm.nodeName)
	if !canDrain {
		log.Debug().
			Str("nodeName", sm.nodeName).
			Str("reason", reason).
			Msg("Cannot safely drain node yet")
		return false
	}

	// Step 6: All conditions met - scale down THIS node
	log.Info().
		Str("nodeName", sm.nodeName).
		Str("spotASG", sm.spotASGName).
		Str("onDemandASG", sm.onDemandASGName).
		Dur("onDemandRuntime", elapsed).
		Msg("All conditions met, initiating scale-down of this on-demand node")

	// Create a fallback event for this node
	event := &FallbackEvent{
		EventID:              fmt.Sprintf("self-monitor-%s-%d", sm.nodeName, time.Now().Unix()),
		Timestamp:            sm.startTime,
		SpotASGName:          sm.spotASGName,
		OnDemandASGName:      sm.onDemandASGName,
		OnDemandNodeName:     sm.nodeName,
		OnDemandInstanceID:   sm.getInstanceID(),
		ScaleDownInitiated:   true,
		SpotCapacityRestored: true,
	}

	// Mark scale-down as initiated (prevents duplicate attempts if pod restarts during scale-down)
	if err := sm.markScaleDownInitiated(); err != nil {
		log.Warn().Err(err).Msg("Failed to mark scale-down as initiated, continuing anyway")
	}

	// Execute scale-down
	if err := sm.scaleDownExecutor.ScaleDownOnDemandNode(ctx, event); err != nil {
		log.Error().
			Err(err).
			Str("nodeName", sm.nodeName).
			Str("eventID", event.EventID).
			Msg("Failed to scale down on-demand node")
		return false
	}

	log.Info().
		Str("nodeName", sm.nodeName).
		Str("eventID", event.EventID).
		Dur("totalRuntime", elapsed).
		Msg("Successfully scaled down this on-demand node")

	return true
}

// getOrCreateStartTime loads the start time from node annotation or creates a new one
func (sm *SelfMonitor) getOrCreateStartTime() time.Time {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		log.Warn().
			Err(err).
			Str("nodeName", sm.nodeName).
			Msg("Failed to get node, using current time as start time")
		return time.Now()
	}

	// Check for existing start time annotation
	if startTimeStr, exists := node.Annotations[AnnotationStartTime]; exists {
		if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			log.Info().
				Time("startTime", startTime).
				Str("nodeName", sm.nodeName).
				Msg("Loaded start time from node annotation (pod restart detected)")
			return startTime
		}
		log.Warn().
			Err(err).
			Str("startTimeStr", startTimeStr).
			Msg("Failed to parse start time annotation, creating new one")
	}

	// Create new start time annotation
	startTime := time.Now()
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[AnnotationStartTime] = startTime.Format(time.RFC3339)
	node.Annotations[AnnotationSpotASG] = sm.spotASGName
	node.Annotations[AnnotationOnDemandASG] = sm.onDemandASGName

	_, err = sm.clientset.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	if err != nil {
		log.Warn().
			Err(err).
			Str("nodeName", sm.nodeName).
			Msg("Failed to set start time annotation, will retry")
		// Return the time anyway, we'll try to update again next cycle
	} else {
		log.Info().
			Time("startTime", startTime).
			Str("nodeName", sm.nodeName).
			Msg("Created start time annotation on node")
	}

	return startTime
}

// markScaleDownInitiated marks that scale-down has been initiated
func (sm *SelfMonitor) markScaleDownInitiated() error {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[AnnotationScaleDownDone] = time.Now().Format(time.RFC3339)

	_, err = sm.clientset.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node annotation: %w", err)
	}

	log.Debug().
		Str("nodeName", sm.nodeName).
		Msg("Marked scale-down as initiated in node annotation")
	return nil
}

// isScaleDownCompleted checks if scale-down was already completed
func (sm *SelfMonitor) isScaleDownCompleted() bool {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		// If we can't get the node, assume scale-down not completed
		return false
	}

	_, exists := node.Annotations[AnnotationScaleDownDone]
	return exists
}

// getInstanceID gets the EC2 instance ID for this node
func (sm *SelfMonitor) getInstanceID() string {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		return ""
	}

	// Extract instance ID from provider ID (format: aws:///us-west-2a/i-1234567890abcdef0)
	providerID := node.Spec.ProviderID
	if len(providerID) > 0 {
		// Simple extraction - split by / and get last part
		parts := []rune(providerID)
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == '/' {
				return string(parts[i+1:])
			}
		}
	}

	return ""
}
