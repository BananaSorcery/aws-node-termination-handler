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
	"time"
)

// Config holds spot guard configuration
type Config struct {
	// Enabled determines if spot guard on-demand scale-down is enabled
	Enabled bool

	// SpotASGName is the name of the spot instance Auto Scaling Group
	SpotASGName string

	// OnDemandASGName is the name of the on-demand instance Auto Scaling Group (fallback)
	OnDemandASGName string

	// MinimumWaitDuration is the minimum time to wait before considering scale-down
	// Default: 10 minutes
	MinimumWaitDuration time.Duration

	// CheckInterval is how often to check for scale-down opportunities
	// Default: 30 seconds
	CheckInterval time.Duration

	// SpotStabilityDuration is how long spot capacity must be stable before trusting it
	// Default: 2 minutes
	SpotStabilityDuration time.Duration

	// MaxClusterUtilization is the maximum cluster utilization percentage allowed before scale-down
	// Default: 75% (0.75)
	MaxClusterUtilization float64

	// PodEvictionTimeout is the maximum time to wait for pod eviction during drain
	// Default: 5 minutes
	PodEvictionTimeout time.Duration

	// CleanupInterval is how often to cleanup old events
	// Default: 10 minutes
	CleanupInterval time.Duration

	// MaxEventAge is the maximum age of events to keep in tracking
	// Default: 24 hours
	MaxEventAge time.Duration
}

// DefaultConfig returns the recommended default configuration
func DefaultConfig() Config {
	return Config{
		Enabled:               false, // Must be explicitly enabled
		SpotASGName:           "",
		OnDemandASGName:       "",
		MinimumWaitDuration:   10 * time.Minute,
		CheckInterval:         30 * time.Second,
		SpotStabilityDuration: 2 * time.Minute,
		MaxClusterUtilization: 75.0,
		PodEvictionTimeout:    5 * time.Minute,
		CleanupInterval:       10 * time.Minute,
		MaxEventAge:           24 * time.Hour,
	}
}

// ConservativeConfig returns a conservative configuration (safer, higher cost)
func ConservativeConfig() Config {
	cfg := DefaultConfig()
	cfg.MinimumWaitDuration = 15 * time.Minute
	cfg.SpotStabilityDuration = 5 * time.Minute
	cfg.MaxClusterUtilization = 70.0
	cfg.CheckInterval = 60 * time.Second
	return cfg
}

// AggressiveConfig returns an aggressive configuration (maximum savings, riskier)
func AggressiveConfig() Config {
	cfg := DefaultConfig()
	cfg.MinimumWaitDuration = 5 * time.Minute
	cfg.SpotStabilityDuration = 1 * time.Minute
	cfg.MaxClusterUtilization = 80.0
	cfg.CheckInterval = 20 * time.Second
	return cfg
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // No validation needed if disabled
	}

	if c.SpotASGName == "" {
		return ErrSpotASGNameRequired
	}

	if c.OnDemandASGName == "" {
		return ErrOnDemandASGNameRequired
	}

	if c.MinimumWaitDuration < time.Minute {
		return ErrMinimumWaitTooShort
	}

	if c.SpotStabilityDuration < 30*time.Second {
		return ErrStabilityDurationTooShort
	}

	if c.MaxClusterUtilization <= 0 || c.MaxClusterUtilization > 100 {
		return ErrInvalidMaxUtilization
	}

	return nil
}
