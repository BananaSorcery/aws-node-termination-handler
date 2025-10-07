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

	// Find nodes belonging to the spot ASG
	spotNodes := make([]corev1.Node, 0)
	for _, node := range nodes.Items {
		// Check if node has ASG label/tag
		if asgLabel, ok := node.Labels["alpha.eksctl.io/nodegroup-name"]; ok {
			if asgLabel == asgName {
				log.Debug().
					Str("node", node.Name).
					Str("asg", asgName).
					Str("labelKey", "alpha.eksctl.io/nodegroup-name").
					Msg("Found spot node via eksctl label")
				spotNodes = append(spotNodes, node)
			}
		} else if asgTag, ok := node.Labels["eks.amazonaws.com/nodegroup"]; ok {
			if asgTag == asgName {
				log.Debug().
					Str("node", node.Name).
					Str("asg", asgName).
					Str("labelKey", "eks.amazonaws.com/nodegroup").
					Msg("Found spot node via EKS label")
				spotNodes = append(spotNodes, node)
			}
		}
	}

	if len(spotNodes) == 0 {
		log.Debug().
			Str("asg", asgName).
			Int("totalNodesChecked", len(nodes.Items)).
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
			nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:Cordoned", node.Name))
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
			nodeStatuses = append(nodeStatuses, fmt.Sprintf("%s:NotReady:%s", node.Name, reason))
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
