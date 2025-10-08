# 🎯 Spot Guard CRD Solution - Complete Guide

## Overview

This guide shows how to use Kubernetes Custom Resource Definitions (CRD) to track Spot Guard fallback events in a distributed manner.

## 📋 **Complete Timeline Example**

### **Phase 0: One-Time Setup (Installation)**

#### **Step 1: Define the CRD**

```yaml
# spotguard-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: spotguardfallbackevents.aws.amazon.com
spec:
  group: aws.amazon.com
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                spotASGName:
                  type: string
                  description: "Name of the spot Auto Scaling Group"
                onDemandASGName:
                  type: string
                  description: "Name of the on-demand Auto Scaling Group"
                onDemandInstanceID:
                  type: string
                  description: "EC2 instance ID of the on-demand node"
                onDemandNodeName:
                  type: string
                  description: "Kubernetes node name of the on-demand node"
                timestamp:
                  type: string
                  format: date-time
                  description: "When the fallback occurred"
                minimumWaitDuration:
                  type: string
                  description: "Minimum wait time before scale-down"
              required:
                - spotASGName
                - onDemandASGName
                - onDemandNodeName
                - timestamp
            status:
              type: object
              properties:
                spotCapacityRestored:
                  type: boolean
                  description: "Whether spot capacity is restored"
                scaleDownInitiated:
                  type: boolean
                  description: "Whether scale-down has been initiated"
                spotHealthySince:
                  type: string
                  format: date-time
                  description: "When spot became healthy"
                phase:
                  type: string
                  enum: ["Pending", "Monitoring", "ReadyForScaleDown", "ScalingDown", "Completed", "Failed"]
                  description: "Current phase of the fallback event"
                message:
                  type: string
                  description: "Human-readable status message"
      subresources:
        status: {}
      additionalPrinterColumns:
        - name: Spot ASG
          type: string
          jsonPath: .spec.spotASGName
        - name: On-Demand Node
          type: string
          jsonPath: .spec.onDemandNodeName
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Age
          type: date
          jsonPath: .spec.timestamp
  scope: Namespaced
  names:
    plural: spotguardfallbackevents
    singular: spotguardfallbackevent
    kind: SpotGuardFallbackEvent
    shortNames:
      - sgfe
```

#### **Step 2: Install the CRD**

```bash
kubectl apply -f spotguard-crd.yaml
```

#### **Step 3: Update RBAC**

```yaml
# Add to ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: aws-node-termination-handler
rules:
  # ... existing rules ...
  
  # Spot Guard CRD permissions
  - apiGroups: ["aws.amazon.com"]
    resources: ["spotguardfallbackevents"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["aws.amazon.com"]
    resources: ["spotguardfallbackevents/status"]
    verbs: ["get", "update", "patch"]
```

---

### **Phase 1: Rebalance Recommendation (T+0s)**

#### **10:00:00 - Spot Node A receives rebalance recommendation**

```
NTH Pod on Spot Node A (ip-10-0-1-50):
  - Detects rebalance recommendation from IMDS
  - Attempts to scale up spot ASG
```

```bash
# Logs from NTH Pod on Spot Node A
INFO: Rebalance recommendation detected on node ip-10-0-1-50
INFO: Spot Guard: Attempting to scale up spot ASG: my-spot-asg
INFO: Spot Guard: Scaling ASG my-spot-asg from 3 to 4 instances
```

---

### **Phase 2: Spot Capacity Unavailable (T+30s)**

#### **10:00:30 - Spot capacity fails, fallback to on-demand**

```
NTH Pod on Spot Node A:
  - Detects spot capacity unavailable
  - Scales up on-demand ASG
  - Creates CRD object to track this event
```

```bash
# Logs from NTH Pod on Spot Node A
WARN: Spot Guard: Spot capacity appears unavailable (timeout waiting for InService)
WARN: Spot Guard: Falling back to on-demand ASG: my-ondemand-asg
INFO: Spot Guard: Scaling ASG my-ondemand-asg from 0 to 1 instances
INFO: Spot Guard: Successfully scaled up on-demand ASG: my-ondemand-asg
```

#### **10:00:45 - CRD Object Created**

