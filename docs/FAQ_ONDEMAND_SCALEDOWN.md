# FAQ: On-Demand Scale-Down Strategy

## Q1: Does Continuous Monitoring Need Concurrent Programs?

**Short Answer**: YES! It absolutely needs to run concurrently (in a goroutine).

### Why Concurrent?

The continuous monitoring must run **independently** in the background, otherwise it will block the main flow of handling new rebalance events.

### Visual Explanation

```
WITHOUT Concurrency (BAD ❌):
┌─────────────────────────────────────────────────────────┐
│ Main Thread                                             │
├─────────────────────────────────────────────────────────┤
│ 1. Detect rebalance                                     │
│ 2. Try spot scale-up (fails)                            │
│ 3. Scale up on-demand                                   │
│ 4. Start monitoring... [BLOCKING]                       │
│    - Wait 10 min... [STUCK HERE]                        │
│    - Check every 30s... [STUCK HERE]                    │
│    - Cannot handle new rebalance events! ❌             │
│                                                          │
│ Meanwhile: New rebalance event arrives → IGNORED! ❌    │
└─────────────────────────────────────────────────────────┘

WITH Concurrency (GOOD ✅):
┌─────────────────────────────────────────────────────────┐
│ Main Thread                   │ Background Goroutine    │
├───────────────────────────────┼─────────────────────────┤
│ 1. Detect rebalance           │                         │
│ 2. Try spot (fails)            │                         │
│ 3. Scale up on-demand         │                         │
│ 4. Record fallback event      │                         │
│ 5. Continue monitoring IMDS ✅│→ Start monitoring      │
│                               │   - Wait & check spot   │
│ 6. NEW rebalance arrives ✅   │   - Run independently   │
│ 7. Handle it immediately ✅   │   - Scale down when OK  │
│ 8. Continue normal ops ✅     │                         │
└───────────────────────────────┴─────────────────────────┘
```

### Code Implementation

```go
// In your main initialization
func main() {
    // ... initialization code ...
    
    // Create fallback tracker (shared state)
    fallbackTracker := NewFallbackTracker()
    
    // Start the background monitor ONCE at startup
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // This runs in its own goroutine and monitors ALL fallback events
    go monitorAndScaleDownOnDemand(ctx, fallbackTracker)
    
    // Main loop continues to handle rebalance events
    for {
        // Handle rebalance events
        if rebalanceDetected {
            handleRebalanceEvent(fallbackTracker) // Non-blocking
        }
    }
}

// When fallback happens
func handleRebalanceEvent(tracker *FallbackTracker) {
    // Try spot scale-up
    if err := scaleUpSpot(); err != nil {
        // Fallback to on-demand
        scaleUpOnDemand()
        
        // Record the event (non-blocking)
        event := &FallbackEvent{
            EventID:     generateEventID(),
            Timestamp:   time.Now(),
            SpotASGName: "my-spot-asg",
            OnDemandASGName: "my-ondemand-asg",
            // ... other fields
        }
        
        // Add to tracker (the background goroutine will pick it up)
        tracker.AddEvent(event) // ← Non-blocking, just adds to map
        
        // Main thread continues immediately ✅
    }
}

// Background goroutine (runs continuously)
func monitorAndScaleDownOnDemand(ctx context.Context, tracker *FallbackTracker) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Check ALL tracked fallback events
            events := tracker.GetActiveEvents()
            
            for _, event := range events {
                // Process each event independently
                if shouldScaleDown(event) {
                    scaleDownOnDemandNode(event)
                }
            }
        }
    }
}
```

### Key Points

1. **One Background Goroutine**: Starts once at application startup
2. **Monitors Multiple Events**: Can track multiple on-demand instances simultaneously
3. **Non-Blocking Main Flow**: Main thread continues to handle new rebalance events
4. **Shared State**: Uses mutex-protected `FallbackTracker` for safe concurrent access

---

## Q2: Why Check Pod Safety Before Evicting?

**Short Answer**: To prevent service disruptions and ensure workloads can actually run somewhere else!

### Real-World Problems Without Pod Safety Checks

#### Problem 1: Stateful Applications

```
Scenario:
- You have a database pod on the on-demand node
- The pod has a 50GB persistent volume attached
- You drain the node without checking

What happens:
1. Pod gets evicted from on-demand node
2. Kubernetes tries to reschedule it to spot node
3. Volume cannot attach because it's in a different AZ ❌
4. Pod stuck in Pending state
5. Database is DOWN ❌
6. Your application breaks ❌
```

