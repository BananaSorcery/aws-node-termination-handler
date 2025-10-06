// Copyright 2024 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/rs/zerolog/log"
)

// SpotGuard handles scaling operations for spot instances with on-demand fallback
type SpotGuard struct {
	ASGClient            autoscalingiface.AutoScalingAPI
	SpotAsgName          string
	OnDemandAsgName      string
	ScaleTimeout         time.Duration
	CapacityCheckTimeout time.Duration
}

// NewSpotGuard creates a new SpotGuard instance
func NewSpotGuard(asgClient autoscalingiface.AutoScalingAPI, nthConfig *config.Config) *SpotGuard {
	return &SpotGuard{
		ASGClient:            asgClient,
		SpotAsgName:          nthConfig.SpotAsgName,
		OnDemandAsgName:      nthConfig.OnDemandAsgName,
		ScaleTimeout:         time.Duration(nthConfig.SpotGuardScaleTimeout) * time.Second,
		CapacityCheckTimeout: time.Duration(nthConfig.SpotGuardCapacityCheckTimeout) * time.Second,
	}
}

// ScaleUpWithFallback attempts to scale up spot instances, with fallback to on-demand
func (sg *SpotGuard) ScaleUpWithFallback() error {
	log.Info().Msgf("Spot Guard: Attempting to scale up spot ASG: %s", sg.SpotAsgName)

	// Try to scale up spot instance
	err := sg.scaleUpASG(sg.SpotAsgName)
	if err != nil {
		log.Warn().Err(err).Msgf("Spot Guard: Failed to initiate spot ASG scale-up")
		return sg.fallbackToOnDemand()
	}

	// Wait and check if new instance becomes InService
	success, err := sg.waitForNewInstance(sg.SpotAsgName)
	if err != nil {
		log.Warn().Err(err).Msgf("Spot Guard: Error checking spot instance status")
		return sg.fallbackToOnDemand()
	}

	if !success {
		log.Warn().Msgf("Spot Guard: Spot capacity appears unavailable (timeout waiting for InService)")
		return sg.fallbackToOnDemand()
	}

	log.Info().Msgf("Spot Guard: Successfully scaled up spot ASG: %s", sg.SpotAsgName)
	return nil
}

// scaleUpASG increases the desired capacity of an ASG by 1
func (sg *SpotGuard) scaleUpASG(asgName string) error {
	// Get current ASG configuration
	describeInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}

	describeOutput, err := sg.ASGClient.DescribeAutoScalingGroups(describeInput)
	if err != nil {
		return fmt.Errorf("failed to describe ASG %s: %w", asgName, err)
	}

	if len(describeOutput.AutoScalingGroups) == 0 {
		return fmt.Errorf("ASG %s not found", asgName)
	}

	asg := describeOutput.AutoScalingGroups[0]
	currentDesired := aws.Int64Value(asg.DesiredCapacity)
	maxSize := aws.Int64Value(asg.MaxSize)
	newDesired := currentDesired + 1

	if newDesired > maxSize {
		return fmt.Errorf("cannot scale up ASG %s: would exceed max size (%d)", asgName, maxSize)
	}

	log.Info().Msgf("Spot Guard: Scaling ASG %s from %d to %d instances", asgName, currentDesired, newDesired)

	// Update desired capacity
	updateInput := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int64(newDesired),
		HonorCooldown:        aws.Bool(false),
	}

	_, err = sg.ASGClient.SetDesiredCapacity(updateInput)
	if err != nil {
		return fmt.Errorf("failed to set desired capacity for ASG %s: %w", asgName, err)
	}

	return nil
}

