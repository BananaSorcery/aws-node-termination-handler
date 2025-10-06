# On-Demand Node Scale-Down Strategy for Spot Guard

## Overview
This document outlines the strategy for automatically scaling down on-demand instances when spot capacity becomes available, minimizing cost while maintaining cluster capacity.

## Problem Statement
When spot capacity is unavailable, we scale up on-demand instances as fallback. However, we need to automatically scale down these on-demand instances when:
1. Spot instances successfully launch and become healthy
2. Cluster has sufficient capacity
3. Workloads can safely migrate from on-demand to spot instances

## Proposed Solution

### Approach 1: Time-Based with Health Check (Your Initial Idea - Simplified)
**Wait 30 minutes, then check if spot capacity is healthy**

**Pros:**
- Simple to implement
- Predictable behavior
- Low complexity

**Cons:**
- Fixed delay regardless of how quickly spot becomes available
- May waste money if spot is available after 5 minutes but we wait 30
- Not reactive to actual cluster state

### Approach 2: Continuous Health Monitoring (Recommended)
**Continuously monitor spot ASG health and scale down on-demand when conditions are met**

**Criteria for scaling down on-demand:**
1. **Spot ASG is healthy**: `InService instances >= Desired capacity`
2. **Minimum wait time passed**: At least 5-10 minutes since on-demand scale-up (allows pods to stabilize)
3. **Workload safety**: Ensure pods on on-demand node can be safely evicted
4. **Cluster capacity check**: Total cluster capacity meets minimum requirements

## Detailed Implementation Strategy

### 1. Data Structure to Track Fallback Events

```go
type FallbackEvent struct {
    EventID              string
    Timestamp            time.Time
    SpotASGName          string
    OnDemandASGName      string
    OnDemandInstanceID   string
    OnDemandNodeName     string
    SpotCapacityRestored bool
    ScaleDownInitiated   bool
    MinimumWaitDuration  time.Duration // Default: 10 minutes
}

type FallbackTracker struct {
    events map[string]*FallbackEvent
    mutex  sync.RWMutex
}
```

### 2. Enhanced Flow

```
┌─────────────────────────────────────────────────────────────┐
│ Step 1: Rebalance Recommendation Received                   │
│ - Spot instance needs replacement                           │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│ Step 2: Attempt Spot Scale-Up                               │
│ - Increase spot ASG desired capacity by +1                  │
│ - Wait up to 2 minutes for instance launch                  │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
         ┌───────────────┐
         │ Spot Success? │
         └───────┬───────┘
                 │
    ┌────────────┴────────────┐
    │ YES                     │ NO
    ▼                         ▼
┌─────────────┐      ┌────────────────────────────────┐
│ Step 3a:    │      │ Step 3b: Fallback to On-Demand│
│ Taint Spot  │      │ - Scale on-demand ASG by +1    │
│ Instance    │      │ - Record FallbackEvent         │
└─────────────┘      │ - Start monitoring timer       │
                     └────────────┬───────────────────┘
                                  │
                                  ▼
                     ┌────────────────────────────────┐
                     │ Step 4: Continuous Monitoring  │
                     │ - Check every 30 seconds       │
                     │ - Monitor spot ASG health      │
                     │ - Check minimum wait time      │
                     └────────────┬───────────────────┘
                                  │
                                  ▼
                     ┌────────────────────────────────┐
                     │ Step 5: Scale-Down Conditions  │
                     │ ✓ Min wait time passed (10m)   │
                     │ ✓ Spot InService >= Desired    │
                     │ ✓ On-demand node drainable     │
                     │ ✓ Cluster has buffer capacity  │
                     └────────────┬───────────────────┘
                                  │
                                  ▼
                     ┌────────────────────────────────┐
                     │ Step 6: Taint & Drain On-Demand│
                     │ - Apply NoSchedule taint       │
                     │ - Drain pods gracefully        │
                     │ - Scale down on-demand ASG -1  │
                     └────────────────────────────────┘
```

