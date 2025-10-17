# 🔄 Spot Guard Complete Flow Diagram v2.0

## Overview

This diagram shows the **complete enhanced Spot Guard flow** including:
- ✅ **Smart Pre-Scale** with 3-level fallback
- ✅ **CA Protection** for spot nodes
- ✅ **Timestamp-based capacity detection** (no false positives)
- ✅ **Self-monitoring** on on-demand nodes
- ✅ **Comprehensive health checks** (single API call)

---

## 🏗️ Architecture: Two Parallel Systems

```
┌────────────────────────────────────────────────────────────────┐
│                     SPOT GUARD ARCHITECTURE                     │
└────────────────────────────────────────────────────────────────┘

┌─────────────────────────┐         ┌──────────────────────────┐
│   ON SPOT NODES         │         │   ON ON-DEMAND NODES      │
│   (CA Protection)       │         │   (Self-Monitor)          │
└─────────────────────────┘         └──────────────────────────┘
          ↓                                      ↓
┌─────────────────────────┐         ┌──────────────────────────┐
│  CA Protector           │         │  Self-Monitor            │
│  • Starts immediately   │         │  • Starts immediately    │
│  • Runs every 5 min     │         │  • Checks every 30s      │
│  • Protects for 7 min   │         │  • Waits min 2 min       │
│  • Prevents CA scale-   │         │  • Monitors spot health  │
│    down of new spots    │         │  • Scales down on-demand │
└─────────────────────────┘         └──────────────────────────┘

PLUS: Rebalance Monitor (on all spot nodes)
• Detects rebalance recommendations
• Triggers scale-up with fallback
• Creates fallback events for tracking
```

---

## 📋 Complete Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    SPOT GUARD COMPLETE FLOW v2.0                                │
│          (Rebalance → Scale-Up → Protect → Monitor → Scale-Down)                │
└─────────────────────────────────────────────────────────────────────────────────┘

╔═════════════════════════════════════════════════════════════════════════════════╗
║  PHASE 1: REBALANCE DETECTION & SCALE-UP (On Spot Nodes)                        ║
╚═════════════════════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────────────────────────────────────────────┐
│  1. IMDS Rebalance Check (Every 2s on Spot Nodes)                               │
│     ↓ Rebalance Recommendation Detected                                         │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  2. Scale Up Spot ASG (+1)                                                      │
│     📝 Mark scaleStartTime = NOW ⏰ (for timestamp-based capacity check)         │
│     • Get current desired capacity (1 AWS API call)                             │
│     • Set desired capacity to current + 1                                       │
│     • Log: "Attempting to scale up spot ASG"                                    │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  3. Wait for New Instance (Check every 10s, max 5 minutes)                      │
│     Primary Check: ✅ InService count increased?                                │
│     Secondary Check: ❌ Capacity failure detected?                              │
│       ├─ Fetch 50 scaling activities (increased from 10)                        │
│       ├─ Filter: Only check activities AFTER scaleStartTime - 5s                │
│       ├─ Check for: "InsufficientInstanceCapacity", "Spot request failed"       │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────────────────────────┐
    │  3.1 ✅ SUCCESS  │    │  3.2 ❌ FAILURE                       │
    │  New instance    │    │  • Timeout after 5 min                │
    │  is InService    │    │  • OR Capacity failure detected       │
    │  (1-3 minutes)   │    │    (AFTER scaleStartTime only!)       │
    └──────────────────┘    └──────────────────────────────────────┘
              ↓                       ↓
              |            ┌────────────────────────────────────────┐
              |            │  🔄 Fallback: Scale Up On-Demand       │
              |            │  📝 Mark onDemandScaleStartTime = NOW  │
              |            │  • Scale on-demand ASG (+1)            │
              |            │  • Wait for InService (same checks)    │
              |            │  • Create fallback event for tracking  │
              |            └────────────────────────────────────────┘
              |                       ↓
              └───────────┬───────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  4. Taint & Drain Interrupted Spot Node                                         │
│     • Apply RebalanceRecommendationTaint (NoSchedule)                           │
│     • Cordon node (mark unschedulable)                                          │
│     • Drain node using original NTH CordonAndDrain() ✅                         │
│       ├─ Respects PodDisruptionBudgets                                          │
│       ├─ Honors terminationGracePeriodSeconds                                   │
│       └─ Evicts pods gracefully                                                 │
│     • Pods migrate to new spot/on-demand instances                              │
└─────────────────────────────────────────────────────────────────────────────────┘

