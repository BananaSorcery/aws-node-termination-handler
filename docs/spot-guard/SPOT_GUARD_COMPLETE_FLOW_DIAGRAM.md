# 🔄 Spot Guard Complete Flow Diagram

## Overview

This diagram shows the complete Spot Guard flow including both **scale-up** (when spot capacity is needed) and **scale-down** (when on-demand nodes can be removed after spot instances are healthy).

## Complete Spot Guard Flow

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           SPOT GUARD COMPLETE FLOW                              │
│                    (Scale-Up + Scale-Down Operations)                           │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  PHASE 1: SPOT INTERRUPTION DETECTION & SCALE-UP                                │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  1. IMDS Rebalance Check (Every 2s)                                             │
│     ↓ Rebalance Recommendation Detected                                         │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  2. Scale Up Spot ASG (+1)                                                      │
│     • Get current desired capacity                                              │
│     • Set desired capacity to current + 1                                       │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  3. Wait for New Instance (Check every 10s)                                     │
│     • Monitor InService instance count                                          │
│     • Check scaling activities for errors                                       │
│     • Timeout after configured duration                                         │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────┐
    │  3.1 SUCCESS     │    │  3.2 FAILURE     │
    │  New instance    │    │  Timeout or      │
    │  is InService    │    │  Capacity Issue  │
    └──────────────────┘    └──────────────────┘
              ↓                       ↓
              |            ┌──────────────────────────┐
              |            │  Fallback: Scale Up      │
              |            │  On-Demand ASG (+1)      │
              |            │  Wait for InService      │
              |            └──────────────────────────┘
              |                       ↓
              └───────────┬───────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  4. Taint Node                                                                  │
│     • Apply RebalanceRecommendationTaint                                        │
│     • Effect: NoSchedule                                                        │
│     • Prevents new pod scheduling                                               │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  5. Drain Node (Existing NTH Logic)                                             │
│     • Evict pods from tainted node                                              │
│     • Respect PodDisruptionBudgets                                              │
│     • Pods migrate to new instances                                             │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  PHASE 2: SPOT GUARD MONITORING & SCALE-DOWN                                    │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  6. Spot Guard Monitor (Background Process)                                     │
│     • Runs continuously every 30s                                               │
│     • Monitors spot ASG health                                                  │
│     • Tracks on-demand fallback events                                          │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  7. Health Check: Is Spot ASG Healthy?                                          │
│     • InService instances == Desired capacity                                   │
│     • No scaling activities in progress                                         │
│     • No recent errors or failures                                              │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────┐
    │  7.1 HEALTHY    │    │  7.2 UNHEALTHY    │
    │  Spot ASG is    │    │  Spot ASG has     │
    │  stable and     │    │  issues or        │
    │  ready          │    │  still scaling    │
    └──────────────────┘    └──────────────────┘
              ↓                       ↓
              |            ┌──────────────────────────┐
              |            │  Continue Monitoring     │
              |            │  Wait for stability      │
              |            └──────────────────────────┘
              |                       ↓
              └───────────┬───────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  8. Stability Check: Has Spot Been Healthy Long Enough?                         │
│     • Spot ASG healthy for minimum duration (e.g., 2 minutes)                   │
│     • No recent scaling activities                                              │
│     • All spot nodes are ready in Kubernetes                                    │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────┐
    │  8.1 STABLE      │    │  8.2 UNSTABLE    │
    │  Spot capacity   │    │  Spot capacity   │
    │  is stable       │    │  still changing  │
    └──────────────────┘    └──────────────────┘
              ↓                       ↓
              |            ┌──────────────────────────┐
              |            │  Continue Monitoring     │
              |            │  Wait for stability      │
              |            └──────────────────────────┘
              |                       ↓
              └───────────┬───────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  9. Safety Check: Can We Safely Drain On-Demand Node?                          │
│     • Pod Safety: All pods can be rescheduled                                  │
│     • PDB Compliance: No PodDisruptionBudget violations                        │
│     • Cluster Buffer: Sufficient cluster capacity                             │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────┐
    │  9.1 SAFE        │    │  9.2 UNSAFE      │
    │  Can safely      │    │  Cannot safely   │
    │  drain node      │    │  drain node      │
    └──────────────────┘    └──────────────────┘
              ↓                       ↓
              |            ┌──────────────────────────┐
              |            │  Continue Monitoring     │
              |            │  Wait for safety         │
              |            └──────────────────────────┘
              |                       ↓
              └───────────┬───────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  10. Scale Down On-Demand Node                                                  │