### 3. Spot Health Check Criteria

#### 3.1 Primary Check: ASG Instance Health
```go
func isSpotASGHealthy(asgName string) (bool, error) {
    asg, err := describeAutoScalingGroup(asgName)
    if err != nil {
        return false, err
    }
    
    // Count InService instances
    inServiceCount := 0
    for _, instance := range asg.Instances {
        if *instance.LifecycleState == "InService" && 
           *instance.HealthStatus == "Healthy" {
            inServiceCount++
        }
    }
    
    // Check if spot capacity meets desired
    return inServiceCount >= int(*asg.DesiredCapacity), nil
}
```

#### 3.2 Secondary Check: Kubernetes Node Readiness
```go
func areSpotNodesReady(spotASGName string) (bool, error) {
    // Get all nodes in the spot ASG
    nodes, err := getNodesInASG(spotASGName)
    if err != nil {
        return false, err
    }
    
    // Check each node is Ready and not cordoned
    for _, node := range nodes {
        if !isNodeReady(node) || isNodeCordoned(node) {
            return false, nil
        }
    }
    
    return true, nil
}
```

#### 3.3 Combined Health Check
```go
func isSpotCapacityRestored(spotASGName string, minHealthyDuration time.Duration) (bool, error) {
    // 1. Check ASG health
    asgHealthy, err := isSpotASGHealthy(spotASGName)
    if err != nil || !asgHealthy {
        return false, err
    }
    
    // 2. Check Kubernetes node readiness
    nodesReady, err := areSpotNodesReady(spotASGName)
    if err != nil || !nodesReady {
        return false, err
    }
    
    // 3. Check if capacity has been stable for minimum duration (e.g., 2 minutes)
    // This prevents premature scale-down if spot is flapping
    isStable, err := isCapacityStable(spotASGName, minHealthyDuration)
    if err != nil || !isStable {
        return false, err
    }
    
    return true, nil
}
```

### 4. On-Demand Scale-Down Safety Checks

Before scaling down on-demand, ensure:

#### 4.1 Minimum Wait Time
```go
func canScaleDownOnDemand(fallbackEvent *FallbackEvent) (bool, string) {
    // Must wait at least 10 minutes since on-demand scale-up
    elapsed := time.Since(fallbackEvent.Timestamp)
    if elapsed < fallbackEvent.MinimumWaitDuration {
        return false, fmt.Sprintf("minimum wait time not met: %v remaining", 
            fallbackEvent.MinimumWaitDuration - elapsed)
    }
    
    return true, ""
}
```

#### 4.2 Pod Safety Check
```go
func canSafelyDrainOnDemandNode(nodeName string) (bool, string) {
    // Get all pods on the node
    pods, err := getPodsOnNode(nodeName)
    if err != nil {
        return false, fmt.Sprintf("failed to get pods: %v", err)
    }
    
    // Check for critical pods that cannot be evicted
    for _, pod := range pods {
        // Skip DaemonSets
        if isDaemonSetPod(pod) {
            continue
        }
        
        // Check if pod has PodDisruptionBudget
        if hasPDB, _ := checkPodDisruptionBudget(pod); hasPDB {
            // Verify draining won't violate PDB
            if violatesPDB, _ := wouldViolatePDB(pod); violatesPDB {
                return false, "draining would violate PodDisruptionBudget"
            }
        }
        
        // Check if pod can be scheduled on other nodes
        canSchedule, err := canPodScheduleElsewhere(pod)
        if err != nil || !canSchedule {
            return false, "pod cannot be scheduled elsewhere"
        }
    }
    
    return true, ""
}
```