#### Problem 2: PodDisruptionBudget Violations

```
Scenario:
- You have a Redis cluster with 3 replicas
- PodDisruptionBudget (PDB) says: "Always keep at least 2 replicas running"
- 2 replicas are on spot nodes, 1 replica is on on-demand node
- You drain on-demand without checking PDB

What happens:
1. You evict the Redis pod from on-demand
2. During rescheduling, there are only 2 replicas running
3. At the same time, a spot rebalance happens
4. Kubernetes tries to evict one of the spot replicas
5. PDB blocks it because it would violate "at least 2" rule
6. Now you have deadlock: on-demand is drained but spot can't drain ❌
```

#### Problem 3: Resource Constraints

```
Scenario:
- On-demand node has a pod that requires 8 CPU cores
- All spot nodes only have 4 CPU cores available
- You drain without checking

What happens:
1. Pod gets evicted
2. Kubernetes cannot reschedule it (no node has 8 cores)
3. Pod stuck in Pending state
4. Workload is DOWN ❌
```

### What Pod Safety Checks Do

```go
func canSafelyDrainOnDemandNode(nodeName string) (bool, string) {
    pods, _ := getPodsOnNode(nodeName)
    
    for _, pod := range pods {
        // Skip system pods (DaemonSets)
        if isDaemonSetPod(pod) {
            continue // These are OK, they run on every node
        }
        
        // ❌ Check 1: Does the pod have PodDisruptionBudget?
        if hasPDB, _ := checkPodDisruptionBudget(pod); hasPDB {
            if violatesPDB, _ := wouldViolatePDB(pod); violatesPDB {
                return false, "evicting would violate PDB - other replicas already disrupted"
            }
        }
        
        // ❌ Check 2: Can the pod physically fit on other nodes?
        canSchedule, err := canPodScheduleElsewhere(pod)
        if !canSchedule {
            return false, "no other node has enough resources (CPU/memory/storage)"
        }
        
        // ❌ Check 3: Does the pod have node affinity/anti-affinity rules?
        if hasNodeAffinity, _ := checkNodeAffinity(pod); hasNodeAffinity {
            if cantSatisfyAffinity, _ := wouldBreakAffinity(pod); cantSatisfyAffinity {
                return false, "pod has node affinity rules that cannot be satisfied"
            }
        }
        
        // ❌ Check 4: Local storage issues?
        if hasLocalStorage, _ := usesLocalStorage(pod); hasLocalStorage {
            return false, "pod uses local storage - data would be lost"
        }
    }
    
    return true, "" // ✅ Safe to drain
}
```

### Summary: Pod Safety Prevents

- ❌ Service outages
- ❌ Data loss
- ❌ PDB violations
- ❌ Pods stuck in Pending state
- ❌ Application failures

---

## Q3: What is Cluster Buffer and Why Check It?

**Short Answer**: Cluster buffer is **spare capacity** in your cluster. You check it to avoid overloading the cluster and causing cascading failures.

### What is Cluster Buffer?

Cluster buffer is the percentage of **unused resources** in your cluster:

```
Cluster Buffer = (Total Capacity - Used Capacity) / Total Capacity

Example:
- Total Cluster CPU: 100 cores
- Currently Used: 60 cores
- Buffer: (100 - 60) / 100 = 40%

If buffer < 20% → Cluster is tight on resources!
```

### Why You Need Buffer

#### Real-World Scenario Without Buffer Check

```
Initial State:
┌─────────────────────────────────────────────────────┐
│ Cluster: 100 cores total, 85 cores used (85%)      │
├─────────────────────────────────────────────────────┤
│ Spot Nodes:       60 cores (50 used)               │
│ On-Demand Node:   40 cores (35 used) ← Want to remove
└─────────────────────────────────────────────────────┘

What happens if you scale down on-demand WITHOUT buffer check:

Step 1: You drain on-demand node (40 cores)
┌─────────────────────────────────────────────────────┐
│ Cluster: 60 cores total, 85 cores NEEDED ❌        │
│                                                     │
│ Problem: 85 cores needed > 60 cores available ❌   │
└─────────────────────────────────────────────────────┘

Step 2: Pods from on-demand try to reschedule
- 35 cores worth of pods need new homes
- Spot nodes only have 10 cores free (60 total - 50 used)
- 25 cores worth of pods CANNOT fit ❌

Step 3: Cascading Failure
- 25+ pods stuck in Pending state ❌
- Applications start failing ❌
- Alerts everywhere ❌
- You manually scale up on-demand again 😭
- You just wasted money and caused an outage!
```

