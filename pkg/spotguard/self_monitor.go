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
		Str("checkInterval", checkInterval.String()).
		Str("jitter", jitter.String()).
		Str("actualCheckInterval", actualCheckInterval.String()).
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
				// Scale-down initiated - now verify THIS node is actually terminating
				log.Info().
					Str("nodeName", sm.nodeName).
					Msg("Scale-down initiated, verifying THIS node is being terminated")

				if sm.waitForThisNodeTermination(ctx) {
					log.Info().
						Str("nodeName", sm.nodeName).
						Msg("THIS node is terminating, will keep monitoring until pod dies naturally")
					// Don't exit! Let node termination kill the pod
					// Continue loop to keep monitoring
				} else {
					// Verification timeout - wrong node was terminated
					log.Warn().
						Str("nodeName", sm.nodeName).
						Msg("Verification timeout: THIS node still running - clearing marker to retry on next cycle")

					if err := sm.clearScaleDownMarker(); err != nil {
						log.Error().Err(err).Msg("Failed to clear scale-down marker")
					}
				}
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

	// Step 2: Comprehensive spot ASG check (ONE API call instead of 3!)
	stabilityDuration := time.Duration(sm.config.SpotGuardSpotStabilityDuration) * time.Second
	status, err := sm.healthChecker.CheckSpotASGComprehensive(
		ctx,
		sm.spotASGName,
		stabilityDuration,
		sm.healthySince,
	)
	if err != nil {
		log.Warn().
			Err(err).
			Str("spotASG", sm.spotASGName).
			Msg("Failed to perform comprehensive spot ASG check")
		return false
	}

	// Check ASG health
	if !status.IsHealthy {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Msg("Spot ASG not yet healthy")
		return false
	}

	// Check nodes readiness
	if !status.NodesReady {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Msg("Spot nodes not yet ready in Kubernetes")
		return false
	}

	// Update and check stability
	sm.healthySince = status.HealthySince
	if !status.IsStable {
		log.Debug().
			Str("spotASG", sm.spotASGName).
			Dur("requiredStability", stabilityDuration).
			Msg("Spot capacity not yet stable")
		return false
	}

	// Step 3: Check if this node can be safely drained
	canDrain, reason := sm.safetyChecker.CanSafelyDrainNode(ctx, sm.nodeName)
	if !canDrain {
		// Check if we should attempt pre-scale
		if sm.config.EnablePreScale && reason == "Cluster utilization too high" {
			log.Info().
				Str("nodeName", sm.nodeName).
				Str("reason", reason).
				Msg("Cluster utilization too high, attempting smart pre-scale")

			// Try pre-scale with 3-level fallback
			preScaleSuccess := sm.attemptPreScaleWithFallback(ctx)
			if preScaleSuccess {
				log.Info().Msg("Pre-scale successful, will retry drain on next check cycle")
				return false // Will retry on next cycle
			}

			log.Warn().Msg("Pre-scale failed, keeping on-demand node for now")
			return false
		}

		log.Debug().
			Str("nodeName", sm.nodeName).
			Str("reason", reason).
			Msg("Cannot safely drain node yet")
		return false
	}

	// Step 4: All conditions met - scale down THIS node
	log.Info().
		Str("nodeName", sm.nodeName).
		Str("spotASG", sm.spotASGName).
		Str("onDemandASG", sm.onDemandASGName).
		Dur("onDemandRuntime", elapsed).
		Msg("All conditions met, initiating scale-down of this on-demand node")

	// Apply execution jitter to prevent simultaneous scale-downs from multiple daemonset pods
	// Random delay between 0-30 seconds to spread out scale-down operations
	executionJitter := time.Duration(time.Now().UnixNano()%30) * time.Second
	log.Info().
		Dur("executionJitter", executionJitter).
		Msg("Applying execution jitter to prevent simultaneous scale-downs from multiple pods")

	select {
	case <-time.After(executionJitter):
		// Continue with scale-down
	case <-ctx.Done():
		log.Info().Msg("Context cancelled during execution jitter, aborting scale-down")
		return false
	}

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

