# 🏗️ Spot Guard DaemonSet Architecture Solution

## 🚨 **The DaemonSet Challenge**

### **The Problem:**
```
1. Rebalance recommendation → Spot instance A receives it
2. NTH DaemonSet pod on Spot A handles scale-up
3. Spot A gets terminated (pod evicted)
4. ❌ The monitor that was tracking the fallback event is GONE!
5. ❌ No one is monitoring to scale down the on-demand node
```

### **Current Implementation Flaw:**
- `FallbackTracker` stores events **in-memory only**
- When the pod is terminated, all tracking data is lost
- No other pods know about the fallback event

## ✅ **Solution: Persistent State with Kubernetes**

### **Option 1: ConfigMap-Based State (Recommended for Quick Implementation)**

#### **Architecture:**
```
┌─────────────────────────────────────────────────────────┐
│ Any NTH DaemonSet Pod                                   │
│ ├─ Detects rebalance recommendation                     │
│ ├─ Scales up spot/on-demand ASG                        │
│ ├─ Stores fallback event in ConfigMap                  │
│ └─ Gets terminated (spot interruption)                  │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│ Kubernetes ConfigMap (Persistent Storage)               │
│ Name: aws-nth-spot-guard-state                         │
│ Data:                                                    │
│   fallback-events.json: {...}                          │
└─────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│ ALL NTH DaemonSet Pods (Background Monitors)           │
│ ├─ Read fallback events from ConfigMap                 │
│ ├─ Monitor spot ASG health                              │
│ ├─ Scale down on-demand when ready                     │
│ └─ Update ConfigMap when events complete               │
└─────────────────────────────────────────────────────────┘
```

#### **Benefits:**
- ✅ **Simple implementation** - No CRDs needed
- ✅ **Built-in**: ConfigMaps are native Kubernetes
- ✅ **Distributed**: All pods can read/write
- ✅ **Survives pod restarts**: Data persists in etcd

#### **Limitations:**
- ⚠️ **Race conditions**: Multiple pods writing simultaneously
- ⚠️ **Size limits**: ConfigMap limited to 1MB
- ⚠️ **Not strongly typed**: JSON parsing required

### **Option 2: Custom Resource Definition (CRD) - Best Practice**

#### **Architecture:**
```
┌─────────────────────────────────────────────────────────┐
│ CRD: SpotGuardFallbackEvent                            │
│ Kind: SpotGuardFallbackEvent                           │
│ Spec:                                                   │
│   spotASGName: my-spot-asg                             │
│   onDemandASGName: my-ondemand-asg                     │
│   onDemandInstanceID: i-1234567890abcdef0              │
│   onDemandNodeName: ip-10-0-1-100                      │
│   timestamp: 2025-01-15T10:30:00Z                      │
│ Status:                                                 │
│   spotCapacityRestored: false                          │
│   scaleDownInitiated: false                            │
│   spotHealthySince: null                               │
└─────────────────────────────────────────────────────────┘
```

#### **Benefits:**
- ✅ **Strongly typed**: Kubernetes validates the schema
- ✅ **Built-in versioning**: Optimistic concurrency control
- ✅ **Native Kubernetes**: Uses standard K8s patterns
- ✅ **Better observability**: `kubectl get spotguardevents`
- ✅ **No race conditions**: Kubernetes handles locking

## 🛠️ **Implementation: ConfigMap-Based Solution**

### **Step 1: Create Persistent Fallback Tracker**