```bash
# NTH creates this CRD object
kubectl apply -f - <<EOF
apiVersion: aws.amazon.com/v1
kind: SpotGuardFallbackEvent
metadata:
  name: fallback-1736938845-abc123
  namespace: kube-system
  labels:
    app: aws-node-termination-handler
    component: spot-guard
spec:
  spotASGName: my-spot-asg
  onDemandASGName: my-ondemand-asg
  onDemandInstanceID: i-0987654321fedcba0
  onDemandNodeName: ip-10-0-2-100
  timestamp: "2025-01-15T10:00:45Z"
  minimumWaitDuration: "10m"
status:
  spotCapacityRestored: false
  scaleDownInitiated: false
  phase: "Pending"
  message: "Fallback to on-demand completed, waiting 10m before monitoring"
EOF
```

```bash
# You can now see it with kubectl
$ kubectl get spotguardfallbackevents -n kube-system
NAME                          SPOT ASG       ON-DEMAND NODE    PHASE     AGE
fallback-1736938845-abc123   my-spot-asg    ip-10-0-2-100     Pending   5s
```

---

### **Phase 3: Spot Node Termination (T+2m)**

#### **10:02:00 - Spot Instance A gets 2-minute warning**

```
Spot Node A (ip-10-0-1-50):
  - Receives spot interruption notice
  - NTH taints and drains the node
  - Pods get evicted
  - NTH Pod on Spot Node A is TERMINATED
```

```bash
# Logs from NTH Pod on Spot Node A (before termination)
INFO: Spot interruption notice received for node ip-10-0-1-50
INFO: Tainting node ip-10-0-1-50
INFO: Draining node ip-10-0-1-50
INFO: Pod aws-node-termination-handler-xyz evicted
# ... Pod terminates ...
```

#### **10:02:30 - Spot Node A terminates**

```
✅ CRD Object SURVIVES (stored in etcd)
✅ Other NTH pods continue monitoring
```

```bash
# CRD still exists even after pod termination
$ kubectl get spotguardfallbackevents -n kube-system
NAME                          SPOT ASG       ON-DEMAND NODE    PHASE        AGE
fallback-1736938845-abc123   my-spot-asg    ip-10-0-2-100     Monitoring   1m45s
```

---

### **Phase 4: Distributed Monitoring (T+3m - T+10m)**

#### **10:03:00 - All NTH Pods Monitor the CRD**

```
NTH Pod on Spot Node B (ip-10-0-1-51):
  ↓ Watches for SpotGuardFallbackEvent objects
  ↓ Discovers: fallback-1736938845-abc123

NTH Pod on Spot Node C (ip-10-0-1-52):
  ↓ Watches for SpotGuardFallbackEvent objects
  ↓ Discovers: fallback-1736938845-abc123

NTH Pod on On-Demand Node (ip-10-0-2-100):
  ↓ Watches for SpotGuardFallbackEvent objects
  ↓ Discovers: fallback-1736938845-abc123

ALL Pods monitor ALL events!
```

```bash
# Logs from NTH Pod on Spot Node B
DEBUG: Discovered fallback event: fallback-1736938845-abc123
DEBUG: Minimum wait time not met: 7m30s remaining
```

#### **10:10:00 - Minimum Wait Time Elapsed**

```bash
# Logs from ANY NTH Pod
INFO: Fallback event fallback-1736938845-abc123 passed minimum wait time
INFO: Starting spot capacity health checks
```

#### **10:10:05 - Update CRD Status**

```bash
# Any pod can update the status
kubectl patch spotguardfallbackevent fallback-1736938845-abc123 -n kube-system --type=merge -p '
{
  "status": {
    "phase": "Monitoring",
    "message": "Minimum wait time elapsed, monitoring spot ASG health"
  }
}'
```

---

### **Phase 5: Spot Capacity Restored (T+12m)**

#### **10:12:00 - Spot ASG Becomes Healthy**

```
NTH Pod on Spot Node B checks:
  ✅ Spot ASG InService count == Desired capacity
  ✅ All spot nodes ready in Kubernetes
  ✅ No scaling activities in progress
  ✅ Stable for 2+ minutes
```

```bash
# Logs from NTH Pod on Spot Node B
INFO: Spot ASG my-spot-asg is now healthy (InService: 3, Desired: 3)
INFO: Spot ASG has been stable for 2m15s
INFO: Updating fallback event status
```

#### **10:12:05 - Update CRD: Spot Capacity Restored**

