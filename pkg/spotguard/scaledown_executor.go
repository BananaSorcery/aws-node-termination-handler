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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ScaleDownExecutor handles the execution of scaling down on-demand nodes
type ScaleDownExecutor struct {
	asgClient          autoscalingiface.AutoScalingAPI
	k8sClient          kubernetes.Interface
	nodeHandler        node.Node
	podEvictionTimeout time.Duration
}

// NewScaleDownExecutor creates a new scale-down executor
func NewScaleDownExecutor(
	asgClient autoscalingiface.AutoScalingAPI,
	k8sClient kubernetes.Interface,
	nodeHandler node.Node,
	podEvictionTimeout time.Duration,
) *ScaleDownExecutor {
	return &ScaleDownExecutor{
		asgClient:          asgClient,
		k8sClient:          k8sClient,
		nodeHandler:        nodeHandler,
		podEvictionTimeout: podEvictionTimeout,
	}
}

// ScaleDownOnDemandNode performs the complete scale-down operation
func (se *ScaleDownExecutor) ScaleDownOnDemandNode(ctx context.Context, event *FallbackEvent) error {
	nodeName := event.OnDemandNodeName

	log.Info().
		Str("eventID", event.EventID).
		Str("node", nodeName).
		Str("instanceID", event.OnDemandInstanceID).
		Str("onDemandASG", event.OnDemandASGName).
		Str("spotASG", event.SpotASGName).
		Dur("onDemandRuntime", time.Since(event.Timestamp)).
		Msg("Starting on-demand node scale-down operation")

	// Step 1: Taint the node
	log.Info().Str("eventID", event.EventID).Str("node", nodeName).Msg("Step 1/5: Tainting node")
	if err := se.taintNode(ctx, nodeName); err != nil {
		log.Error().
			Err(err).
			Str("eventID", event.EventID).
			Str("node", nodeName).
			Msg("Failed to taint node")
		return fmt.Errorf("failed to taint node: %w", err)
	}

	// Step 2: Cordon the node
	log.Info().Str("eventID", event.EventID).Str("node", nodeName).Msg("Step 2/5: Cordoning node")
	if err := se.cordonNode(nodeName); err != nil {
		log.Error().
			Err(err).
			Str("eventID", event.EventID).
			Str("node", nodeName).
			Msg("Failed to cordon node")
		return fmt.Errorf("failed to cordon node: %w", err)
	}

	// Step 3: Drain the node
	log.Info().Str("eventID", event.EventID).Str("node", nodeName).Msg("Step 3/5: Draining node (evicting pods)")
	if err := se.drainNode(ctx, nodeName); err != nil {
		log.Error().
			Err(err).
			Str("eventID", event.EventID).
			Str("node", nodeName).
			Msg("Failed to drain node")
		return fmt.Errorf("failed to drain node: %w", err)
	}

	// Step 4: Wait for pods to be rescheduled
	log.Info().Str("eventID", event.EventID).Str("node", nodeName).Msg("Step 4/5: Waiting for pods to be rescheduled")
	if err := se.waitForPodsRescheduled(ctx, nodeName); err != nil {
		log.Warn().
			Err(err).
			Str("eventID", event.EventID).
			Str("node", nodeName).
			Msg("Warning: could not verify all pods rescheduled, continuing anyway")
		// Continue anyway as pods might have been rescheduled
	}

	// Step 5: Scale down the on-demand ASG
	log.Info().
		Str("eventID", event.EventID).
		Str("asg", event.OnDemandASGName).
		Msg("Step 5/5: Scaling down on-demand ASG")
	if err := se.decreaseASGCapacity(ctx, event.OnDemandASGName); err != nil {
		log.Error().
			Err(err).
			Str("eventID", event.EventID).
			Str("asg", event.OnDemandASGName).
			Msg("Failed to scale down on-demand ASG")
		return fmt.Errorf("failed to scale down ASG: %w", err)
	}

	log.Info().
		Str("eventID", event.EventID).
		Str("node", nodeName).
		Str("instanceID", event.OnDemandInstanceID).
		Str("onDemandASG", event.OnDemandASGName).
		Dur("totalRuntime", time.Since(event.Timestamp)).
		Msg("Successfully completed on-demand node scale-down operation")

	return nil
}

// taintNode applies a taint to the node
func (se *ScaleDownExecutor) taintNode(ctx context.Context, nodeName string) error {
	log.Debug().Str("node", nodeName).Msg("Getting node to apply taint")

	node, err := se.k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("Failed to get node")
		return err
	}

	// Add taint
	taint := corev1.Taint{
		Key:    "spotguard/scale-down-pending",
		Value:  "true",
		Effect: corev1.TaintEffectNoSchedule,
	}

	// Check if taint already exists
	taintExists := false
	for _, existingTaint := range node.Spec.Taints {
		if existingTaint.Key == taint.Key {
			taintExists = true
			break
		}
	}

	if !taintExists {
		log.Debug().
			Str("node", nodeName).
			Str("taintKey", taint.Key).
			Str("taintValue", taint.Value).
			Str("effect", string(taint.Effect)).
			Msg("Applying taint to node")

		node.Spec.Taints = append(node.Spec.Taints, taint)
		_, err = se.k8sClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			log.Error().Err(err).Str("node", nodeName).Msg("Failed to update node with taint")
			return err
		}
		log.Info().Str("node", nodeName).Msg("Successfully applied scale-down taint to node")
	} else {
		log.Debug().Str("node", nodeName).Msg("Taint already exists on node")
	}

	return nil
}

