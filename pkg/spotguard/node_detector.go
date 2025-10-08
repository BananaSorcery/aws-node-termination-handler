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

	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeDetector detects whether the current node is an on-demand or spot node
type NodeDetector struct {
	imds      *ec2metadata.Service
	asgClient autoscalingiface.AutoScalingAPI
	clientset kubernetes.Interface
	nodeName  string
}

// NewNodeDetector creates a new node detector
func NewNodeDetector(imds *ec2metadata.Service, asgClient autoscalingiface.AutoScalingAPI, clientset kubernetes.Interface, nodeName string) *NodeDetector {
	return &NodeDetector{
		imds:      imds,
		asgClient: asgClient,
		clientset: clientset,
		nodeName:  nodeName,
	}
}

// IsOnDemandNode determines if this pod is running on an on-demand node
// It tries multiple detection methods with fallback
func (nd *NodeDetector) IsOnDemandNode(onDemandASGName string) (bool, error) {
	log.Debug().
		Str("nodeName", nd.nodeName).
		Str("onDemandASG", onDemandASGName).
		Msg("Detecting node type")

	// Method 1: Check via ASG membership (most reliable)
	isOnDemand, err := nd.detectViaASG(onDemandASGName)
	if err == nil {
		return isOnDemand, nil
	}
	log.Debug().Err(err).Msg("ASG detection failed, trying node labels")

	// Method 2: Check via node labels (EKS sets these)
	isOnDemand, err = nd.detectViaNodeLabels()
	if err == nil {
		return isOnDemand, nil
	}
	log.Debug().Err(err).Msg("Node label detection failed")

	// If all methods fail, assume spot (safer default - no monitoring)
	log.Warn().
		Str("nodeName", nd.nodeName).
		Msg("Could not definitively determine node type, assuming spot node (safer default)")
	return false, fmt.Errorf("failed to detect node type: %w", err)
}

// detectViaASG checks if this instance is in the on-demand ASG
func (nd *NodeDetector) detectViaASG(onDemandASGName string) (bool, error) {
	// Get instance ID from IMDS
	instanceID, err := nd.imds.GetMetadataInfo("instance-id", false)
	if err != nil {
		return false, fmt.Errorf("failed to get instance ID from IMDS: %w", err)
	}

	log.Debug().
		Str("instanceID", instanceID).
		Str("onDemandASG", onDemandASGName).
		Msg("Checking ASG membership")

	// Check if this instance is in any ASG
	result, err := nd.asgClient.DescribeAutoScalingInstances(&autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []*string{&instanceID},
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe ASG instances: %w", err)
	}

	if len(result.AutoScalingInstances) == 0 {
		// Not in any ASG - assume spot
		log.Debug().Str("instanceID", instanceID).Msg("Instance not in any ASG, assuming spot")
		return false, nil
	}

	asgName := *result.AutoScalingInstances[0].AutoScalingGroupName
	isOnDemand := asgName == onDemandASGName

	log.Info().
		Str("instanceID", instanceID).
		Str("currentASG", asgName).
		Str("onDemandASG", onDemandASGName).
		Bool("isOnDemand", isOnDemand).
		Msg("Detected node type via ASG membership")

	return isOnDemand, nil
}

// detectViaNodeLabels checks node labels for capacity type
func (nd *NodeDetector) detectViaNodeLabels() (bool, error) {
	node, err := nd.clientset.CoreV1().Nodes().Get(context.Background(), nd.nodeName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get node: %w", err)
	}

	log.Debug().
		Str("nodeName", nd.nodeName).
		Int("labelCount", len(node.Labels)).
		Msg("Checking node labels for capacity type")

	// Check for Karpenter capacity type label
	if capacityType, exists := node.Labels["karpenter.sh/capacity-type"]; exists {
		isOnDemand := capacityType != "spot"
		log.Info().
			Str("nodeName", nd.nodeName).
			Str("capacityType", capacityType).
			Bool("isOnDemand", isOnDemand).
			Msg("Detected node type via Karpenter label")
		return isOnDemand, nil
	}

	// Check for EKS node group capacity type label
	if capacityType, exists := node.Labels["eks.amazonaws.com/capacityType"]; exists {
		isOnDemand := capacityType == "ON_DEMAND"
		log.Info().
			Str("nodeName", nd.nodeName).
			Str("capacityType", capacityType).
			Bool("isOnDemand", isOnDemand).
			Msg("Detected node type via EKS label")
		return isOnDemand, nil
	}

	// Check for node lifecycle label (some setups use this)
	if lifecycle, exists := node.Labels["node.kubernetes.io/lifecycle"]; exists {
		isOnDemand := lifecycle != "spot"
		log.Info().
			Str("nodeName", nd.nodeName).
			Str("lifecycle", lifecycle).
			Bool("isOnDemand", isOnDemand).
			Msg("Detected node type via lifecycle label")
		return isOnDemand, nil
	}

	return false, fmt.Errorf("no capacity type labels found on node")
}
