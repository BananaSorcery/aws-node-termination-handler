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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// HealthChecker performs health checks on spot capacity and cluster state
type HealthChecker struct {
	asgClient autoscalingiface.AutoScalingAPI
	k8sClient kubernetes.Interface
}

// SpotASGHealthStatus contains comprehensive health check results
type SpotASGHealthStatus struct {
	// ASG is healthy (inService >= desired)
	IsHealthy bool

	// Kubernetes nodes are ready and matched to ASG instances
	NodesReady bool

	// ASG has been healthy and stable for required duration
	IsStable bool

	// Instance IDs in the ASG (for node matching)
	InstanceIDs []string

	// When the ASG first became healthy (for stability tracking)
	HealthySince *time.Time
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(asgClient autoscalingiface.AutoScalingAPI, k8sClient kubernetes.Interface) *HealthChecker {
	return &HealthChecker{
		asgClient: asgClient,
		k8sClient: k8sClient,
	}
}

// IsSpotASGHealthy checks if the spot ASG has healthy instances matching desired capacity
func (hc *HealthChecker) IsSpotASGHealthy(ctx context.Context, asgName string) (bool, error) {
	log.Debug().Str("asg", asgName).Msg("Checking spot ASG health")

	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}

	result, err := hc.asgClient.DescribeAutoScalingGroupsWithContext(ctx, input)
	if err != nil {
		log.Error().
			Err(err).
			Str("asg", asgName).
			Msg("Failed to describe ASG")
		return false, fmt.Errorf("failed to describe ASG %s: %w", asgName, err)
	}

	if len(result.AutoScalingGroups) == 0 {
		log.Error().Str("asg", asgName).Msg("ASG not found")
		return false, fmt.Errorf("ASG %s not found", asgName)
	}

	asg := result.AutoScalingGroups[0]
	desiredCapacity := aws.Int64Value(asg.DesiredCapacity)
	minSize := aws.Int64Value(asg.MinSize)
	maxSize := aws.Int64Value(asg.MaxSize)

	// Count instances by state and health
	inServiceCount := int64(0)
	unhealthyCount := int64(0)
	pendingCount := int64(0)
	terminatingCount := int64(0)

	instanceDetails := make([]string, 0)
	for _, instance := range asg.Instances {
		instanceID := aws.StringValue(instance.InstanceId)
		lifecycleState := aws.StringValue(instance.LifecycleState)
		healthStatus := aws.StringValue(instance.HealthStatus)

		if lifecycleState == "InService" && healthStatus == "Healthy" {
			inServiceCount++
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:InService:Healthy", instanceID))
		} else if lifecycleState == "Pending" {
			pendingCount++
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:Pending:%s", instanceID, healthStatus))
		} else if lifecycleState == "Terminating" || lifecycleState == "Terminated" {
			terminatingCount++
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:%s:%s", instanceID, lifecycleState, healthStatus))
		} else if healthStatus == "Unhealthy" {
			unhealthyCount++
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:%s:Unhealthy", instanceID, lifecycleState))
		} else {
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:%s:%s", instanceID, lifecycleState, healthStatus))
		}
	}

	isHealthy := inServiceCount >= desiredCapacity

	log.Debug().
		Str("asg", asgName).
		Int64("inService", inServiceCount).
		Int64("desired", desiredCapacity).
		Int64("min", minSize).
		Int64("max", maxSize).
		Int64("pending", pendingCount).
		Int64("unhealthy", unhealthyCount).
		Int64("terminating", terminatingCount).
		Int64("totalInstances", int64(len(asg.Instances))).
		Strs("instances", instanceDetails).
		Bool("healthy", isHealthy).
		Msg("Spot ASG health check result")

	if !isHealthy {
		log.Debug().
			Str("asg", asgName).
			Msgf("Spot ASG not healthy: %d/%d instances InService", inServiceCount, desiredCapacity)
	}

	return isHealthy, nil
}