#### 4.3 Cluster Capacity Buffer Check
```go
func hasClusterCapacityBuffer() (bool, string) {
    // Ensure cluster has at least 20% buffer capacity after scale-down
    totalCapacity, usedCapacity, err := getClusterCapacity()
    if err != nil {
        return false, fmt.Sprintf("failed to get cluster capacity: %v", err)
    }
    
    // Calculate if removing on-demand node leaves enough capacity
    onDemandNodeCapacity := getNodeCapacity(onDemandNodeName)
    remainingCapacity := totalCapacity - onDemandNodeCapacity
    utilizationAfterScaleDown := float64(usedCapacity) / float64(remainingCapacity)
    
    if utilizationAfterScaleDown > 0.80 { // 80% threshold
        return false, "cluster would be over 80% utilized after scale-down"
    }
    
    return true, ""
}
```

### 5. Complete Scale-Down Logic

```go
func monitorAndScaleDownOnDemand(ctx context.Context, fallbackTracker *FallbackTracker) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            fallbackTracker.mutex.RLock()
            events := fallbackTracker.getActiveEvents()
            fallbackTracker.mutex.RUnlock()
            
            for _, event := range events {
                // Skip if already processed
                if event.ScaleDownInitiated {
                    continue
                }
                
                // 1. Check minimum wait time
                if canScale, reason := canScaleDownOnDemand(event); !canScale {
                    log.Debug().Str("eventID", event.EventID).
                        Str("reason", reason).
                        Msg("Cannot scale down on-demand yet")
                    continue
                }
                
                // 2. Check spot capacity is restored and stable
                spotHealthy, err := isSpotCapacityRestored(event.SpotASGName, 2*time.Minute)
                if err != nil || !spotHealthy {
                    log.Debug().Str("eventID", event.EventID).
                        Bool("spotHealthy", spotHealthy).
                        Msg("Spot capacity not yet restored")
                    continue
                }
                
                // 3. Check if on-demand node can be safely drained
                if canDrain, reason := canSafelyDrainOnDemandNode(event.OnDemandNodeName); !canDrain {
                    log.Warn().Str("eventID", event.EventID).
                        Str("reason", reason).
                        Msg("Cannot safely drain on-demand node")
                    continue
                }
                
                // 4. Check cluster capacity buffer
                if hasBuffer, reason := hasClusterCapacityBuffer(); !hasBuffer {
                    log.Warn().Str("eventID", event.EventID).
                        Str("reason", reason).
                        Msg("Insufficient cluster capacity buffer")
                    continue
                }
                
                // 5. All checks passed - initiate scale-down
                log.Info().Str("eventID", event.EventID).
                    Str("onDemandNode", event.OnDemandNodeName).
                    Msg("Initiating on-demand node scale-down")
                
                if err := scaleDownOnDemandNode(event); err != nil {
                    log.Error().Err(err).
                        Str("eventID", event.EventID).
                        Msg("Failed to scale down on-demand node")
                    continue
                }
                
                // Mark as processed
                fallbackTracker.mutex.Lock()
                event.ScaleDownInitiated = true
                event.SpotCapacityRestored = true
                fallbackTracker.mutex.Unlock()
                
                log.Info().Str("eventID", event.EventID).
                    Str("onDemandNode", event.OnDemandNodeName).
                    Msg("Successfully scaled down on-demand node")
            }
        }
    }
}
```

### 6. Scale-Down Execution