// waitForNewInstance waits for a new instance to reach InService state
func (sg *SpotGuard) waitForNewInstance(asgName string) (bool, error) {
	startTime := time.Now()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Get initial instance count
	initialCount, err := sg.getInServiceInstanceCount(asgName)
	if err != nil {
		return false, err
	}

	log.Info().Msgf("Spot Guard: Waiting for new instance in ASG %s (initial count: %d)", asgName, initialCount)

	for range ticker.C {
		currentCount, err := sg.getInServiceInstanceCount(asgName)
		if err != nil {
			log.Warn().Err(err).Msg("Spot Guard: Error checking instance count")
			continue
		}

		if currentCount > initialCount {
			log.Info().Msgf("Spot Guard: New instance reached InService in ASG %s (count: %d)", asgName, currentCount)
			return true, nil
		}

		// Check for capacity issues in scaling activities
		hasCapacityIssue, err := sg.checkForCapacityIssues(asgName)
		if err != nil {
			log.Warn().Err(err).Msg("Spot Guard: Error checking scaling activities")
		}
		if hasCapacityIssue {
			log.Warn().Msgf("Spot Guard: Detected capacity issue in ASG %s", asgName)
			return false, nil
		}

		elapsed := time.Since(startTime)
		log.Debug().Msgf("Spot Guard: Still waiting for instance (elapsed: %v)", elapsed)

		if elapsed >= sg.CapacityCheckTimeout {
			log.Warn().Msgf("Spot Guard: Timeout waiting for instance in ASG %s", asgName)
			return false, nil
		}
	}

	return false, nil
}

// getInServiceInstanceCount returns the number of InService instances in an ASG
func (sg *SpotGuard) getInServiceInstanceCount(asgName string) (int, error) {
	describeInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	}

	describeOutput, err := sg.ASGClient.DescribeAutoScalingGroups(describeInput)
	if err != nil {
		return 0, fmt.Errorf("failed to describe ASG %s: %w", asgName, err)
	}

	if len(describeOutput.AutoScalingGroups) == 0 {
		return 0, fmt.Errorf("ASG %s not found", asgName)
	}

	asg := describeOutput.AutoScalingGroups[0]
	inServiceCount := 0

	for _, instance := range asg.Instances {
		if aws.StringValue(instance.LifecycleState) == "InService" {
			inServiceCount++
		}
	}

	return inServiceCount, nil
}

// checkForCapacityIssues checks recent scaling activities for capacity-related errors
func (sg *SpotGuard) checkForCapacityIssues(asgName string) (bool, error) {
	input := &autoscaling.DescribeScalingActivitiesInput{
		AutoScalingGroupName: aws.String(asgName),
		MaxRecords:           aws.Int64(10),
	}

	output, err := sg.ASGClient.DescribeScalingActivities(input)
	if err != nil {
		return false, fmt.Errorf("failed to describe scaling activities for ASG %s: %w", asgName, err)
	}

	// Check recent activities for capacity issues
	for _, activity := range output.Activities {
		statusCode := aws.StringValue(activity.StatusCode)
		description := aws.StringValue(activity.Description)
		cause := aws.StringValue(activity.Cause)

		// Look for capacity-related failures
		if statusCode == "Failed" || statusCode == "Cancelled" {
			log.Debug().Msgf("Spot Guard: Found failed activity - Status: %s, Description: %s, Cause: %s",
				statusCode, description, cause)

			// Check for common capacity issue indicators
			capacityKeywords := []string{
				"InsufficientInstanceCapacity",
				"Insufficient capacity",
				"capacity",
				"capacity-not-available",
				"insufficient",
			}

			for _, keyword := range capacityKeywords {
				if contains(description, keyword) || contains(cause, keyword) {
					log.Info().Msgf("Spot Guard: Detected capacity issue in activity: %s", description)
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// fallbackToOnDemand scales up the on-demand ASG
func (sg *SpotGuard) fallbackToOnDemand() error {
	log.Warn().Msgf("Spot Guard: Falling back to on-demand ASG: %s", sg.OnDemandAsgName)

	err := sg.scaleUpASG(sg.OnDemandAsgName)
	if err != nil {
		return fmt.Errorf("failed to scale up on-demand ASG: %w", err)
	}

	// Wait for on-demand instance
	success, err := sg.waitForNewInstance(sg.OnDemandAsgName)
	if err != nil {
		return fmt.Errorf("error waiting for on-demand instance: %w", err)
	}

	if !success {
		return fmt.Errorf("timeout waiting for on-demand instance to reach InService")
	}

	log.Info().Msgf("Spot Guard: Successfully scaled up on-demand ASG: %s", sg.OnDemandAsgName)
	return nil
}

// contains is a helper function to check if a string contains a substring (case-insensitive)
func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr ||
			len(str) > len(substr) &&
				(str[:len(substr)] == substr ||
					str[len(str)-len(substr):] == substr ||
					indexOf(str, substr) >= 0))
}

// indexOf returns the index of substr in str, or -1 if not found
func indexOf(str, substr string) int {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