// AreSpotNodesReady checks if all nodes in the spot ASG are ready in Kubernetes
func (hc *HealthChecker) AreSpotNodesReady(ctx context.Context, asgName string) (bool, error) {
	log.Debug().Str("asg", asgName).Msg("Checking if spot nodes are ready in Kubernetes")

	// First, get instance IDs from the ASG
	asgInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}
	asgOutput, err := hc.asgClient.DescribeAutoScalingGroups(asgInput)
	if err != nil {
		return false, fmt.Errorf("failed to describe ASG: %w", err)
	}
	if len(asgOutput.AutoScalingGroups) == 0 {
		return false, fmt.Errorf("ASG not found: %s", asgName)
	}

	asg := asgOutput.AutoScalingGroups[0]
	asgInstanceIDs := make(map[string]bool)
	for _, instance := range asg.Instances {
		if instance.InstanceId != nil {
			asgInstanceIDs[*instance.InstanceId] = true
		}
	}

	log.Debug().
		Str("asg", asgName).
		Int("asgInstanceCount", len(asgInstanceIDs)).
		Msg("Got instance IDs from ASG")

	// Get all nodes in the cluster
	nodes, err := hc.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error().
			Err(err).
			Str("asg", asgName).
			Msg("Failed to list Kubernetes nodes")
		return false, fmt.Errorf("failed to list nodes: %w", err)
	}

	log.Debug().
		Str("asg", asgName).
		Int("totalNodesInCluster", len(nodes.Items)).
		Msg("Listed all nodes in cluster")

	// Find nodes belonging to the spot ASG by matching instance IDs
	spotNodes := make([]corev1.Node, 0)
	for _, node := range nodes.Items {
		// Extract instance ID from providerID (format: aws:///us-west-2a/i-0123456789abcdef0)
		instanceID := extractInstanceIDFromProviderID(node.Spec.ProviderID)
		if instanceID != "" && asgInstanceIDs[instanceID] {
			log.Debug().
				Str("node", node.Name).
				Str("instanceID", instanceID).
				Str("asg", asgName).
				Msg("Found spot node via instance ID match")
			spotNodes = append(spotNodes, node)
		}
	}

	if len(spotNodes) == 0 {
		log.Debug().
			Str("asg", asgName).
			Int("totalNodesChecked", len(nodes.Items)).
			Int("asgInstanceCount", len(asgInstanceIDs)).
			Msg("No Kubernetes nodes found for spot ASG yet (instances may not have joined cluster)")
		return false, nil
	}

	// Check if all spot nodes are Ready and not cordoned
	nodeStatuses := make([]string, 0)
	for _, node := range spotNodes {
		// Check if node is cordoned
		if node.Spec.Unschedulable {
			log.Debug().
				Str("node", node.Name).
				Str("asg", asgName).
				Msg("Spot node is cordoned (unschedulable)")
			return false, nil
		}

		// Check if node is Ready
		isReady := false
		var readyCondition *corev1.NodeCondition
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				isReady = condition.Status == corev1.ConditionTrue
				readyCondition = &condition
				break
			}
		}

		if !isReady {
			reason := "Unknown"
			message := ""
			if readyCondition != nil {
				reason = readyCondition.Reason
				message = readyCondition.Message
			}
			log.Debug().
				Str("node", node.Name).
				Str("asg", asgName).
				Str("reason", reason).
				Str("message", message).
				Msg("Spot node is not ready")
			return false, nil
		}

		nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:Ready", node.Name))
	}

	log.Debug().
		Str("asg", asgName).
		Int("nodeCount", len(spotNodes)).
		Strs("nodeStatuses", nodeStatuses).
		Msg("All spot nodes are ready and schedulable")

	return true, nil
}