```go
func scaleDownOnDemandNode(event *FallbackEvent) error {
    nodeName := event.OnDemandNodeName
    
    // Step 1: Taint the on-demand node
    if err := taintNode(nodeName, "scale-down-pending", "true", "NoSchedule"); err != nil {
        return fmt.Errorf("failed to taint node: %w", err)
    }
    log.Info().Str("node", nodeName).Msg("Tainted on-demand node")
    
    // Step 2: Cordon the node
    if err := cordonNode(nodeName); err != nil {
        return fmt.Errorf("failed to cordon node: %w", err)
    }
    log.Info().Str("node", nodeName).Msg("Cordoned on-demand node")
    
    // Step 3: Drain the node (gracefully evict pods)
    if err := drainNode(nodeName, 300*time.Second); err != nil {
        return fmt.Errorf("failed to drain node: %w", err)
    }
    log.Info().Str("node", nodeName).Msg("Drained on-demand node")
    
    // Step 4: Wait for pods to be rescheduled and running
    if err := waitForPodsRescheduled(nodeName, 120*time.Second); err != nil {
        return fmt.Errorf("failed to verify pod rescheduling: %w", err)
    }
    log.Info().Str("node", nodeName).Msg("All pods successfully rescheduled")
    
    // Step 5: Scale down the on-demand ASG
    if err := decreaseASGDesiredCapacity(event.OnDemandASGName, 1); err != nil {
        return fmt.Errorf("failed to scale down on-demand ASG: %w", err)
    }
    log.Info().Str("asg", event.OnDemandASGName).Msg("Scaled down on-demand ASG")
    
    // Step 6: Emit metrics and events
    emitMetric("ondemand_scaledown_success", 1, map[string]string{
        "asg": event.OnDemandASGName,
        "reason": "spot_capacity_restored",
    })
    
    return nil
}
```

## Configuration Parameters

### Recommended Values

```yaml
# Minimum wait time before considering on-demand scale-down
minimumOnDemandWaitTime: 10m  # Your initial idea was 30m, but 10m is more cost-effective

# How often to check for scale-down opportunities
scaleDownCheckInterval: 30s

# How long spot capacity must be stable before trusting it
spotStabilityDuration: 2m

# Maximum cluster utilization before scale-down
maxClusterUtilizationForScaleDown: 80%

# Grace period for pod eviction during drain
podEvictionGracePeriod: 300s

# Timeout for waiting for pods to reschedule
podRescheduleTimeout: 120s
```

## Comparison: 30 Minutes vs Continuous Monitoring

### Scenario: Spot becomes available after 5 minutes

| Approach | Cost Impact | Complexity | Responsiveness |
|----------|-------------|------------|----------------|
| **30-min fixed wait** | Wastes 25 min of on-demand cost | Low | Poor |
| **Continuous monitoring** | Saves 25 min of on-demand cost | Medium | Excellent |

**Cost Calculation Example:**
- On-demand instance: $0.10/hour ($0.0017/min)
- 25 minutes saved = $0.0425 per event
- If 100 rebalance events/month = **$4.25/month savings per instance type**
- Across multiple instance types/AZs = **significant savings**

## Recommended Implementation

### Option A: Conservative (Safer but Higher Cost)
- Minimum wait: 15 minutes
- Spot stability: 5 minutes
- Check interval: 60 seconds
- Max utilization: 70%

### Option B: Aggressive (More Savings)
- Minimum wait: 5 minutes
- Spot stability: 2 minutes
- Check interval: 30 seconds
- Max utilization: 80%

### Option C: Balanced (Recommended)
- Minimum wait: 10 minutes
- Spot stability: 3 minutes
- Check interval: 30 seconds
- Max utilization: 75%

## Edge Cases to Handle

1. **Multiple Concurrent Fallbacks**: Track multiple on-demand instances
2. **Spot Flapping**: Don't scale down if spot has been unstable
3. **Cluster-Wide Capacity Issues**: Don't scale down if cluster is under pressure
4. **Critical Workloads**: Respect PDBs and non-evictable pods
5. **Scale-Down During High Load**: Pause scale-downs during high cluster utilization

## Monitoring & Metrics

```go
// Metrics to track
metrics := []Metric{
    "fallback_events_total",
    "ondemand_runtime_duration_seconds",
    "scaledown_success_total",
    "scaledown_failure_total",
    "cost_savings_dollars_estimated",
    "spot_stability_duration_seconds",
}
```

## Implementation in Your Custom Fork

Add this logic in the rebalance handling flow:
1. **When fallback to on-demand occurs**: Record FallbackEvent
2. **Start background monitor**: Launch `monitorAndScaleDownOnDemand` goroutine
3. **On scale-down success**: Clean up tracking data
4. **On errors**: Log and retry with exponential backoff

This approach gives you maximum cost savings while maintaining safety and reliability!

