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

import "errors"

var (
	// ErrSpotASGNameRequired is returned when spot ASG name is not provided
	ErrSpotASGNameRequired = errors.New("spot ASG name is required")

	// ErrOnDemandASGNameRequired is returned when on-demand ASG name is not provided
	ErrOnDemandASGNameRequired = errors.New("on-demand ASG name is required")

	// ErrMinimumWaitTooShort is returned when minimum wait duration is too short
	ErrMinimumWaitTooShort = errors.New("minimum wait duration must be at least 1 minute")

	// ErrStabilityDurationTooShort is returned when stability duration is too short
	ErrStabilityDurationTooShort = errors.New("stability duration must be at least 30 seconds")

	// ErrInvalidMaxUtilization is returned when max utilization is invalid
	ErrInvalidMaxUtilization = errors.New("max cluster utilization must be between 0 and 100")
)