// IsSpotCapacityStable checks if spot capacity has been stable for the specified duration
func (hc *HealthChecker) IsSpotCapacityStable(ctx context.Context, asgName string, stabilityDuration time.Duration, healthySince *time.Time) (bool, *time.Time, error) {
	log.Debug().
		Str("asg", asgName).
		Dur("requiredStability", stabilityDuration).
		Msg("Checking spot capacity stability")

	// Check if spot is currently healthy
	isHealthy, err := hc.IsSpotASGHealthy(ctx, asgName)
	if err != nil {
		log.Error().
			Err(err).
			Str("asg", asgName).
			Msg("Failed to check spot ASG health during stability check")
		return false, nil, err
	}

	if !isHealthy {
		// Reset stability timer if unhealthy
		if healthySince != nil {
			elapsed := time.Since(*healthySince)
			log.Debug().
				Str("asg", asgName).
				Dur("wasHealthyFor", elapsed).
				Msg("Spot ASG became unhealthy, resetting stability timer")
		} else {
			log.Debug().
				Str("asg", asgName).
				Msg("Spot ASG is not healthy, stability timer not started")
		}
		return false, nil, nil
	}

	// Check Kubernetes node readiness
	nodesReady, err := hc.AreSpotNodesReady(ctx, asgName)
	if err != nil {
		log.Error().
			Err(err).
			Str("asg", asgName).
			Msg("Failed to check Kubernetes node readiness during stability check")
		return false, healthySince, err
	}

	if !nodesReady {
		if healthySince != nil {
			elapsed := time.Since(*healthySince)
			log.Debug().
				Str("asg", asgName).
				Dur("wasHealthyFor", elapsed).
				Msg("Spot nodes not ready, resetting stability timer")
		} else {
			log.Debug().
				Str("asg", asgName).
				Msg("Spot nodes not ready, stability timer not started")
		}
		return false, nil, nil
	}

	// If this is the first time healthy, start the timer
	now := time.Now()
	if healthySince == nil {
		log.Info().
			Str("asg", asgName).
			Time("since", now).
			Dur("requiredStability", stabilityDuration).
			Msg("Spot capacity became healthy (ASG + K8s), starting stability timer")
		return false, &now, nil
	}

	// Check if stable for long enough
	elapsed := now.Sub(*healthySince)
	isStable := elapsed >= stabilityDuration
	remaining := stabilityDuration - elapsed

	if isStable {
		log.Info().
			Str("asg", asgName).
			Dur("elapsed", elapsed).
			Dur("required", stabilityDuration).
			Msg("Spot capacity is stable for required duration")
	} else {
		log.Debug().
			Str("asg", asgName).
			Dur("elapsed", elapsed).
			Dur("required", stabilityDuration).
			Dur("remaining", remaining).
			Msg("Spot capacity is healthy but not yet stable for required duration")
	}

	return isStable, healthySince, nil
}

// IsSpotCapacityRestored performs comprehensive check if spot capacity is restored
func (hc *HealthChecker) IsSpotCapacityRestored(ctx context.Context, asgName string, stabilityDuration time.Duration, healthySince *time.Time) (bool, *time.Time, error) {
	// Run stability check which includes ASG and K8s node checks
	isStable, newHealthySince, err := hc.IsSpotCapacityStable(ctx, asgName, stabilityDuration, healthySince)
	if err != nil {
		return false, healthySince, err
	}

	if isStable {
		log.Info().
			Str("asg", asgName).
			Dur("stabilityDuration", stabilityDuration).
			Msg("Spot capacity is fully restored and stable")
	}

	return isStable, newHealthySince, nil
}

// extractInstanceIDFromProviderID extracts the EC2 instance ID from a Kubernetes providerID
// Format: aws:///us-west-2a/i-0123456789abcdef0 or aws:///<zone>/i-<instanceid>
func extractInstanceIDFromProviderID(providerID string) string {
	if providerID == "" {
		return ""
	}

	// Split by "/" and get the last part (instance ID)
	parts := strings.Split(providerID, "/")
	if len(parts) > 0 {
		instanceID := parts[len(parts)-1]
		// Verify it looks like an instance ID (starts with "i-")
		if strings.HasPrefix(instanceID, "i-") {
			return instanceID
		}
	}

	return ""
}