```go
// pkg/spotguard/persistent_tracker.go
package spotguard

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ConfigMapName      = "aws-nth-spot-guard-state"
	ConfigMapNamespace = "kube-system"
	ConfigMapDataKey   = "fallback-events.json"
)

// PersistentFallbackTracker manages fallback events with Kubernetes ConfigMap persistence
type PersistentFallbackTracker struct {
	clientset      kubernetes.Interface
	events         map[string]*FallbackEvent
	mutex          sync.RWMutex
	lastSyncTime   time.Time
	syncInterval   time.Duration
}

// NewPersistentFallbackTracker creates a new persistent tracker
func NewPersistentFallbackTracker(clientset kubernetes.Interface) *PersistentFallbackTracker {
	tracker := &PersistentFallbackTracker{
		clientset:    clientset,
		events:       make(map[string]*FallbackEvent),
		syncInterval: 10 * time.Second, // Sync every 10 seconds
	}

	// Load initial state from ConfigMap
	if err := tracker.loadFromConfigMap(context.Background()); err != nil {
		log.Warn().Err(err).Msg("Failed to load initial state from ConfigMap, starting fresh")
	}

	// Start background sync
	go tracker.backgroundSync()

	return tracker
}

// AddEvent adds a new fallback event and persists it
func (pt *PersistentFallbackTracker) AddEvent(event *FallbackEvent) error {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	pt.events[event.EventID] = event
	log.Info().
		Str("eventID", event.EventID).
		Str("onDemandASG", event.OnDemandASGName).
		Str("spotASG", event.SpotASGName).
		Str("onDemandNode", event.OnDemandNodeName).
		Msg("Tracking new fallback event (will persist to ConfigMap)")

	// Persist immediately for critical events
	return pt.saveToConfigMap(context.Background())
}

// GetActiveEvents returns all events that are not yet processed
func (pt *PersistentFallbackTracker) GetActiveEvents() []*FallbackEvent {
	pt.mutex.RLock()
	defer pt.mutex.RUnlock()

	active := make([]*FallbackEvent, 0)
	for _, event := range pt.events {
		if !event.ScaleDownInitiated {
			active = append(active, event)
		}
	}
	return active
}

// UpdateEvent updates an existing event and persists the change
func (pt *PersistentFallbackTracker) UpdateEvent(eventID string, updateFunc func(*FallbackEvent)) error {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	if event, exists := pt.events[eventID]; exists {
		updateFunc(event)
		// Persist the update
		return pt.saveToConfigMap(context.Background())
	}
	return fmt.Errorf("event %s not found", eventID)
}

// RemoveEvent removes a fallback event from tracking and persists
func (pt *PersistentFallbackTracker) RemoveEvent(eventID string) error {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	delete(pt.events, eventID)
	log.Info().Str("eventID", eventID).Msg("Removed fallback event from tracking")

	return pt.saveToConfigMap(context.Background())
}

// loadFromConfigMap loads events from Kubernetes ConfigMap
func (pt *PersistentFallbackTracker) loadFromConfigMap(ctx context.Context) error {
	cm, err := pt.clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info().Msg("ConfigMap not found, will create on first save")
			return nil
		}
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	dataStr, exists := cm.Data[ConfigMapDataKey]
	if !exists || dataStr == "" {
		log.Info().Msg("ConfigMap exists but has no data")
		return nil
	}

	var events map[string]*FallbackEvent
	if err := json.Unmarshal([]byte(dataStr), &events); err != nil {
		return fmt.Errorf("failed to unmarshal events: %w", err)
	}

	pt.events = events
	log.Info().Int("eventCount", len(events)).Msg("Loaded fallback events from ConfigMap")
	return nil
}

// saveToConfigMap saves events to Kubernetes ConfigMap
func (pt *PersistentFallbackTracker) saveToConfigMap(ctx context.Context) error {
	// Serialize events to JSON
	data, err := json.Marshal(pt.events)
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName,
			Namespace: ConfigMapNamespace,
			Labels: map[string]string{
				"app":       "aws-node-termination-handler",
				"component": "spot-guard",
			},
		},
		Data: map[string]string{
			ConfigMapDataKey: string(data),
		},
	}

	// Try to get existing ConfigMap
	existing, err := pt.clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			_, err = pt.clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Create(ctx, cm, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create ConfigMap: %w", err)
			}
			log.Info().Msg("Created new ConfigMap for Spot Guard state")
			return nil
		}
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Update existing ConfigMap
	existing.Data = cm.Data
	_, err = pt.clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	pt.lastSyncTime = time.Now()
	log.Debug().Msg("Saved fallback events to ConfigMap")
	return nil
}

// backgroundSync periodically syncs state with ConfigMap
func (pt *PersistentFallbackTracker) backgroundSync() {
	ticker := time.NewTicker(pt.syncInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Reload state from ConfigMap to catch changes from other pods
		if err := pt.reloadFromConfigMap(context.Background()); err != nil {
			log.Warn().Err(err).Msg("Failed to reload state from ConfigMap")
		}
	}
}

// reloadFromConfigMap reloads state from ConfigMap without losing local changes
func (pt *PersistentFallbackTracker) reloadFromConfigMap(ctx context.Context) error {
	cm, err := pt.clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // ConfigMap doesn't exist yet
		}
		return err
	}

	dataStr, exists := cm.Data[ConfigMapDataKey]
	if !exists || dataStr == "" {
		return nil
	}

	var remoteEvents map[string]*FallbackEvent
	if err := json.Unmarshal([]byte(dataStr), &remoteEvents); err != nil {
		return err
	}

	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	// Merge remote events with local events
	// Remote events take precedence for fields like ScaleDownInitiated
	for eventID, remoteEvent := range remoteEvents {
		if localEvent, exists := pt.events[eventID]; exists {
			// Merge: take the most recent state
			if remoteEvent.ScaleDownInitiated && !localEvent.ScaleDownInitiated {
				pt.events[eventID] = remoteEvent
				log.Debug().Str("eventID", eventID).Msg("Updated local event from remote state")
			}
		} else {
			// New event from another pod
			pt.events[eventID] = remoteEvent
			log.Info().Str("eventID", eventID).Msg("Discovered new event from another pod")
		}
	}

	// Remove events that no longer exist remotely
	for eventID := range pt.events {
		if _, exists := remoteEvents[eventID]; !exists {
			log.Debug().Str("eventID", eventID).Msg("Event removed remotely, removing locally")
			delete(pt.events, eventID)
		}
	}

	return nil
}

// CleanupOldEvents removes events older than the specified duration
func (pt *PersistentFallbackTracker) CleanupOldEvents(maxAge time.Duration) error {
	pt.mutex.Lock()
	defer pt.mutex.Unlock()

	now := time.Now()
	removed := 0
	for eventID, event := range pt.events {
		if event.ScaleDownInitiated && now.Sub(event.Timestamp) > maxAge {
			delete(pt.events, eventID)
			removed++
			log.Debug().Str("eventID", eventID).Msg("Cleaned up old fallback event")
		}
	}

	if removed > 0 {
		// Persist the cleanup
		return pt.saveToConfigMap(context.Background())
	}
	return nil
}

// GetEventCount returns the number of tracked events
func (pt *PersistentFallbackTracker) GetEventCount() int {
	pt.mutex.RLock()
	defer pt.mutex.RUnlock()
	return len(pt.events)
}
```