╔═════════════════════════════════════════════════════════════════════════════════╗
║  PHASE 2: CA PROTECTION (On ALL Spot Nodes - Runs Immediately)                  ║
╚═════════════════════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────────────────────────────────────────────┐
│  5. CA Protector Starts (On Each Spot Node)                                     │
│     🛡️ Protects NEW spot nodes from premature Cluster Autoscaler scale-down     │
│     • Starts immediately when pod detects it's on a spot node                   │
│     • Runs every 5 minutes (checks if protection needed)                        │
│     • Protection duration: 7 minutes (2m + 2m + 3m buffer)                      │
│       ├─ spotStabilityDuration: 2 minutes                                       │
│       ├─ minimumWaitDuration: 2 minutes                                         │
│       └─ podMigrationBuffer: 3 minutes                                          │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  6. Apply CA Protection Annotation                                              │
│     📝 Adds to spot node:                                                       │
│     • cluster-autoscaler.kubernetes.io/scale-down-disabled: "true"              │
│     • nth.aws.amazon.com/ca-protected-until-approx: "2025-10-09T08:00:00Z"      │
│                                                                                 │
│     ✅ Effect: Cluster Autoscaler CANNOT scale down this spot node              │
│     ✅ Prevents: CA from tainting new spots with "DeletionCandidateOfCA"        │
│     ✅ Allows: Pods to migrate from on-demand to this spot node safely          │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  7. Auto-Remove Protection After Expiry                                         │
│     • CA Protector checks every 5 minutes                                       │
│     • If current time > protectedUntil timestamp:                               │
│       ├─ Remove "scale-down-disabled" annotation                                │
│       ├─ Remove "ca-protected-until-approx" annotation                          │
│       └─ Log: "Removed CA protection (period expired)"                          │
│     • Spot node now available for CA scale-down (if needed)                     │
└─────────────────────────────────────────────────────────────────────────────────┘

╔═════════════════════════════════════════════════════════════════════════════════╗
║  PHASE 3: SELF-MONITORING (On On-Demand Nodes - Checks Every 30s)               ║
╚═════════════════════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────────────────────────────────────────────┐
│  8. Self-Monitor Starts (On Each On-Demand Node)                                │
│     🎯 Goal: Scale down THIS on-demand node when spot capacity is restored      │
│     • Starts immediately when pod detects it's on an on-demand node             │
│     • Checks every 30 seconds                                                   │
│     • Minimum wait: 2 minutes (protects workloads during migration)             │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  9. Check #1: Minimum Wait Time ⏰                                              │
│     Has on-demand node been running for ≥ 2 minutes?                           │
│       ├─ ❌ NO  → Continue monitoring (wait)                                    │
│       └─ ✅ YES → Proceed to spot health check                                  │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  10. Check #2: Spot ASG Comprehensive Health (1 API Call!) 🚀                                   │
│      🎯 Optimization: Combined 3 checks into 1 AWS API call                                     │
│      • DescribeAutoScalingGroups (single call)                                                  │
│        ├─ ASG Health: InService == Desired? ✅                                  │
│        ├─ Nodes Ready: K8s nodes matched via providerID ✅ (Check if connected to k8s)                      │
│        └─ Stability: Healthy for ≥ 2 minutes? ✅                                │
│                                      │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────────────┐
    │  ✅ HEALTHY &    │    │  ❌ NOT READY YET        │
    │     STABLE       │    │  • ASG unhealthy         │
    │                  │    │  • Nodes not ready       │
    │                  │    │  • Not stable yet        │
    └──────────────────┘    └──────────────────────────┘
              ↓                       ↓
              |                       └─→ Continue monitoring (30s)
              ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  11. Check #3: Cluster Utilization ⚡                                           │
