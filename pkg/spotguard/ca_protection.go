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
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Cluster Autoscaler annotation to prevent scale-down
	AnnotationCAScaleDownDisabled = "cluster-autoscaler.kubernetes.io/scale-down-disabled"

	// NTH annotation to track protection expiry time
	AnnotationCAProtectedUntil = "nth.aws.amazon.com/ca-protected-until"
)

// CAProtector manages Cluster Autoscaler protection for spot nodes
type CAProtector struct {
	clientset kubernetes.Interface
	nodeName  string
	config    config.Config
}

// NewCAProtector creates a new CA protector for the current spot node
func NewCAProtector(clientset kubernetes.Interface, nodeName string, nthConfig config.Config) *CAProtector {
	return &CAProtector{
		clientset: clientset,
		nodeName:  nodeName,
		config:    nthConfig,
	}
}

// Start begins the CA protection monitoring loop
func (cp *CAProtector) Start(ctx context.Context) {
	log.Info().
		Str("nodeName", cp.nodeName).
		Msg("🛡️  Starting Cluster Autoscaler protection for spot node")

	// Apply protection immediately on startup
	cp.checkAndApplyProtection(ctx)

	// Then check every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().
				Str("nodeName", cp.nodeName).
				Msg("CA protection stopped")
			return
		case <-ticker.C:
			cp.checkAndApplyProtection(ctx)
		}
	}
}

// checkAndApplyProtection checks if protection is needed and applies/removes it
func (cp *CAProtector) checkAndApplyProtection(ctx context.Context) {
	node, err := cp.clientset.CoreV1().Nodes().Get(ctx, cp.nodeName, metav1.GetOptions{})
	if err != nil {
		log.Warn().
			Err(err).
			Str("nodeName", cp.nodeName).
			Msg("Failed to get node for CA protection check")
		return
	}

	// Calculate protection duration
	// Total = spotStabilityDuration + minimumWaitDuration + podMigrationBuffer
	protectionDuration := time.Duration(
		cp.config.SpotGuardSpotStabilityDuration+
			cp.config.SpotGuardMinimumWaitDuration+
			cp.config.SpotGuardPodMigrationBuffer,
	) * time.Second

	// Calculate when protection should expire (based on node creation time)
	protectedUntil := node.CreationTimestamp.Add(protectionDuration)
	now := time.Now()

	// Check if node should be protected
	if now.Before(protectedUntil) {
		// Node should be protected
		cp.applyProtection(ctx, node, protectedUntil)
	} else {
		// Node protection should be removed
		cp.removeProtection(ctx, node)
	}
}

// applyProtection applies CA scale-down protection to the node
func (cp *CAProtector) applyProtection(ctx context.Context, node *corev1.Node, protectedUntil time.Time) {
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	// Check if already protected
	existingProtection := node.Annotations[AnnotationCAScaleDownDisabled]
	if existingProtection == "true" {
		// Already protected, no update needed
		log.Debug().
			Str("nodeName", cp.nodeName).
			Time("protectedUntil", protectedUntil).
			Msg("CA protection already applied")
		return
	}

	// Apply protection annotations
	node.Annotations[AnnotationCAScaleDownDisabled] = "true"
	node.Annotations[AnnotationCAProtectedUntil] = protectedUntil.Format(time.RFC3339)

	_, err := cp.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		log.Warn().
			Err(err).
			Str("nodeName", cp.nodeName).
			Msg("Failed to apply CA protection annotation")
		return
	}

	remaining := time.Until(protectedUntil)
	log.Info().
		Str("nodeName", cp.nodeName).
		Time("protectedUntil", protectedUntil).
		Dur("remainingDuration", remaining).
		Msg("✅ Applied CA scale-down protection to spot node")
}

// removeProtection removes CA scale-down protection from the node
func (cp *CAProtector) removeProtection(ctx context.Context, node *corev1.Node) {
	if node.Annotations == nil {
		return
	}

	// Check if protection exists
	_, hasProtection := node.Annotations[AnnotationCAScaleDownDisabled]
	if !hasProtection {
		// Not protected, no update needed
		log.Debug().
			Str("nodeName", cp.nodeName).
			Msg("CA protection not present (already removed or never applied)")
		return
	}

	// Remove protection annotations
	delete(node.Annotations, AnnotationCAScaleDownDisabled)
	delete(node.Annotations, AnnotationCAProtectedUntil)

	_, err := cp.clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		log.Warn().
			Err(err).
			Str("nodeName", cp.nodeName).
			Msg("Failed to remove CA protection annotation")
		return
	}

	log.Info().
		Str("nodeName", cp.nodeName).
		Msg("✅ Removed CA scale-down protection from spot node (protection period expired)")
}