```bash
kubectl patch spotguardfallbackevent fallback-1736938845-abc123 -n kube-system --type=merge -p '
{
  "status": {
    "spotCapacityRestored": true,
    "spotHealthySince": "2025-01-15T10:09:45Z",
    "phase": "ReadyForScaleDown",
    "message": "Spot capacity restored and stable, ready for scale-down"
  }
}'
```

---

### **Phase 6: Safety Checks (T+12m)**

#### **10:12:10 - One Pod Performs Safety Checks**

```
NTH Pod on Spot Node C (elected to handle this):
  ✅ Check pod safety (can reschedule)
  ✅ Check PodDisruptionBudgets
  ✅ Check cluster capacity buffer
```

```bash
# Logs from NTH Pod on Spot Node C
INFO: Performing safety checks for on-demand node ip-10-0-2-100
INFO: Pod safety check passed - all pods can be safely rescheduled
INFO: PodDisruptionBudget check passed
INFO: Cluster capacity buffer check passed (utilization: 65%)
INFO: All safety checks passed, initiating scale-down
```

#### **10:12:15 - Update CRD: Scale-Down Initiated**

```bash
kubectl patch spotguardfallbackevent fallback-1736938845-abc123 -n kube-system --type=merge -p '
{
  "status": {
    "scaleDownInitiated": true,
    "phase": "ScalingDown",
    "message": "Safety checks passed, scaling down on-demand node"
  }
}'
```

**🔒 Kubernetes Optimistic Concurrency Control:**
- If another pod tries to update at the same time, Kubernetes will reject it
- The rejected pod will retry and see the event is already being handled
- This prevents duplicate scale-downs

---

### **Phase 7: Scale-Down Execution (T+12m - T+17m)**

#### **10:12:20 - Scale Down On-Demand Node**

```
NTH Pod on Spot Node C executes:
  1. Taint on-demand node
  2. Cordon on-demand node
  3. Drain on-demand node
  4. Wait for pods to reschedule
  5. Scale down on-demand ASG
```

```bash
# Logs from NTH Pod on Spot Node C
INFO: Step 1/5: Tainting node ip-10-0-2-100
INFO: Step 2/5: Cordoning node ip-10-0-2-100
INFO: Step 3/5: Draining node ip-10-0-2-100 (evicting pods)
INFO: Step 4/5: Waiting for pods to be rescheduled
INFO: Step 5/5: Scaling down on-demand ASG from 1 to 0
INFO: Successfully completed on-demand node scale-down
```

#### **10:17:00 - Scale-Down Complete**

```bash
# Update CRD final status
kubectl patch spotguardfallbackevent fallback-1736938845-abc123 -n kube-system --type=merge -p '
{
  "status": {
    "phase": "Completed",
    "message": "On-demand node successfully scaled down"
  }
}'
```

---

### **Phase 8: Cleanup (T+24h)**

#### **Next Day - Automatic Cleanup**

```bash
# After 24 hours, cleanup old events
kubectl delete spotguardfallbackevent fallback-1736938845-abc123 -n kube-system
```

---

## 🎯 **Complete Timeline Summary**

| Time | Event | Who Handles | CRD Status |
|------|-------|-------------|------------|
| T+0s | Rebalance detected | Spot Node A | N/A |
| T+30s | Fallback to on-demand | Spot Node A | Created (Pending) |
| T+45s | CRD created | Spot Node A | Pending |
| T+2m | Spot Node A terminates | - | ✅ CRD survives |
| T+3m | Discovery by other pods | All pods | Monitoring |
| T+10m | Min wait time elapsed | All pods | Monitoring |
| T+12m | Spot capacity restored | Any pod | ReadyForScaleDown |
| T+12m | Safety checks passed | One pod (elected) | ScalingDown |
| T+17m | Scale-down complete | One pod | Completed |
| T+24h | Cleanup | Any pod | Deleted |

---

## 🔍 **How CRD Handles Race Conditions**

### **Scenario: Two Pods Try to Scale Down at Same Time**

```
10:12:15 - Pod B tries to update CRD status:
  GET spotguardfallbackevent/fallback-123 (resourceVersion: 42)
  Modify: scaleDownInitiated = true
  UPDATE with resourceVersion: 42
  ✅ SUCCESS (first to update)

10:12:15 - Pod C tries to update CRD status:
  GET spotguardfallbackevent/fallback-123 (resourceVersion: 42)
  Modify: scaleDownInitiated = true
  UPDATE with resourceVersion: 42
  ❌ CONFLICT (resourceVersion changed to 43 by Pod B)
  
  Pod C retries:
    GET spotguardfallbackevent/fallback-123 (resourceVersion: 43)
    Sees: scaleDownInitiated = true (already set by Pod B)
    Skips scale-down operation
    ✅ No duplicate scale-down!
```