│      Can we safely drain this on-demand node?                                   │
│      • Calculate cluster CPU & memory utilization                               │
│      • Check: Would drain exceed 75% utilization?                               │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌────────────────────────────────────────┐
    │  ✅ SAFE         │    │  ❌ UTILIZATION TOO HIGH (>75%)        │
    │  Utilization OK  │    │  Cluster would be overloaded!          │
    │  Can drain       │    └────────────────────────────────────────┘
    └──────────────────┘                     ↓
              ↓                  ┌───────────┴────────────┐
              |                  ↓ Pre-Scale Enabled?     ↓
              |         ┌─────────────────┐    ┌──────────────────┐
              |         │  ✅ YES         │    │  ❌ NO           │
              |         │  Try pre-scale  │    │  Keep on-demand  │
              |         └─────────────────┘    │  Wait & retry    │
              |                  ↓             └──────────────────┘
              |                  |
              |    ╔═════════════════════════════════════════════════════════╗
              |    ║  SMART PRE-SCALE (3-Level Safety Net) 🚀                ║
              |    ╚═════════════════════════════════════════════════════════╝
              |                  ↓
              |    ┌──────────────────────────────────────────────────────────┐
              |    │  Level 1: Calculate & Scale Up Spot ASG                  │
              |    │  📊 Calculate required spot nodes:                       │
              |    │    • currentUtil = 92%, target = 65%                     │
              |    │    • usedCapacity = used / (1 - buffer)                  │
              |    │    • requiredTotal = usedCapacity / target               │
              |    │    • nodesToAdd = requiredTotal - current + 1            │
              |    │  • Scale spot ASG desired capacity                       │
              |    │  • Wait up to 5 minutes for nodes ready                  │
              |    └──────────────────────────────────────────────────────────┘
              |                  ↓
              |         ┌────────┴────────┐
              |         ↓                 ↓
              |    ┌─────────┐      ┌──────────────────────────────────┐
              |    │ SUCCESS │      │ FAILED                            │
              |    │ Spots   │      │ • Capacity unavailable            │
              |    │ ready!  │      │ • Timeout                         │
              |    └─────────┘      │ • AWS API error                   │
              |         ↓           └──────────────────────────────────┘
              |         |                        ↓
              |         |           ┌────────────────────────────────────┐
              |         |           │  Level 2: Increase Threshold       │
              |         |           │  🔼 Temporarily allow 95% util     │
              |         |           │  • maxClusterUtilization = 95%     │
              |         |           │  • Retry drain check               │
              |         |           │  • If still too high → Level 3     │
              |         |           └────────────────────────────────────┘
              |         |                        ↓
              |         |           ┌────────────────────────────────────┐
              |         |           │  Level 3: Keep On-Demand (Safe)    │
              |         |           │  🛡️ Don't drain, keep running      │
              |         |           │  • Log: "Pre-scale failed"         │
              |         |           │  • Wait 10 minutes before retry    │
              |         |           │  • Protects workload stability     │
              |         |           └────────────────────────────────────┘
              |         |                        ↓
              |         └────────────┬───────────┘
              |                      ↓
              |         Retry on next cycle (30s or 10min backoff)
              |
              └───────────┬──────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  12. Check #4: Pod & PDB Safety ✅                                              │
│      Additional safety checks:                                                  │
│      • All pods can be safely rescheduled                                       │
│      • No PodDisruptionBudget violations                                        │
│      • No critical DaemonSets would be affected                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
              ┌───────────┴───────────┐
              ↓                       ↓
    ┌──────────────────┐    ┌──────────────────────────┐
    │  ✅ ALL SAFE     │    │  ❌ UNSAFE               │
    │  Can drain now   │    │  Continue monitoring     │
    └──────────────────┘    └──────────────────────────┘
              ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  13. 🎯 Scale Down This On-Demand Node                                          │
│      Step 1: Taint node                                                         │
│        • Add: "nth.aws.amazon.com/scaling-down:NoExecute"                       │
│      Step 2: Cordon node                                                        │
│        • Mark: node.Spec.Unschedulable = true                                   │
│      Step 3: Drain node (USES ORIGINAL NTH CordonAndDrain()!) ✅                │
│        • Evict pods gracefully                                                  │
│        • Respect PodDisruptionBudgets                                           │
│        • Honor terminationGracePeriodSeconds                                    │
│      Step 4: Wait for pods rescheduled                                          │
│        • Verify no non-DaemonSet pods remain                                    │
│      Step 5: Decrease on-demand ASG desired capacity (-1)                       │
│      Step 6: Terminate this EC2 instance                                        │
│        • Call: TerminateInstanceInAutoScalingGroup()                            │
└─────────────────────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────────────────────┐
│  14. ✅ Scale-Down Complete!                                                    │
│      • On-demand node removed                                                   │
│      • ASG capacity reduced                                                     │
│      • Workloads running on spot nodes                                          │
│      • 💰 Cost savings achieved!                                                │
│      • Self-monitor exits (pod terminates with node)                            │
└─────────────────────────────────────────────────────────────────────────────────┘