// cordonNode marks the node as unschedulable
func (se *ScaleDownExecutor) cordonNode(nodeName string) error {
	log.Debug().Str("node", nodeName).Msg("Cordoning node (marking unschedulable)")

	err := se.nodeHandler.Cordon(nodeName, "Scaling down on-demand due to spot capacity restored")
	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("Failed to cordon node")
		return err
	}

	log.Info().Str("node", nodeName).Msg("Successfully cordoned on-demand node")
	return nil
}

// drainNode gracefully evicts pods from the node
func (se *ScaleDownExecutor) drainNode(ctx context.Context, nodeName string) error {
	log.Debug().
		Str("node", nodeName).
		Dur("timeout", se.podEvictionTimeout).
		Msg("Starting node drain (graceful pod eviction)")

	// Use the existing NTH drain functionality
	err := se.nodeHandler.CordonAndDrain(nodeName, "Scaling down on-demand due to spot capacity restored", nil)
	if err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("Failed to drain node")
		return err
	}

	log.Info().Str("node", nodeName).Msg("Successfully drained on-demand node")
	return nil
}

// waitForPodsRescheduled waits for pods to be rescheduled on other nodes
func (se *ScaleDownExecutor) waitForPodsRescheduled(ctx context.Context, nodeName string) error {
	timeout := time.After(se.podEvictionTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for pods to be rescheduled")
		case <-ticker.C:
			pods, err := se.k8sClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
			})
			if err != nil {
				return err
			}

			// Count non-DaemonSet pods
			nonDaemonSetPods := 0
			for _, pod := range pods.Items {
				if !isDaemonSetPod(&pod) {
					nonDaemonSetPods++
				}
			}

			if nonDaemonSetPods == 0 {
				log.Info().Str("node", nodeName).Msg("All pods have been evicted from node")
				return nil
			}

			log.Debug().
				Str("node", nodeName).
				Int("remainingPods", nonDaemonSetPods).
				Msg("Waiting for pods to be evicted")
		}
	}
}

// decreaseASGCapacity decreases the desired capacity of the ASG by 1
func (se *ScaleDownExecutor) decreaseASGCapacity(ctx context.Context, asgName string) error {
	log.Debug().Str("asg", asgName).Msg("Getting current ASG capacity")

	// Get current ASG configuration
	input := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}

	result, err := se.asgClient.DescribeAutoScalingGroupsWithContext(ctx, input)
	if err != nil {
		log.Error().Err(err).Str("asg", asgName).Msg("Failed to describe ASG")
		return fmt.Errorf("failed to describe ASG: %w", err)
	}

	if len(result.AutoScalingGroups) == 0 {
		log.Error().Str("asg", asgName).Msg("ASG not found")
		return fmt.Errorf("ASG %s not found", asgName)
	}

	asg := result.AutoScalingGroups[0]
	currentDesired := aws.Int64Value(asg.DesiredCapacity)
	minSize := aws.Int64Value(asg.MinSize)
	maxSize := aws.Int64Value(asg.MaxSize)
	currentInstances := int64(len(asg.Instances))

	log.Debug().
		Str("asg", asgName).
		Int64("currentDesired", currentDesired).
		Int64("currentInstances", currentInstances).
		Int64("min", minSize).
		Int64("max", maxSize).
		Msg("Current ASG capacity")

	newDesired := currentDesired - 1
	if newDesired < minSize {
		log.Error().
			Str("asg", asgName).
			Int64("desiredNew", newDesired).
			Int64("minSize", minSize).
			Msg("Cannot decrease capacity below minimum size")
		return fmt.Errorf("cannot decrease capacity below minimum size (%d)", minSize)
	}

	log.Info().
		Str("asg", asgName).
		Int64("oldDesired", currentDesired).
		Int64("newDesired", newDesired).
		Msg("Updating ASG desired capacity")

	// Update ASG desired capacity
	updateInput := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(newDesired),
		HonorCooldown:        aws.Bool(false),
	}

	_, err = se.asgClient.SetDesiredCapacityWithContext(ctx, updateInput)
	if err != nil {
		log.Error().
			Err(err).
			Str("asg", asgName).
			Int64("desiredCapacity", newDesired).
			Msg("Failed to update ASG capacity")
		return fmt.Errorf("failed to update ASG capacity: %w", err)
	}

	// Verify the capacity was actually updated (protection against race conditions)
	// Wait a short time for AWS to process the update
	time.Sleep(2 * time.Second)

	verifyResult, err := se.asgClient.DescribeAutoScalingGroupsWithContext(ctx, input)
	if err != nil {
		log.Warn().Err(err).Str("asg", asgName).Msg("Failed to verify ASG capacity update, assuming success")
	} else if len(verifyResult.AutoScalingGroups) > 0 {
		verifiedDesired := aws.Int64Value(verifyResult.AutoScalingGroups[0].DesiredCapacity)
		if verifiedDesired != newDesired {
			log.Warn().
				Str("asg", asgName).
				Int64("expectedCapacity", newDesired).
				Int64("actualCapacity", verifiedDesired).
				Msg("ASG capacity verification mismatch - possible concurrent modification")
		}
	}

	log.Info().
		Str("asg", asgName).
		Int64("oldCapacity", currentDesired).
		Int64("newCapacity", newDesired).
		Msg("Successfully scaled down on-demand ASG")

	return nil
}