│      • Taint on-demand node (NoSchedule)                                        │
│      • Cordon on-demand node (unschedulable)                                   │
│      • Drain on-demand node (evict pods)                                         │
│      • Wait for pods to reschedule                                              │
│      • Decrease on-demand ASG desired capacity by 1                            │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  11. Cleanup & Continue Monitoring                                             │
│      • Remove fallback event from tracking                                     │
│      • Continue monitoring for next cycle                                       │
│      • Log successful scale-down operation                                     │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  PHASE 3: CONTINUOUS MONITORING CYCLE                                           │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────┐
│  12. Background Monitoring Loop                                                 │
│      • Check for new spot interruptions                                        │
│      • Monitor spot ASG health                                                 │
│      • Track on-demand fallback events                                         │
│      • Scale down on-demand when spot is stable                                │
│      • Repeat cycle every 30 seconds                                          │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Key Components

### 🔄 **Scale-Up Process (Phase 1)**
1. **Detection**: IMDS rebalance recommendation
2. **Scaling**: Scale up spot ASG (+1)
3. **Fallback**: Scale up on-demand ASG if spot fails
4. **Tainting**: Taint node after successful scaling
5. **Draining**: Drain node using existing NTH logic

### 📊 **Monitoring Process (Phase 2)**
1. **Health Check**: Verify spot ASG is healthy
2. **Stability Check**: Ensure spot capacity is stable
3. **Safety Check**: Verify it's safe to drain on-demand node
4. **Scale Down**: Taint, cordon, drain, and scale down on-demand ASG

### 🔁 **Continuous Cycle (Phase 3)**
1. **Background Monitoring**: Runs every 30 seconds
2. **Event Tracking**: Tracks fallback events
3. **Automatic Scale-Down**: Removes on-demand nodes when spot is stable

## Timing & Intervals

| Component | Interval | Purpose |
|-----------|----------|---------|
| **IMDS Check** | 2 seconds | Detect rebalance recommendations |
| **ASG Status Check** | 10 seconds | Monitor scaling progress |
| **Spot Guard Monitor** | 30 seconds | Background health monitoring |
| **Stability Duration** | 2 minutes | Ensure spot capacity is stable |
| **Cleanup Interval** | 10 minutes | Clean up old events |

## Success Criteria

### ✅ **Scale-Up Success**
- Spot ASG scaled up successfully
- New instance reached InService state
- Node tainted and drained successfully

### ✅ **Scale-Down Success**
- Spot ASG is healthy and stable
- On-demand node can be safely drained
- Cluster has sufficient capacity buffer
- On-demand ASG scaled down successfully

## Error Handling

### ⚠️ **Scale-Up Failures**
- **Spot ASG fails**: Fallback to on-demand ASG
- **On-demand ASG fails**: Continue with existing node
- **Timeout**: Log error and continue

### ⚠️ **Scale-Down Failures**
- **Spot ASG unhealthy**: Skip scale-down
- **Safety check fails**: Wait and retry
- **Drain fails**: Log error and retry

## Benefits

### 💰 **Cost Optimization**
- **Automatic scale-down** of on-demand nodes when spot is available
- **Intelligent fallback** to on-demand only when needed
- **Continuous monitoring** ensures optimal resource usage

### 🛡️ **Reliability**
- **Safety checks** prevent unsafe operations
- **Stability monitoring** ensures spot capacity is ready
- **Graceful degradation** handles failures

### 🔄 **Automation**
- **No manual intervention** required
- **Continuous monitoring** and adjustment
- **Automatic cleanup** of fallback events

## Implementation Status

- ✅ **Scale-Up Process**: Implemented and tested
- ✅ **Monitoring Process**: Implemented and tested
- ✅ **Scale-Down Process**: Implemented and tested
- ✅ **Safety Checks**: Implemented and tested
- ✅ **Error Handling**: Implemented and tested
- ✅ **Continuous Monitoring**: Implemented and tested

## Next Steps

1. **Deploy and Test**: Deploy the complete Spot Guard system
2. **Monitor Performance**: Track cost savings and reliability
3. **Tune Parameters**: Adjust timing and thresholds based on usage
4. **Scale Up**: Deploy to production environments

---

**Status**: ✅ **COMPLETE IMPLEMENTATION READY**

**This diagram shows the full Spot Guard flow including both scale-up and scale-down operations for complete cost optimization!** 🚀