// CheckSpotASGComprehensive performs all spot ASG checks in ONE API call
// This combines IsSpotASGHealthy, AreSpotNodesReady, and IsSpotCapacityStable
// to avoid duplicate AWS API calls and reduce throttling risk
func (hc *HealthChecker) CheckSpotASGComprehensive(
	ctx context.Context,
	asgName string,
	stabilityDuration time.Duration,
	previousHealthySince *time.Time,
) (*SpotASGHealthStatus, error) {

	log.Debug().
		Str("asg", asgName).
		Dur("requiredStability", stabilityDuration).
		Msg("Performing comprehensive spot ASG health check")

	// ═══════════════════════════════════════════════════════════
	// ONE AWS API CALL - Get all ASG data
	// ═══════════════════════════════════════════════════════════
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}
	result, err := hc.asgClient.DescribeAutoScalingGroupsWithContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe ASG %s: %w", asgName, err)
	}
	if len(result.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("ASG %s not found", asgName)
	}

	asg := result.AutoScalingGroups[0]
	status := &SpotASGHealthStatus{}

	// ═══════════════════════════════════════════════════════════
	// CHECK 1: ASG Health (inService >= desired?)
	// ═══════════════════════════════════════════════════════════
	desiredCapacity := aws.Int64Value(asg.DesiredCapacity)
	inServiceCount := int64(0)
	instanceIDs := make([]string, 0)

	instanceDetails := make([]string, 0)
	for _, instance := range asg.Instances {
		instanceID := aws.StringValue(instance.InstanceId)
		instanceIDs = append(instanceIDs, instanceID)

		lifecycleState := aws.StringValue(instance.LifecycleState)
		healthStatus := aws.StringValue(instance.HealthStatus)

		if lifecycleState == "InService" && healthStatus == "Healthy" {
			inServiceCount++
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:InService:Healthy", instanceID))
		} else {
			instanceDetails = append(instanceDetails, fmt.Sprintf("%s:%s:%s", instanceID, lifecycleState, healthStatus))
		}
	}

	status.IsHealthy = (inServiceCount >= desiredCapacity)
	status.InstanceIDs = instanceIDs

	log.Debug().
		Str("asg", asgName).
		Int64("inService", inServiceCount).
		Int64("desired", desiredCapacity).
		Int("totalInstances", len(instanceIDs)).
		Strs("instances", instanceDetails).
		Bool("healthy", status.IsHealthy).
		Msg("Comprehensive check: ASG health")

	// ═══════════════════════════════════════════════════════════
	// CHECK 2: Kubernetes Nodes Ready (match instances to nodes)
	// ═══════════════════════════════════════════════════════════
	nodes, err := hc.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Build map of ASG instance IDs for quick lookup
	asgInstanceMap := make(map[string]bool)
	for _, instanceID := range instanceIDs {
		asgInstanceMap[instanceID] = true
	}

	// Find and check nodes belonging to this ASG
	spotNodes := make([]corev1.Node, 0)
	for _, node := range nodes.Items {
		instanceID := extractInstanceIDFromProviderID(node.Spec.ProviderID)
		if instanceID != "" && asgInstanceMap[instanceID] {
			spotNodes = append(spotNodes, node)
			log.Debug().
				Str("node", node.Name).
				Str("instanceID", instanceID).
				Str("asg", asgName).
				Msg("Found spot node via instance ID match")
		}
	}

	if len(spotNodes) == 0 {
		status.NodesReady = false
		log.Debug().
			Str("asg", asgName).
			Int("asgInstances", len(instanceIDs)).
			Int("k8sNodesFound", 0).
			Msg("Comprehensive check: No K8s nodes found for ASG instances yet")
		return status, nil
	}

	// Check if all spot nodes are Ready
	allReady := true
	nodeStatuses := make([]string, 0)
	for _, node := range spotNodes {
		if node.Spec.Unschedulable {
			allReady = false
			nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:Cordoned", node.Name))
			log.Debug().
				Str("node", node.Name).
				Str("asg", asgName).
				Msg("Spot node is cordoned (unschedulable)")
			break
		}

		isReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				isReady = (condition.Status == corev1.ConditionTrue)
				break
			}
		}

		if !isReady {
			allReady = false
			nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:NotReady", node.Name))
			log.Debug().
				Str("node", node.Name).
				Str("asg", asgName).
				Msg("Spot node is not ready")
			break
		} else {
			nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:Ready", node.Name))
		}
	}

	status.NodesReady = allReady

	log.Debug().
		Str("asg", asgName).
		Int("spotNodes", len(spotNodes)).
		Strs("nodeStatuses", nodeStatuses).
		Bool("allReady", allReady).
		Msg("Comprehensive check: K8s node readiness")

	// ═══════════════════════════════════════════════════════════
	// CHECK 3: Stability (healthy for required duration?)
	// ═══════════════════════════════════════════════════════════
	now := time.Now()

	// If currently unhealthy or nodes not ready, reset stability tracking
	if !status.IsHealthy || !status.NodesReady {
		status.IsStable = false
		status.HealthySince = nil
		log.Debug().
			Str("asg", asgName).
			Bool("healthy", status.IsHealthy).
			Bool("nodesReady", status.NodesReady).
			Msg("Comprehensive check: Spot capacity not healthy, resetting stability tracking")
		return status, nil
	}

	// If previously unhealthy, start tracking from now
	if previousHealthySince == nil {
		status.HealthySince = &now
		status.IsStable = false
		log.Debug().
			Str("asg", asgName).
			Time("healthySince", now).
			Msg("Comprehensive check: Spot capacity became healthy, starting stability tracking")
		return status, nil
	}

	// Check if stable for required duration
	elapsed := now.Sub(*previousHealthySince)
	status.HealthySince = previousHealthySince
	status.IsStable = (elapsed >= stabilityDuration)

	if status.IsStable {
		log.Info().
			Str("asg", asgName).
			Dur("elapsed", elapsed).
			Dur("required", stabilityDuration).
			Msg("Comprehensive check: Spot capacity is stable for required duration")
	} else {
		remaining := stabilityDuration - elapsed
		log.Debug().
			Str("asg", asgName).
			Dur("elapsed", elapsed).
			Dur("required", stabilityDuration).
			Dur("remaining", remaining).
			Msg("Comprehensive check: Spot capacity is healthy but not yet stable")
	}

	return status, nil
}