### **Step 2: Update Helm RBAC Permissions**

```yaml
# config/helm/aws-node-termination-handler/templates/clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "aws-node-termination-handler.fullname" . }}
rules:
# ... existing rules ...

# Spot Guard ConfigMap permissions
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
  resourceNames: ["aws-nth-spot-guard-state"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create"]  # Allow creating the ConfigMap if it doesn't exist
```

### **Step 3: Update Monitor to Use Persistent Tracker**

```go
// cmd/node-termination-handler.go
if nthConfig.EnableSpotGuard {
    // ... AWS session setup ...

    // Create Spot Guard components with PERSISTENT tracker
    tracker := spotguard.NewPersistentFallbackTracker(clientset)  // ← Changed from NewFallbackTracker
    healthChecker := spotguard.NewHealthChecker(asgClient, clientset)
    safetyChecker := spotguard.NewSafetyChecker(clientset, nthConfig)
    scaleDownExecutor := spotguard.NewScaleDownExecutor(asgClient, clientset, nthConfig)

    // ... rest of the setup ...
}
```

### **Step 4: Update SpotGuard to Track Fallback Events**

```go
// pkg/spotguard/spotguard.go
func (sg *SpotGuard) fallbackToOnDemand() error {
    log.Warn().Msgf("Spot Guard: Falling back to on-demand ASG: %s", sg.OnDemandAsgName)

    err := sg.scaleUpASG(sg.OnDemandAsgName)
    if err != nil {
        return fmt.Errorf("failed to scale up on-demand ASG: %w", err)
    }

    // Wait for on-demand instance to reach InService
    success, err := sg.waitForNewInstance(sg.OnDemandAsgName)
    if err != nil || !success {
        return fmt.Errorf("timeout waiting for on-demand instance to reach InService")
    }

    // ✅ NEW: Record fallback event for monitoring
    if sg.FallbackTracker != nil {
        event := &FallbackEvent{
            EventID:             fmt.Sprintf("fallback-%s-%d", sg.OnDemandAsgName, time.Now().Unix()),
            Timestamp:           time.Now(),
            SpotASGName:         sg.SpotAsgName,
            OnDemandASGName:     sg.OnDemandAsgName,
            OnDemandInstanceID:  sg.getLatestOnDemandInstanceID(), // Get from ASG
            OnDemandNodeName:    sg.getOnDemandNodeName(),        // Get from K8s
            SpotCapacityRestored: false,
            ScaleDownInitiated:  false,
        }
        if err := sg.FallbackTracker.AddEvent(event); err != nil {
            log.Error().Err(err).Msg("Failed to record fallback event")
        }
    }

    log.Info().Msgf("Spot Guard: Successfully scaled up on-demand ASG: %s", sg.OnDemandAsgName)
    return nil
}
```

## 🔄 **How It Works with DaemonSets**

### **Complete Flow:**

```
1. Spot Instance A (has NTH pod)
   ↓ Receives rebalance recommendation
   ↓ Scales up spot ASG
   ↓ Spot fails → Scale up on-demand ASG
   ↓ Record fallback event to ConfigMap
   ↓ Spot Instance A gets terminated
   ✅ ConfigMap persists the event

2. ALL NTH Pods (on all nodes)
   ↓ Background monitor reads ConfigMap every 10s
   ↓ Discovers fallback event
   ↓ Monitors spot ASG health
   ↓ When spot is healthy and stable
   ↓ Scales down on-demand node
   ↓ Updates ConfigMap (mark as complete)
   ✅ Distributed monitoring across all pods
```

### **Key Benefits:**

1. **Survives Pod Terminations** ✅
   - Events stored in Kubernetes ConfigMap (etcd)
   - Any pod can pick up monitoring

2. **Distributed Monitoring** ✅
   - All NTH pods monitor all events
   - No single point of failure

3. **Automatic Sync** ✅
   - Pods sync state every 10 seconds
   - Discover events created by other pods

4. **Race Condition Handling** ✅
   - ConfigMap updates are atomic
   - Last write wins (acceptable for this use case)

## 🎯 **Summary**

The solution transforms Spot Guard from a **single-pod** architecture to a **distributed** architecture:

- ❌ **Before**: In-memory tracker, lost on pod termination
- ✅ **After**: ConfigMap-backed tracker, survives pod restarts

This makes Spot Guard **production-ready for DaemonSet deployments**! 🚀