╔═════════════════════════════════════════════════════════════════════════════════╗
║  CONTINUOUS OPERATION                                                            ║
╚═════════════════════════════════════════════════════════════════════════════════╝

┌─────────────────────────────────────────────────────────────────────────────────┐
│  Parallel Operations:                                                           │
│  • Rebalance Monitor: Detects new interruptions (every 2s)                      │
│  • CA Protector: Protects spot nodes (every 5 min, on each spot pod)           │
│  • Self-Monitor: Monitors on-demand nodes (every 30s, on each on-demand pod)   │
│  • All pods run independently via DaemonSet                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
```

---

## 🔑 Key Improvements in v2.0

### 1. **🚀 Smart Pre-Scale (NEW!)**
```
Problem: High cluster utilization (92%) blocks drain
Solution: 3-level fallback
  ├─ Level 1: Calculate & add spot nodes proactively
  ├─ Level 2: Temporarily allow higher threshold (95%)
  └─ Level 3: Keep on-demand running (safe fallback)
```

### 2. **🛡️ CA Protection (NEW!)**
```
Problem: CA taints new spot nodes, prevents migration
Solution: Protect spots for 7 minutes
  ├─ Annotation: "scale-down-disabled: true"
  ├─ Duration: spotStability + minWait + buffer
  └─ Auto-remove when expired
```

### 3. **✅ Timestamp-Based Capacity Check (NEW!)**
```
Problem: False positives from old failures
Solution: Filter by scaleStartTime
  ├─ Mark time before scaling
  ├─ Fetch 50 activities (up from 10)
  ├─ Filter: only after scaleStartTime - 5s
  └─ Result: Zero false positives!
```

### 4. **⚡ Comprehensive Health Check (NEW!)**
```
Problem: 3 AWS API calls for health checks
Solution: Combined into 1 call
  ├─ ASG health check
  ├─ Node readiness check
  └─ Stability check
  → 67% reduction in API calls!
```

### 5. **🎯 Self-Monitoring (IMPROVED!)**
```
Each on-demand node monitors itself
  ├─ Independent decision making
  ├─ No race conditions
  ├─ Scales down when ready
  └─ Exits when node terminates
```

---

## 📊 Timing & Intervals

| Component | Interval | Purpose | Notes |
|-----------|----------|---------|-------|
| **IMDS Check** | 2 seconds | Detect rebalance | On all spot nodes |
| **ASG Status Check** | 10 seconds | Monitor scaling | During scale-up only |
| **Self-Monitor** | 30 seconds | Health checks | On on-demand nodes |
| **CA Protector** | 5 minutes | Protection check | On all spot nodes |
| **Minimum Wait** | 2 minutes | Protect workloads | Before scale-down |
| **Stability Duration** | 2 minutes | Ensure capacity | Spot must be stable |
| **Pod Migration Buffer** | 3 minutes | Extra safety | For CA protection |
| **Pre-Scale Timeout** | 5 minutes | Wait for spots | During pre-scale |
| **Pre-Scale Backoff** | 10 minutes | Retry delay | After failure |

---

## 🎯 Decision Flow: Can We Scale Down?

```
┌─────────────────────────────────────────┐
│  Should Scale Down On-Demand Node?      │
└─────────────────────────────────────────┘
                   ↓
         ┌─────────┴─────────┐
         │ Minimum wait met?  │
         │    (≥ 2 min)       │
         └─────────┬─────────┘
                   ↓ YES
         ┌─────────┴─────────┐
         │ Spot ASG healthy?  │
         │ (InService == Desired) │
         └─────────┬─────────┘
                   ↓ YES
         ┌─────────┴─────────┐
         │ Spot nodes ready?  │
         │ (In Kubernetes)    │
         └─────────┬─────────┘
                   ↓ YES
         ┌─────────┴─────────┐
         │ Spot been stable?  │
         │    (≥ 2 min)       │
         └─────────┬─────────┘
                   ↓ YES
         ┌─────────┴─────────┐
         │ Cluster util OK?   │
         │    (<75%)          │
         └─────────┬─────────┘
            ↓ YES  │  ↓ NO
            |      └──→ Try Pre-Scale?
            |             ↓ YES
            |        ┌─────────────┐
            |        │ Pre-Scale   │
            |        │ (3 levels)  │
            |        └─────────────┘
            |             ↓
         ┌─────────┴─────────┐
         │ Pods can migrate?  │
         │ (PDB OK?)          │
         └─────────┬─────────┘
                   ↓ YES
         ┌─────────┴─────────┐
         │  ✅ SCALE DOWN!   │
         └───────────────────┘