// PreScaleCalculation contains pre-scale calculation results
type PreScaleCalculation struct {
	CurrentNodes         int
	CurrentSpotNodes     int
	CurrentOnDemandNodes int
	CurrentUtilization   float64
	TargetUtilization    float64
	NodesNeeded          int
	AdditionalSpotNodes  int
	ExpectedUtilization  float64
	SafetyBuffer         float64
}

// CalculatePreScaleNodes determines how many spot nodes to add
// to achieve target utilization after draining on-demand
func (hc *HealthChecker) CalculatePreScaleNodes(
	ctx context.Context,
	spotASGName string,
	onDemandASGName string,
	currentUtilization float64,
	targetUtilization float64,
	safetyBufferPercent int,
) (*PreScaleCalculation, error) {

	calc := &PreScaleCalculation{
		CurrentUtilization: currentUtilization,
		TargetUtilization:  targetUtilization,
		SafetyBuffer:       float64(safetyBufferPercent) / 100.0,
	}

	log.Debug().
		Str("spotASG", spotASGName).
		Str("onDemandASG", onDemandASGName).
		Float64("currentUtilization", currentUtilization).
		Float64("targetUtilization", targetUtilization).
		Float64("safetyBuffer", calc.SafetyBuffer*100).
		Msg("Starting pre-scale calculation")

	// Get current spot ASG size
	spotInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(spotASGName)},
	}
	spotResult, err := hc.asgClient.DescribeAutoScalingGroupsWithContext(ctx, spotInput)
	if err != nil || len(spotResult.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("failed to get spot ASG data: %w", err)
	}
	calc.CurrentSpotNodes = int(aws.Int64Value(spotResult.AutoScalingGroups[0].DesiredCapacity))

	// Get current on-demand ASG size
	onDemandInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(onDemandASGName)},
	}
	onDemandResult, err := hc.asgClient.DescribeAutoScalingGroupsWithContext(ctx, onDemandInput)
	if err != nil || len(onDemandResult.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("failed to get on-demand ASG data: %w", err)
	}
	calc.CurrentOnDemandNodes = int(aws.Int64Value(onDemandResult.AutoScalingGroups[0].DesiredCapacity))

	calc.CurrentNodes = calc.CurrentSpotNodes + calc.CurrentOnDemandNodes

	log.Debug().
		Int("currentNodes", calc.CurrentNodes).
		Int("spotNodes", calc.CurrentSpotNodes).
		Int("onDemandNodes", calc.CurrentOnDemandNodes).
		Msg("Retrieved current cluster node counts")

	// Check if already below target
	if currentUtilization <= targetUtilization {
		calc.NodesNeeded = calc.CurrentSpotNodes
		calc.AdditionalSpotNodes = 0
		calc.ExpectedUtilization = currentUtilization

		log.Info().
			Float64("currentUtilization", currentUtilization).
			Float64("targetUtilization", targetUtilization).
			Msg("No pre-scale needed - utilization already below target")

		return calc, nil
	}

	// Calculate total workload (in abstract capacity units)
	// Formula: Total workload = Current nodes × Current utilization%
	totalWorkload := float64(calc.CurrentNodes) * (currentUtilization / 100.0)

	log.Debug().
		Float64("totalWorkload", totalWorkload).
		Int("currentNodes", calc.CurrentNodes).
		Float64("currentUtilization", currentUtilization).
		Msg("Calculated total cluster workload")

	// After draining on-demand, all workload goes to spot nodes
	// Required spot nodes = Total workload / Target utilization%
	// Formula: nodes = workload / (target% / 100)
	requiredCapacity := totalWorkload / (targetUtilization / 100.0)

	// Round up to ensure we have enough capacity
	calc.NodesNeeded = int(requiredCapacity)
	if requiredCapacity > float64(calc.NodesNeeded) {
		calc.NodesNeeded++
	}

	// How many additional spot nodes needed
	calc.AdditionalSpotNodes = calc.NodesNeeded - calc.CurrentSpotNodes

	log.Debug().
		Float64("requiredCapacity", requiredCapacity).
		Int("nodesNeeded", calc.NodesNeeded).
		Int("currentSpotNodes", calc.CurrentSpotNodes).
		Int("additionalSpotNodes", calc.AdditionalSpotNodes).
		Msg("Base pre-scale calculation complete")

	// Apply safety buffer (e.g., +10% more nodes)
	if calc.SafetyBuffer > 0 && calc.AdditionalSpotNodes > 0 {
		bufferedNodes := float64(calc.AdditionalSpotNodes) * (1.0 + calc.SafetyBuffer)
		originalAdditional := calc.AdditionalSpotNodes
		calc.AdditionalSpotNodes = int(bufferedNodes)
		if bufferedNodes > float64(calc.AdditionalSpotNodes) {
			calc.AdditionalSpotNodes++
		}
		calc.NodesNeeded = calc.CurrentSpotNodes + calc.AdditionalSpotNodes

		log.Debug().
			Int("originalAdditional", originalAdditional).
			Int("bufferedAdditional", calc.AdditionalSpotNodes).
			Float64("safetyBufferPercent", calc.SafetyBuffer*100).
			Msg("Applied safety buffer to pre-scale calculation")
	}

	// Ensure we add at least 1 node if utilization is high
	if calc.AdditionalSpotNodes < 1 && currentUtilization > targetUtilization {
		log.Debug().
			Int("calculated", calc.AdditionalSpotNodes).
			Msg("Calculated less than 1 node, setting minimum to 1")
		calc.AdditionalSpotNodes = 1
		calc.NodesNeeded = calc.CurrentSpotNodes + 1
	}

	// Calculate expected utilization after pre-scale
	if calc.NodesNeeded > 0 {
		calc.ExpectedUtilization = (totalWorkload / float64(calc.NodesNeeded)) * 100.0
	}

	log.Info().
		Int("currentNodes", calc.CurrentNodes).
		Int("currentSpotNodes", calc.CurrentSpotNodes).
		Int("requiredSpotNodes", calc.NodesNeeded).
		Int("additionalSpotNodes", calc.AdditionalSpotNodes).
		Float64("currentUtilization", currentUtilization).
		Float64("expectedUtilization", calc.ExpectedUtilization).
		Float64("targetUtilization", targetUtilization).
		Float64("safetyBufferPercent", calc.SafetyBuffer*100).
		Msg("Pre-scale calculation complete")

	return calc, nil
}

// ScaleSpotASG scales the spot ASG to the desired capacity
func (hc *HealthChecker) ScaleSpotASG(ctx context.Context, asgName string, desiredCapacity int) error {
	log.Info().
		Str("asg", asgName).
		Int("desiredCapacity", desiredCapacity).
		Msg("Scaling spot ASG")

	input := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(int64(desiredCapacity)),
		HonorCooldown:        aws.Bool(false), // Skip cooldown for immediate scaling
	}

	_, err := hc.asgClient.SetDesiredCapacity(input)
	if err != nil {
		return fmt.Errorf("failed to scale ASG: %w", err)
	}

	log.Info().
		Str("asg", asgName).
		Int("desiredCapacity", desiredCapacity).
		Msg("Successfully scaled spot ASG")

	return nil
}
