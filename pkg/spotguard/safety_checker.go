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

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// SafetyChecker performs safety checks before scaling down on-demand nodes
type SafetyChecker struct {
	k8sClient                    kubernetes.Interface
	maxClusterUtilizationPercent float64
}

// NewSafetyChecker creates a new safety checker
func NewSafetyChecker(k8sClient kubernetes.Interface, maxUtilization float64) *SafetyChecker {
	return &SafetyChecker{
		k8sClient:                    k8sClient,
		maxClusterUtilizationPercent: maxUtilization,
	}
}

// CanScaleDownOnDemand checks if minimum wait time has passed
func (sc *SafetyChecker) CanScaleDownOnDemand(event *FallbackEvent) (bool, string) {
	elapsed := time.Since(event.Timestamp)
	if elapsed < event.MinimumWaitDuration {
		remaining := event.MinimumWaitDuration - elapsed
		return false, fmt.Sprintf("minimum wait time not met: %v remaining", remaining.Round(time.Second))
	}
	return true, ""
}

// CanSafelyDrainNode checks if the on-demand node can be safely drained
func (sc *SafetyChecker) CanSafelyDrainNode(ctx context.Context, nodeName string) (bool, string) {
	log.Debug().Str("node", nodeName).Msg("Starting pod safety check for node drain")

	// Get the node
	node, err := sc.k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		log.Error().
			Err(err).
			Str("node", nodeName).
			Msg("Failed to get node for safety check")
		return false, fmt.Sprintf("failed to get node: %v", err)
	}

	log.Debug().
		Str("node", nodeName).
		Str("instanceType", node.Labels["node.kubernetes.io/instance-type"]).
		Str("zone", node.Labels["topology.kubernetes.io/zone"]).
		Msg("Retrieved node details")

	// Get all pods on the node
	pods, err := sc.k8sClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("node", nodeName).
			Msg("Failed to list pods on node")
		return false, fmt.Sprintf("failed to list pods: %v", err)
	}

	log.Debug().
		Str("node", nodeName).
		Int("totalPods", len(pods.Items)).
		Msg("Checking pod safety for node drain")

	// Check each pod
	daemonSetCount := 0
	terminatingCount := 0
	checkablePodsCount := 0

	for _, pod := range pods.Items {
		podInfo := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

		// Skip DaemonSet pods (they're OK, they run on every node)
		if isDaemonSetPod(&pod) {
			daemonSetCount++
			log.Debug().
				Str("node", nodeName).
				Str("pod", podInfo).
				Msg("Skipping DaemonSet pod (will be recreated on other nodes)")
			continue
		}

		// Skip pods that are already terminating
		if pod.DeletionTimestamp != nil {
			terminatingCount++
			log.Debug().
				Str("node", nodeName).
				Str("pod", podInfo).
				Msg("Skipping pod that is already terminating")
			continue
		}

		checkablePodsCount++
		log.Debug().
			Str("node", nodeName).
			Str("pod", podInfo).
			Str("phase", string(pod.Status.Phase)).
			Msg("Checking if pod can be safely evicted")

		// Check if pod can be scheduled elsewhere
		canSchedule, reason := sc.canPodScheduleElsewhere(ctx, &pod, nodeName)
		if !canSchedule {
			log.Warn().
				Str("node", nodeName).
				Str("pod", podInfo).
				Str("reason", reason).
				Msg("Pod cannot be rescheduled elsewhere")
			return false, fmt.Sprintf("pod %s/%s cannot be rescheduled: %s", pod.Namespace, pod.Name, reason)
		}

		// Check PodDisruptionBudget
		if violates, reason := sc.wouldViolatePDB(ctx, &pod); violates {
			log.Warn().
				Str("node", nodeName).
				Str("pod", podInfo).
				Str("reason", reason).
				Msg("Evicting pod would violate PodDisruptionBudget")
			return false, fmt.Sprintf("pod %s/%s would violate PDB: %s", pod.Namespace, pod.Name, reason)
		}

		log.Debug().
			Str("node", nodeName).
			Str("pod", podInfo).
			Msg("Pod can be safely evicted")
	}

	log.Info().
		Str("node", nodeName).
		Int("totalPods", len(pods.Items)).
		Int("daemonSets", daemonSetCount).
		Int("alreadyTerminating", terminatingCount).
		Int("checked", checkablePodsCount).
		Msg("Pod safety check passed - all pods can be safely rescheduled")

	// Check cluster capacity buffer
	hasBuffer, reason := sc.hasClusterCapacityBuffer(ctx, node)
	if !hasBuffer {
		return false, reason
	}

	return true, ""
}

// isDaemonSetPod checks if a pod is managed by a DaemonSet
func isDaemonSetPod(pod *corev1.Pod) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// canPodScheduleElsewhere checks if a pod can be scheduled on other nodes
func (sc *SafetyChecker) canPodScheduleElsewhere(ctx context.Context, pod *corev1.Pod, excludeNode string) (bool, string) {
	// Get all nodes
	nodes, err := sc.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Sprintf("failed to list nodes: %v", err)
	}

	// Check if any node can fit the pod
	for _, node := range nodes.Items {
		// Skip the node we're draining
		if node.Name == excludeNode {
			continue
		}

		// Skip cordoned nodes
		if node.Spec.Unschedulable {
			continue
		}

		// Skip nodes that are not ready
		if !isNodeReady(&node) {
			continue
		}

		// Check if pod fits on this node
		if canFit, _ := sc.podFitsOnNode(pod, &node); canFit {
			return true, ""
		}
	}

	return false, "no suitable node found with sufficient resources"
}