```

---

## 💰 Cost Optimization Example

### Scenario: 3 spot interruptions in 1 hour

#### Without Spot Guard:
```
Hour 1: 3 spot interruptions
  ├─ Scale up 3 on-demand (manual or basic logic)
  ├─ On-demand runs for rest of day
  └─ Cost: 3 × $0.50/hr × 23 hours = $34.50
```

#### With Spot Guard v2.0:
```
Hour 1: 3 spot interruptions
  ├─ 0:00 - Interruption #1
  │   ├─ Scale up spot (fails)
  │   ├─ Fallback: Scale up on-demand #1
  │   └─ Total: 1 on-demand
  ├─ 0:20 - Interruption #2
  │   ├─ Scale up spot (fails)
  │   ├─ Fallback: Scale up on-demand #2
  │   └─ Total: 2 on-demand
  ├─ 0:40 - Interruption #3
  │   ├─ Scale up spot (SUCCESS! ✅)
  │   ├─ CA Protection applied (7 min)
  │   └─ Total: 2 on-demand + spot capacity restored

Hour 1: Scale-down begins
  ├─ 0:42 - On-demand #1 ready to scale (2 min wait)
  │   ├─ Spot healthy ✅, stable ✅, util OK ✅
  │   └─ Scale down on-demand #1
  ├─ 0:47 - CA protection expires on new spot
  ├─ 1:02 - On-demand #2 ready to scale (2 min wait)
  │   ├─ Spot healthy ✅, stable ✅, util OK ✅
  │   └─ Scale down on-demand #2

Result:
  ├─ On-demand #1 ran: 42 minutes = $0.35
  ├─ On-demand #2 ran: 62 minutes = $0.52
  └─ Total cost: $0.87 (vs $34.50 without Spot Guard)

💰 Savings: $33.63 (97% reduction!)
```

---

## 🛡️ Safety Features

### 1. **Multi-Layer Safety Checks**
```
Before scale-down, verify:
├─ ✅ Minimum wait time (protect migrations)
├─ ✅ Spot ASG healthy
├─ ✅ Spot nodes ready in K8s
├─ ✅ Spot capacity stable (no flapping)
├─ ✅ Cluster utilization safe
├─ ✅ Pods can migrate (PDB check)
└─ ✅ No critical workloads affected
```

### 2. **Pre-Scale Fallback Chain**
```
If cluster util too high:
├─ Try: Scale up spot proactively
├─ Fail → Try: Allow higher threshold
└─ Fail → Keep on-demand running (safe!)
```

### 3. **CA Protection**
```
Protects new spot nodes:
├─ 7-minute protection window
├─ Prevents premature scale-down
└─ Auto-expires when safe
```

### 4. **Graceful Degradation**
```
If anything fails:
├─ AWS API throttled → Retry next cycle
├─ Spot unavailable → Keep on-demand
├─ Pre-scale fails → Try threshold increase
└─ All fails → Keep on-demand (safest)
```

---

## 🎯 Success Metrics

| Metric | Target | How Measured |
|--------|--------|--------------|
| **On-Demand Lifetime** | < 10 min | Time from scale-up to scale-down |
| **Spot Success Rate** | > 90% | Spot scale-ups / total attempts |
| **False Positives** | 0 | Old capacity failures detected |
| **API Call Reduction** | 67% | Comprehensive check vs 3 calls |
| **Cost Savings** | > 95% | On-demand hours avoided |
| **Zero Downtime** | 100% | No workload interruptions |

---

## 🔍 Observability & Debugging

### Key Log Messages:

```bash
# Phase 1: Scale-Up
"Attempting to scale up spot ASG"
"🚨 Detected capacity issue (keyword: 'InsufficientInstanceCapacity')"
"Falling back to on-demand ASG"

# Phase 2: CA Protection
"🛡️ Starting Cluster Autoscaler protection for spot node"
"✅ Applied CA scale-down protection to spot node"
"✅ Removed CA scale-down protection (protection period expired)"

# Phase 3: Self-Monitoring
"Detected on-demand node, starting Spot Guard self-monitor"
"Performing comprehensive spot ASG health check"
"Cluster utilization too high, attempting smart pre-scale"
"🚀 Pre-scale Level 1: Calculating required spot nodes"
"Pre-scale successful, will retry drain on next check cycle"
"All conditions met, initiating scale-down of this on-demand node"

