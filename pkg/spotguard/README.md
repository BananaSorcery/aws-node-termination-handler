# Spot Guard - On-Demand Auto Scale-Down

## Overview

Spot Guard is a cost-optimization feature that automatically scales down on-demand instances when spot capacity becomes available. When a rebalance recommendation triggers a fallback to on-demand instances due to spot capacity unavailability, Spot Guard monitors the spot capacity and automatically scales down the on-demand instances once spot is healthy again.

## Features

- ✅ **Automatic Monitoring**: Continuously monitors spot ASG health
- ✅ **Safety Checks**: Ensures pods can be safely rescheduled before draining
- ✅ **Cluster Buffer Protection**: Prevents overloading the cluster
- ✅ **PDB Compliance**: Respects PodDisruptionBudgets
- ✅ **Configurable Wait Times**: Adjustable minimum wait and stability durations
- ✅ **Concurrent Processing**: Non-blocking background monitoring
- ✅ **Multiple Event Tracking**: Can handle multiple fallback events simultaneously

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Rebalance Event                                         │
│ ↓                                                        │
│ Try Spot Scale-Up → Failed? → Scale Up On-Demand       │
│                                ↓                         │
│                       Record Fallback Event             │
└─────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────┐
│ Background Monitor (Goroutine)                          │
│ ├─ Every 30s: Check all fallback events                │
│ ├─ Wait 10+ minutes since on-demand scale-up           │
│ ├─ Check spot ASG health (InService >= Desired)        │
│ ├─ Check K8s nodes ready and not cordoned              │
│ ├─ Check spot stability (healthy for 2+ minutes)       │
│ ├─ Check pod safety (can reschedule)                   │
│ ├─ Check cluster buffer (< 75% utilization)            │
│ └─ All OK? → Taint → Drain → Scale Down On-Demand     │
└─────────────────────────────────────────────────────────┘
```

## Components

### 1. `FallbackTracker`
Manages tracking of fallback events with thread-safe operations.

### 2. `HealthChecker`
Performs health checks on spot ASG and Kubernetes nodes:
- ASG health: `InService instances >= Desired capacity`
- K8s readiness: All nodes Ready and not cordoned
- Stability: Capacity stable for configured duration

### 3. `SafetyChecker`
Validates safety conditions before scale-down:
- Minimum wait time elapsed
- Pods can be safely evicted
- PodDisruptionBudgets respected
- Cluster has capacity buffer

### 4. `ScaleDownExecutor`
Executes the scale-down operation:
- Taints the on-demand node
- Cordons the node
- Drains pods gracefully
- Scales down the ASG

### 5. `Monitor`
Orchestrates the entire process in a background goroutine.

## Configuration

### Default Configuration (Recommended)

```go
config := spotguard.Config{
    Enabled:                true,
    SpotASGName:            "my-spot-asg",
    OnDemandASGName:        "my-ondemand-asg",
    MinimumWaitDuration:    10 * time.Minute,  // Wait at least 10 min
    CheckInterval:          30 * time.Second,   // Check every 30s
    SpotStabilityDuration:  2 * time.Minute,    // Spot must be stable for 2 min
    MaxClusterUtilization:  75.0,               // Max 75% utilization
    PodEvictionTimeout:     5 * time.Minute,    // 5 min to evict pods
}
```

### Conservative Configuration (Production)

```go
config := spotguard.ConservativeConfig()
config.SpotASGName = "my-spot-asg"
config.OnDemandASGName = "my-ondemand-asg"
// MinimumWaitDuration: 15 minutes
// MaxClusterUtilization: 70%
```

### Aggressive Configuration (Cost Optimized)

```go
config := spotguard.AggressiveConfig()
config.SpotASGName = "my-spot-asg"
config.OnDemandASGName = "my-ondemand-asg"
// MinimumWaitDuration: 5 minutes
// MaxClusterUtilization: 80%
```

## Integration

### Step 1: Initialize in main()

```go
// In cmd/node-termination-handler.go

import "github.com/aws/aws-node-termination-handler/pkg/spotguard"

func main() {
    // ... existing initialization code ...
    
    // Configure spot guard
    spotGuardConfig := spotguard.Config{
        Enabled:                true,
        SpotASGName:            "my-spot-asg",
        OnDemandASGName:        "my-ondemand-fallback-asg",
        MinimumWaitDuration:    10 * time.Minute,
        CheckInterval:          30 * time.Second,
        SpotStabilityDuration:  2 * time.Minute,
        MaxClusterUtilization:  75.0,
        PodEvictionTimeout:     5 * time.Minute,
    }
    
    // Initialize spot guard
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    monitor, tracker, err := spotguard.InitializeSpotGuard(
        ctx,
        spotGuardConfig,
        asgClient,
        clientset,
        *node,
    )
    if err != nil {
        log.Fatal().Err(err).Msg("Failed to initialize spot guard")
    }
    
    // Continue with existing NTH logic...
}
```

### Step 2: Record Fallback Events

```go
// In your rebalance handler