---

## 💡 **Benefits of CRD Approach**

### **1. Strongly Typed**
```bash
# Kubernetes validates the schema
kubectl apply -f - <<EOF
apiVersion: aws.amazon.com/v1
kind: SpotGuardFallbackEvent
spec:
  spotASGName: 123  # ❌ Error: expected string, got number
EOF
```

### **2. Native Kubernetes Tooling**
```bash
# Use standard kubectl commands
kubectl get spotguardfallbackevents
kubectl describe spotguardfallbackevent fallback-123
kubectl delete spotguardfallbackevent fallback-123

# Watch for changes
kubectl get spotguardfallbackevents --watch

# Filter by labels
kubectl get spotguardfallbackevents -l component=spot-guard
```

### **3. Built-in Versioning**
```bash
# Kubernetes handles optimistic concurrency control
# No race conditions for updates
```

### **4. Status Subresource**
```yaml
# Separate spec (desired state) and status (observed state)
spec:
  spotASGName: my-spot-asg  # What we want
status:
  phase: "Monitoring"        # What's happening
```

### **5. Better Observability**
```bash
# See all fallback events at a glance
$ kubectl get spotguardfallbackevents -n kube-system
NAME                    SPOT ASG      ON-DEMAND NODE    PHASE            AGE
fallback-123           my-spot-asg   ip-10-0-2-100     ReadyForScaleDown 12m
fallback-456           my-spot-asg   ip-10-0-2-101     Monitoring        5m
```

---

## 🔧 **Implementation Example**

```go
// pkg/spotguard/crd_tracker.go
package spotguard

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

type CRDFallbackTracker struct {
	clientset kubernetes.Interface
	// Use dynamic client for CRD operations
}

// AddEvent creates a new CRD object
func (ct *CRDFallbackTracker) AddEvent(event *FallbackEvent) error {
	crd := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "aws.amazon.com/v1",
			"kind":       "SpotGuardFallbackEvent",
			"metadata": map[string]interface{}{
				"name":      event.EventID,
				"namespace": "kube-system",
				"labels": map[string]interface{}{
					"app":       "aws-node-termination-handler",
					"component": "spot-guard",
				},
			},
			"spec": map[string]interface{}{
				"spotASGName":         event.SpotASGName,
				"onDemandASGName":     event.OnDemandASGName,
				"onDemandInstanceID":  event.OnDemandInstanceID,
				"onDemandNodeName":    event.OnDemandNodeName,
				"timestamp":           event.Timestamp.Format(time.RFC3339),
				"minimumWaitDuration": event.MinimumWaitDuration.String(),
			},
			"status": map[string]interface{}{
				"spotCapacityRestored": false,
				"scaleDownInitiated":   false,
				"phase":                "Pending",
				"message":              "Fallback to on-demand completed",
			},
		},
	}

	// Create the CRD object
	_, err := ct.dynamicClient.Resource(spotGuardGVR).Namespace("kube-system").Create(context.Background(), crd, metav1.CreateOptions{})
	return err
}

// GetActiveEvents watches for CRD objects
func (ct *CRDFallbackTracker) GetActiveEvents() []*FallbackEvent {
	list, err := ct.dynamicClient.Resource(spotGuardGVR).Namespace("kube-system").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return []*FallbackEvent{}
	}

	events := make([]*FallbackEvent, 0)
	for _, item := range list.Items {
		// Convert CRD to FallbackEvent
		event := convertCRDToEvent(&item)
		if !event.ScaleDownInitiated {
			events = append(events, event)
		}
	}
	return events
}
```

---

## 🎯 **Summary: Why CRD is Better**

| Feature | In-Memory | ConfigMap | CRD |
|---------|-----------|-----------|-----|
| **Survives pod restart** | ❌ | ✅ | ✅ |
| **Strong typing** | ❌ | ❌ | ✅ |
| **Race condition handling** | ❌ | ⚠️ | ✅ |
| **Native tooling** | ❌ | ⚠️ | ✅ |
| **Observability** | ❌ | ⚠️ | ✅ |
| **Setup complexity** | Simple | Simple | Medium |

**CRD is the most robust solution for production!** 🚀