// podFitsOnNode checks if a pod can fit on a specific node
func (sc *SafetyChecker) podFitsOnNode(pod *corev1.Pod, node *corev1.Node) (bool, string) {
	// Get pod resource requests
	podCPU := int64(0)
	podMemory := int64(0)

	for _, container := range pod.Spec.Containers {
		if cpu := container.Resources.Requests.Cpu(); cpu != nil {
			podCPU += cpu.MilliValue()
		}
		if memory := container.Resources.Requests.Memory(); memory != nil {
			podMemory += memory.Value()
		}
	}

	// Get node allocatable resources
	nodeCPU := node.Status.Allocatable.Cpu().MilliValue()
	nodeMemory := node.Status.Allocatable.Memory().Value()

	// Simple check (in production, you'd want to account for already scheduled pods)
	if podCPU > nodeCPU || podMemory > nodeMemory {
		return false, "insufficient resources"
	}

	return true, ""
}

// wouldViolatePDB checks if evicting the pod would violate a PodDisruptionBudget
func (sc *SafetyChecker) wouldViolatePDB(ctx context.Context, pod *corev1.Pod) (bool, string) {
	// Get all PDBs in the pod's namespace
	pdbs, err := sc.k8sClient.PolicyV1().PodDisruptionBudgets(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list PDBs, assuming no PDB")
		return false, ""
	}

	// Find PDB that matches this pod
	for _, pdb := range pdbs.Items {
		selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			continue
		}

		if selector.Matches(labels.Set(pod.Labels)) {
			// Found matching PDB
			if violates := sc.checkPDBViolation(&pdb, pod); violates {
				return true, fmt.Sprintf("PDB %s would be violated", pdb.Name)
			}
		}
	}

	return false, ""
}

// checkPDBViolation checks if evicting would violate the PDB
func (sc *SafetyChecker) checkPDBViolation(pdb *policyv1.PodDisruptionBudget, pod *corev1.Pod) bool {
	// If DisruptionsAllowed is 0, we cannot evict
	if pdb.Status.DisruptionsAllowed <= 0 {
		log.Debug().
			Str("pdb", pdb.Name).
			Int32("disruptionsAllowed", pdb.Status.DisruptionsAllowed).
			Msg("PDB disallows disruptions")
		return true
	}
	return false
}

// hasClusterCapacityBuffer checks if cluster has sufficient capacity buffer
func (sc *SafetyChecker) hasClusterCapacityBuffer(ctx context.Context, nodeToRemove *corev1.Node) (bool, string) {
	// Get all nodes
	nodes, err := sc.k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Sprintf("failed to list nodes: %v", err)
	}

	// Calculate total and used capacity
	totalCPU := int64(0)
	totalMemory := int64(0)
	usedCPU := int64(0)
	usedMemory := int64(0)

	for _, node := range nodes.Items {
		// Skip nodes that are not ready
		if !isNodeReady(&node) {
			continue
		}

		totalCPU += node.Status.Allocatable.Cpu().MilliValue()
		totalMemory += node.Status.Allocatable.Memory().Value()
	}

	// Get all pods to calculate usage
	pods, err := sc.k8sClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Sprintf("failed to list pods: %v", err)
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			for _, container := range pod.Spec.Containers {
				if cpu := container.Resources.Requests.Cpu(); cpu != nil {
					usedCPU += cpu.MilliValue()
				}
				if memory := container.Resources.Requests.Memory(); memory != nil {
					usedMemory += memory.Value()
				}
			}
		}
	}

	// Calculate capacity after removing the on-demand node
	nodeToRemoveCPU := nodeToRemove.Status.Allocatable.Cpu().MilliValue()
	nodeToRemoveMemory := nodeToRemove.Status.Allocatable.Memory().Value()

	remainingCPU := totalCPU - nodeToRemoveCPU
	remainingMemory := totalMemory - nodeToRemoveMemory

	// Calculate utilization after scale-down
	cpuUtilization := float64(usedCPU) / float64(remainingCPU) * 100
	memoryUtilization := float64(usedMemory) / float64(remainingMemory) * 100

	maxUtilization := cpuUtilization
	if memoryUtilization > maxUtilization {
		maxUtilization = memoryUtilization
	}

	log.Debug().
		Float64("cpuUtilization", cpuUtilization).
		Float64("memoryUtilization", memoryUtilization).
		Float64("maxAllowed", sc.maxClusterUtilizationPercent).
		Msg("Cluster capacity buffer check")

	if maxUtilization > sc.maxClusterUtilizationPercent {
		return false, fmt.Sprintf("cluster would be %.1f%% utilized (max allowed: %.1f%%)", maxUtilization, sc.maxClusterUtilizationPercent)
	}

	return true, ""
}

// isNodeReady checks if a node is in Ready state
func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