func handleRebalanceRecommendation(rebalanceEvent *RebalanceEvent, tracker *spotguard.FallbackTracker) error {
    // Try to scale up spot
    err := scaleUpSpotASG(spotASGName, 1)
    if err != nil {
        log.Warn().Err(err).Msg("Spot capacity unavailable, falling back to on-demand")
        
        // Scale up on-demand as fallback
        instanceID, nodeName, err := scaleUpOnDemandASG(onDemandASGName, 1)
        if err != nil {
            return fmt.Errorf("failed to scale up on-demand: %w", err)
        }
        
        // Record the fallback event
        // Spot guard will automatically handle scale-down when safe
        spotguard.RecordFallbackEvent(tracker, spotGuardConfig, instanceID, nodeName)
        
        log.Info().
            Str("instanceID", instanceID).
            Str("nodeName", nodeName).
            Msg("Scaled up on-demand, spot guard will monitor for scale-down")
    }
    
    // Taint the original node receiving rebalance
    return taintNode(rebalanceEvent.NodeName)
}
```

## How It Works

### Timeline Example

```
T+0:00  Rebalance recommendation received
T+0:05  Spot scale-up fails (no capacity)
T+0:10  On-demand scaled up successfully
T+0:10  Fallback event recorded, monitoring starts
        
T+1:00  Monitor checks: Wait time not met (need 10 min)
T+2:00  Monitor checks: Wait time not met
...
T+10:00 Monitor checks: Wait time met ✓
        Spot ASG unhealthy (only 2/3 instances)
        
T+12:00 Monitor checks: Spot healthy (3/3 instances) ✓
        Start stability timer
        
T+12:30 Monitor checks: Spot stable for 30s (need 2 min)
T+13:00 Monitor checks: Spot stable for 1 min
T+14:00 Monitor checks: Spot stable for 2 min ✓
        Pod safety check ✓
        Cluster buffer check ✓
        
T+14:00 Execute scale-down:
        - Taint on-demand node
        - Cordon node
        - Drain pods (5 min timeout)
        - Wait for pods to reschedule
        - Scale down on-demand ASG
        
T+16:00 On-demand instance terminated
        Cost savings achieved! 💰
```

### Total On-Demand Runtime

- **Without Spot Guard**: On-demand runs indefinitely (expensive!)
- **With Spot Guard**: ~16 minutes of on-demand usage
- **Savings**: Significant cost reduction

## Safety Mechanisms

### 1. Minimum Wait Time
Prevents premature scale-down before spot capacity stabilizes.

### 2. Spot Stability Check
Ensures spot capacity has been healthy for a minimum duration (prevents flapping).

### 3. Pod Safety Check
- Verifies pods can be rescheduled elsewhere
- Respects PodDisruptionBudgets
- Checks resource availability on other nodes
- Handles stateful workloads correctly

### 4. Cluster Buffer Check
Ensures cluster has sufficient spare capacity (default 25%) to handle:
- Traffic spikes
- Pod rescheduling
- Multiple spot interruptions
- Autoscaler lag time

## Monitoring & Metrics

### Log Messages

```
INFO  Tracking new fallback event eventID=fallback-i-1234-1234567890
DEBUG Checking for scale-down opportunities activeEvents=1
DEBUG Cannot scale down yet reason="minimum wait time not met: 5m30s remaining"
INFO  Spot capacity became healthy, starting stability timer
INFO  Spot capacity is fully restored and stable
INFO  Executing on-demand scale-down
INFO  Successfully completed on-demand scale-down totalDuration=16m
```

### Metrics (Placeholder)

Integrate with your Prometheus metrics:
- `spotguard_fallback_events_total`
- `spotguard_ondemand_runtime_seconds`
- `spotguard_scaledown_success_total`
- `spotguard_scaledown_failure_total`

## Troubleshooting

### On-Demand Not Scaling Down?

Check logs for the reason:

```bash
# Check spot guard logs
kubectl logs -n kube-system -l app=aws-node-termination-handler | grep spotguard
```

Common reasons:
- ❌ Minimum wait time not met
- ❌ Spot capacity not stable
- ❌ PodDisruptionBudget would be violated
- ❌ Cluster utilization too high
- ❌ Pods cannot fit on other nodes

### Adjust Configuration

If on-demand is taking too long to scale down, consider:
- Reduce `MinimumWaitDuration` (careful!)
- Reduce `SpotStabilityDuration`
- Increase `MaxClusterUtilization` (risky)

If on-demand scales down too quickly:
- Increase `MinimumWaitDuration`
- Increase `SpotStabilityDuration`
- Decrease `MaxClusterUtilization`

## Testing

### Unit Tests

```bash
cd pkg/spotguard
go test -v
```

### Integration Tests

```bash
# Test with a real cluster (requires AWS credentials and K8s cluster)
go test -v -tags=integration
```

### Manual Testing

1. Deploy with spot guard enabled
2. Trigger a rebalance event
3. Cause spot capacity failure (e.g., terminate spot instance)
4. Verify on-demand scales up
5. Wait for configured duration
6. Verify on-demand scales down automatically

## Best Practices

1. **Start Conservative**: Use conservative config in production first
2. **Monitor Metrics**: Track runtime and cost savings
3. **Adjust Gradually**: Fine-tune based on your workload patterns
4. **Test Thoroughly**: Test in staging before production
5. **Set Alerts**: Alert on repeated scale-down failures

## Cost Savings Estimate

**Example Scenario:**
- Instance type: `m5.xlarge` on-demand ($0.192/hour)
- Spot unavailability: 10 times per month
- Average on-demand runtime: 30 minutes without spot guard

**Without Spot Guard:**
- 10 events × 30 min = 300 min/month = 5 hours/month
- Cost: 5 hours × $0.192 = **$0.96/month per instance**

**With Spot Guard:**
- 10 events × 15 min = 150 min/month = 2.5 hours/month
- Cost: 2.5 hours × $0.192 = **$0.48/month per instance**
- **Savings: $0.48/month per instance (50% reduction)**

**At Scale (100 instances):**
- **Savings: $48/month = $576/year**

## License

Apache 2.0 - See LICENSE file