// waitForThisNodeTermination waits to verify THIS specific node is being terminated
// Returns true if this node is confirmed to be terminating, false if timeout or wrong node terminated
func (sm *SelfMonitor) waitForThisNodeTermination(ctx context.Context) bool {
	maxWaitTime := 5 * time.Minute // Wait up to 5 minutes to confirm
	checkInterval := 10 * time.Second
	startTime := time.Now()

	log.Info().
		Str("nodeName", sm.nodeName).
		Str("maxWaitTime", maxWaitTime.String()).
		Msg("Waiting to confirm THIS node is being terminated...")

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context cancelled during termination verification")
			return false

		case <-ticker.C:
			elapsed := time.Since(startTime)

			// Check if this node is being terminated
			isTerminating, reason := sm.isThisNodeTerminating()

			if isTerminating {
				log.Info().
					Str("nodeName", sm.nodeName).
					Str("reason", reason).
					Str("elapsed", elapsed.String()).
					Msg("Confirmed: THIS node is being terminated")
				return true
			}

			// Check if we've exceeded max wait time
			if elapsed >= maxWaitTime {
				log.Warn().
					Str("nodeName", sm.nodeName).
					Str("elapsed", elapsed.String()).
					Msg("Timeout: THIS node is NOT being terminated - wrong node may have been scaled down")
				return false
			}

			log.Debug().
				Str("nodeName", sm.nodeName).
				Str("elapsed", elapsed.String()).
				Str("remaining", (maxWaitTime - elapsed).String()).
				Msg("Still waiting for THIS node to show termination signs...")
		}
	}
}

// isThisNodeTerminating checks if THIS specific node is being terminated
// Returns true and reason if node is terminating
func (sm *SelfMonitor) isThisNodeTerminating() (bool, string) {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		// If node doesn't exist, it was terminated!
		log.Info().
			Err(err).
			Str("nodeName", sm.nodeName).
			Msg("Node no longer exists - successfully terminated")
		return true, "node deleted"
	}

	// Check 1: Node has DeletionTimestamp (Kubernetes is deleting it)
	if node.DeletionTimestamp != nil {
		return true, "node has DeletionTimestamp"
	}

	// NOTE: We do NOT check for cordoned/unschedulable status because:
	// - WE cordon the node ourselves during scale-down
	// - ASG might terminate a different node instead
	// - Our node would stay cordoned but alive (false positive!)
	// Only DeletionTimestamp is a reliable signal that THIS node is actually terminating

	log.Debug().
		Str("nodeName", sm.nodeName).
		Msg("Node shows no signs of termination yet")
	return false, ""
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

// clearScaleDownMarker removes the scale-down marker to allow retry
func (sm *SelfMonitor) clearScaleDownMarker() error {
	node, err := sm.clientset.CoreV1().Nodes().Get(context.Background(), sm.nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node: %w", err)
	}

	if node.Annotations != nil {
		delete(node.Annotations, AnnotationScaleDownDone)
	}

	_, err = sm.clientset.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to clear node annotation: %w", err)
	}

	log.Info().
		Str("nodeName", sm.nodeName).
		Msg("Cleared scale-down marker - retry enabled")
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

// attemptPreScaleWithFallback implements the 3-level safety net for pre-scaling
func (sm *SelfMonitor) attemptPreScaleWithFallback(ctx context.Context) bool {
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msg("LEVEL 1: Attempting Smart Pre-Scale")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Get current cluster utilization
	currentUtilization := sm.safetyChecker.GetClusterUtilization(ctx)

	log.Info().
		Float64("currentUtilization", currentUtilization).
		Float64("targetUtilization", float64(sm.config.PreScaleTargetUtilization)).
		Msg("Current cluster state")

	// Level 1: Attempt pre-scale
	calc, err := sm.healthChecker.CalculatePreScaleNodes(
		ctx,
		sm.spotASGName,
		sm.onDemandASGName,
		currentUtilization,
		float64(sm.config.PreScaleTargetUtilization),
		sm.config.PreScaleSafetyBufferPercent,
	)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Pre-scale calculation failed")
		return sm.attemptFallbackLevel2(ctx, currentUtilization)
	}

	if calc.AdditionalSpotNodes == 0 {
		log.Info().Msg("No additional nodes needed (already below target)")
		return true
	}

	log.Info().
		Int("additionalNodes", calc.AdditionalSpotNodes).
		Int("newDesiredCapacity", calc.NodesNeeded).
		Float64("expectedUtilization", calc.ExpectedUtilization).
		Msg("Pre-scale plan calculated")

	// Scale up spot ASG
	if err := sm.healthChecker.ScaleSpotASG(ctx, sm.spotASGName, calc.NodesNeeded); err != nil {
		log.Error().
			Err(err).
			Str("spotASG", sm.spotASGName).
			Int("desiredCapacity", calc.NodesNeeded).
			Msg("Failed to scale spot ASG")
		return sm.attemptFallbackLevel2(ctx, currentUtilization)
	}

	log.Info().
		Int("additionalNodes", calc.AdditionalSpotNodes).
		Int("timeoutSeconds", sm.config.PreScaleTimeoutSeconds).
		Msg("Waiting for new spot nodes to become ready...")

	// Wait for new nodes to become ready
	timeout := time.Duration(sm.config.PreScaleTimeoutSeconds) * time.Second
	success := sm.waitForSpotNodesReady(ctx, calc.NodesNeeded, timeout)

	if success {
		log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		log.Info().Msg("LEVEL 1 SUCCESS: Pre-scale completed successfully!")
		log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		return true
	}

	log.Warn().
		Dur("timeout", timeout).
		Msg("LEVEL 1 FAILED: Spot nodes not ready within timeout")

	// Spot capacity might not be available - try fallback
	return sm.attemptFallbackLevel2(ctx, currentUtilization)
}