#### With Buffer Check (Proper Way)

```go
func hasClusterCapacityBuffer() (bool, string) {
    // Get cluster capacity
    totalCapacity := getTotalClusterCapacity() // e.g., 100 cores
    usedCapacity := getUsedCapacity()           // e.g., 85 cores
    
    // Get on-demand node capacity
    onDemandCapacity := getNodeCapacity(onDemandNode) // e.g., 40 cores
    
    // Calculate what would happen after scale-down
    remainingCapacity := totalCapacity - onDemandCapacity  // 60 cores
    utilizationAfterScaleDown := usedCapacity / remainingCapacity
    
    // utilization = 85 / 60 = 1.41 = 141% ❌
    
    if utilizationAfterScaleDown > 0.80 { // 80% threshold
        return false, "would overload cluster (141% utilization)"
    }
    
    return true, ""
}
```

### Buffer Prevents Cascading Failures

```
Safe Buffer Levels:

┌──────────────────────────────────────────────────────┐
│ Buffer Level │ Status    │ Action                    │
├──────────────┼───────────┼───────────────────────────┤
│ > 40%        │ Healthy   │ ✅ Safe to scale down    │
│ 20-40%       │ OK        │ ✅ Safe with monitoring   │
│ 10-20%       │ Tight     │ ⚠️ Risky to scale down   │
│ < 10%        │ Critical  │ ❌ DO NOT scale down     │
└──────────────────────────────────────────────────────┘
```

### When Buffer Matters Most

1. **Traffic Spikes**: Sudden load increase needs spare capacity
2. **Pod Rescheduling**: Evicted pods need somewhere to go
3. **Spot Interruptions**: When spot fails, need room on remaining nodes
4. **Autoscaling Lag**: Takes time to provision new nodes

### Real Example

```
Scenario: E-commerce site during sale event

Without Buffer Check:
10:00 AM - Scale down on-demand (cluster at 85% capacity)
10:15 AM - Sale starts, traffic spikes 2x
10:16 AM - Cluster overloaded (170% demand)
10:17 AM - Pods crashing, customers can't checkout ❌
10:20 AM - Emergency scale-up (takes 3-5 minutes)
10:25 AM - Finally recovered
→ 9 minutes of degraded service
→ Lost sales
→ Angry customers

With Buffer Check:
10:00 AM - Try to scale down on-demand
10:00 AM - Buffer check: "Cluster would be 85% after scale-down"
10:00 AM - Decision: DON'T scale down, keep buffer ✅
10:15 AM - Sale starts, traffic spikes 2x
10:16 AM - Cluster handles it with existing buffer ✅
10:17 AM - Autoscaler brings more capacity
10:18 AM - Everything smooth ✅
→ No outage
→ Happy customers
→ Small cost for keeping on-demand few more hours
```

### Buffer = Insurance Policy

Think of cluster buffer like:
- **Car following distance**: Need space to brake safely
- **Bank account savings**: Emergency fund for unexpected expenses
- **Fuel reserve**: Don't run your gas tank to empty

The buffer gives you:
- ✅ Room for traffic spikes
- ✅ Safe pod rescheduling
- ✅ Handling multiple spot interruptions
- ✅ Time for autoscaler to react

### Recommended Buffer Thresholds

```yaml
# Conservative (Production with strict SLAs)
minimumBufferPercentage: 30%  # Never drop below 30% free capacity

# Balanced (Most use cases)
minimumBufferPercentage: 20%  # Recommended

# Aggressive (Cost-optimized, can tolerate brief degradation)
minimumBufferPercentage: 15%  # Risky but saves most money
```

---

## Summary

| Question | Short Answer |
|----------|--------------|
| **Concurrent?** | YES! Use goroutine to avoid blocking main flow |
| **Pod Safety?** | Prevents service outages and pods stuck in Pending state |
| **Cluster Buffer?** | Prevents overloading cluster during scale-down |

All three are **safety mechanisms** to ensure your cost-saving strategy doesn't accidentally cause outages! 🛡️