# Phase 4: Scale-Down
"Starting on-demand node scale-down operation"
"Step 1/6: Tainting node"
"Step 3/6: Draining node (evicting pods)"
"Successfully drained on-demand node"
"✅ Scale-down complete!"
```

### Prometheus Metrics (Future):
```
spot_guard_on_demand_lifetime_seconds
spot_guard_scale_up_attempts_total{type="spot|on-demand"}
spot_guard_scale_up_success_total{type="spot|on-demand"}
spot_guard_scale_down_total
spot_guard_pre_scale_attempts_total{level="1|2|3"}
spot_guard_ca_protection_active_nodes
spot_guard_api_calls_total{api="DescribeAutoScalingGroups"}
```

---

## 🚀 Deployment Checklist

### Required Configuration:
```yaml
spotGuard:
  enabled: true
  spotASGName: "my-spot-asg"
  onDemandASGName: "my-on-demand-asg"
  
  # Timing
  checkInterval: 30              # Self-monitor check frequency
  minimumWaitDuration: 120       # Min time before scale-down (2 min)
  spotStabilityDuration: 120     # Spot must be stable (2 min)
  podMigrationBuffer: 180        # Extra safety buffer (3 min)
  
  # Safety
  maxClusterUtilization: 75      # Max cluster util for scale-down
  podEvictionTimeout: 300        # Max time to evict pods
  
  # Pre-Scale (Optional but Recommended)
  enablePreScale: true           # Enable smart pre-scale
  preScaleTimeout: 300           # Wait for pre-scaled spots (5 min)
  preScaleTargetUtilization: 65  # Target util after pre-scale
  preScaleSafetyBuffer: 10       # Safety buffer percentage
  preScaleFailureFallback: "increase_threshold"  # Level 2 strategy
  preScaleFallbackThreshold: 95  # Level 2 max util
  preScaleRetryBackoff: 600      # Wait before retry (10 min)
```

### Required IAM Permissions:
```json
{
  "Effect": "Allow",
  "Action": [
    "autoscaling:DescribeAutoScalingGroups",
    "autoscaling:SetDesiredCapacity",
    "autoscaling:DescribeScalingActivities",
    "autoscaling:TerminateInstanceInAutoScalingGroup"
  ],
  "Resource": "*"
}
```

### Required RBAC:
```yaml
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "patch", "update"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["pods/eviction"]
  verbs: ["create"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["get", "list"]
```

---

## 📚 Related Documentation

- `CAPACITY_CHECK_IMPROVEMENT_SUMMARY.md` - Timestamp-based capacity detection
- `CAPACITY_CHECK_BEFORE_AFTER.md` - Visual comparison of improvements
- `SPOT_GUARD_DRAIN_ARCHITECTURE.md` - How drain logic works
- `SPOT_GUARD_COMPLETE_FLOW_DIAGRAM.md` - Previous flow (v1.0)

---

## ✅ Implementation Status

- ✅ **Rebalance Detection**: Complete
- ✅ **Scale-Up with Fallback**: Complete
- ✅ **Timestamp-Based Capacity Check**: Complete (v2.0)
- ✅ **CA Protection**: Complete (v2.0)
- ✅ **Self-Monitoring**: Complete
- ✅ **Comprehensive Health Check**: Complete (v2.0)
- ✅ **Smart Pre-Scale**: Complete (v2.0)
- ✅ **Safety Checks**: Complete
- ✅ **Graceful Drain**: Complete (reuses original NTH)
- ✅ **Scale-Down Orchestration**: Complete

---

## 🎉 Summary

**Spot Guard v2.0** is a comprehensive solution for cost optimization that:

✅ **Detects** spot interruptions in real-time  
✅ **Scales up** spot capacity with on-demand fallback  
✅ **Protects** new spot nodes from CA scale-down  
✅ **Monitors** spot health continuously  
✅ **Pre-scales** intelligently when utilization is high  
✅ **Scales down** on-demand nodes automatically  
✅ **Saves costs** by minimizing on-demand usage  
✅ **Maintains reliability** with multi-layer safety checks  

**All while maintaining zero downtime and workload stability!** 🚀

---

**Version**: 2.0  
**Status**: ✅ **PRODUCTION READY**  
**Last Updated**: 2025-10-14