// attemptFallbackLevel2 tries to drain with increased threshold
func (sm *SelfMonitor) attemptFallbackLevel2(ctx context.Context, currentUtilization float64) bool {
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msg(" LEVEL 2: Fallback to Increased Threshold")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fallbackThreshold := float64(sm.config.PreScaleFallbackThreshold)

	log.Info().
		Float64("currentUtilization", currentUtilization).
		Float64("originalThreshold", float64(sm.config.SpotGuardMaxClusterUtilization)).
		Float64("fallbackThreshold", fallbackThreshold).
		Msg("Comparing utilization against increased threshold")

	if currentUtilization <= fallbackThreshold {
		log.Info().
			Float64("currentUtilization", currentUtilization).
			Float64("fallbackThreshold", fallbackThreshold).
			Msg("LEVEL 2 SUCCESS: Current utilization is below fallback threshold")
		log.Info().Msg("Will proceed with drain on next check cycle")
		log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		// Temporarily increase the threshold for the safety checker
		sm.safetyChecker.maxUtilization = fallbackThreshold
		return true
	}

	log.Warn().
		Float64("currentUtilization", currentUtilization).
		Float64("fallbackThreshold", fallbackThreshold).
		Msg(" LEVEL 2 FAILED: Still too high even with increased threshold")

	// Still too high - go to Level 3
	return sm.attemptFallbackLevel3(ctx)
}

// attemptFallbackLevel3 keeps the on-demand node running
func (sm *SelfMonitor) attemptFallbackLevel3(ctx context.Context) bool {
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msg("  LEVEL 3: Keep On-Demand Node (Safety First)")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	checkInterval := time.Duration(sm.config.SpotGuardCheckInterval) * time.Second

	log.Warn().Msg("  Cannot safely drain on-demand node:")
	log.Warn().Msg("   • Spot capacity unavailable or unhealthy")
	log.Warn().Msg("   • Cluster utilization too high")
	log.Warn().Msg("   • Draining now would risk workload disruption")
	log.Warn().Msg("")
	log.Warn().Msg("  Safety First: Keeping on-demand node running")
	log.Warn().Msg(" Note: This costs more, but ensures reliability")
	log.Warn().
		Dur("retryAfter", checkInterval).
		Msg("Will retry on next check cycle")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return false
}

// waitForSpotNodesReady waits for spot nodes to reach the desired count and be ready
func (sm *SelfMonitor) waitForSpotNodesReady(ctx context.Context, desiredCount int, timeout time.Duration) bool {
	startTime := time.Now()
	checkInterval := 10 * time.Second

	for {
		elapsed := time.Since(startTime)
		if elapsed >= timeout {
			log.Warn().
				Dur("elapsed", elapsed).
				Dur("timeout", timeout).
				Msg("Timeout waiting for spot nodes to be ready")
			return false
		}

		// Check spot ASG health
		status, err := sm.healthChecker.CheckSpotASGComprehensive(
			ctx,
			sm.spotASGName,
			30*time.Second, // Short stability check for pre-scale
			nil,
		)
		if err != nil {
			log.Warn().
				Err(err).
				Msg("Failed to check spot ASG status during pre-scale wait")
			time.Sleep(checkInterval)
			continue
		}

		// Check if we have enough healthy nodes
		if status.IsHealthy && status.NodesReady {
			// Count how many nodes we have
			readyCount := len(status.InstanceIDs)

			log.Debug().
				Int("readyCount", readyCount).
				Int("desiredCount", desiredCount).
				Dur("elapsed", elapsed).
				Msg("Checking spot node readiness")

			if readyCount >= desiredCount {
				log.Info().
					Int("readyCount", readyCount).
					Int("desiredCount", desiredCount).
					Dur("elapsed", elapsed).
					Msg("All spot nodes are ready!")
				return true
			}
		}

		remaining := timeout - elapsed
		log.Debug().
			Dur("elapsed", elapsed).
			Dur("remaining", remaining).
			Msg("Waiting for spot nodes...")

		time.Sleep(checkInterval)
	}
}
