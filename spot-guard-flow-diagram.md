# Spot Guard Flow Diagram - Spot Rebalance Recommendation

This diagram shows the complete flow when Spot Guard is enabled and receives a spot rebalance recommendation.

## Visual Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           SPOT GUARD FLOW - REBALANCE RECOMMENDATION            │
└─────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────┐    ┌─────────────────────────────────────────────────────────┐
│   IMDS Polling  │───▶│  Rebalance Recommendation Detected?                    │
│  (Every 2s)    │    │                                                         │
└─────────────────┘    └─────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    Create InterruptionEvent with PreDrainTask                  │
└─────────────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                        Spot Guard PreDrainTask                                  │
│                     spotGuardPreDrainTask()                                    │
└─────────────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                    Step 1: Scale Up with Fallback                              │
│                     ScaleUpWithFallback()                                     │
└─────────────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                        Try Spot ASG Scale Up                                  │
│                          scaleUpASG()                                         │
└─────────────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────┐    ┌─────────────────────────────────────────────────────────┐
│ Get ASG Config  │───▶│  Check Max Size: currentDesired + 1 <= maxSize?        │
│ DescribeAutoScalingGroups │                                                         │
└─────────────────┘    └─────────────────────────────────────────────────────────┘
                                │
                    ┌───────────┴───────────┐
                    │                       │
                    ▼                       ▼
        ┌─────────────────────┐    ┌─────────────────────────────────────────┐
        │  Error: Cannot Scale │    │  Set Desired Capacity +1                │
        │                     │    │  SetDesiredCapacity()                    │
        └─────────────────────┘    └─────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │            ┌─────────────────────────────────────────┐
                    │            │     Wait for New Instance              │
                    │            │    waitForNewInstance()                │
                    │            └─────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │            ┌─────────────────────────────────────────┐
                    │            │  Check Every 10 seconds                │
                    │            │  getInServiceInstanceCount()            │
                    │            └─────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │            ┌─────────────────────────────────────────┐
                    │            │     New Instance InService?             │
                    │            └─────────────────────────────────────────┘
                    │                       │
                    │           ┌───────────┴───────────┐
                    │           │                       │
                    │           ▼                       ▼
                    │   ┌─────────────────┐    ┌─────────────────────────────────┐
                    │   │ ✅ Spot Success │    │  Check Capacity Issues           │
                    │   │   Log Success │    │  checkForCapacityIssues()         │
                    │   └─────────────────┘    └─────────────────────────────────┘
                    │           │                       │
                    │           │                       ▼
                    │           │              ┌─────────────────────────────────┐
                    │           │              │  Capacity Issue Detected?      │
                    │           │              └─────────────────────────────────┘
                    │           │                       │
                    │           │           ┌───────────┴───────────┐
                    │           │           │                       │
                    │           │           ▼                       ▼
                    │           │   ┌─────────────────┐    ┌─────────────────────┐
                    │           │   │ ❌ Spot Capacity │    │  Timeout Reached?   │
                    │           │   │   Unavailable   │    └─────────────────────┘
                    │           │   └─────────────────┘           │
                    │           │           │                       │
                    │           │           │                       ▼
                    │           │           │              ┌─────────────────────┐
                    │           │           │              │  Continue Waiting   │
                    │           │           │              └─────────────────────┘
                    │           │           │                       │
                    │           │           │                       │
                    │           │           │                       │
                    │           │           ▼                       │
                    │           │   ┌─────────────────────────────────┐
                    │           │   │  Fallback to On-Demand          │
                    │           │   │  fallbackToOnDemand()          │
                    │           │   └─────────────────────────────────┘
                    │           │           │
                    │           │           ▼
                    │           │   ┌─────────────────────────────────┐
                    │           │   │  Scale On-Demand ASG          │
                    │           │   │  scaleUpASG()                  │
                    │           │   └─────────────────────────────────┘
                    │           │           │
                    │           │           ▼
                    │           │   ┌─────────────────────────────────┐
                    │           │   │  Wait for On-Demand Instance    │
                    │           │   │  waitForNewInstance()           │
                    │           │   └─────────────────────────────────┘
                    │           │           │
                    │           │           ▼
                    │           │   ┌─────────────────────────────────┐
                    │           │   │     On-Demand Success?          │
                    │           │   └─────────────────────────────────┘
                    │           │           │
                    │           │   ┌───────┴───────┐
                    │           │   │               │
                    │           │   ▼               ▼
                    │           │ ┌─────────────┐ ┌─────────────────────┐
                    │           │ │ ✅ On-Demand │ │ ❌ Both Scaling     │
                    │           │ │   Success    │ │     Failed         │
                    │           │ └─────────────┘ └─────────────────────┘
                    │           │
                    │           ▼
                    │   ┌─────────────────────────────────────────────────────────┐
                    │   │              Step 2: Taint Node                        │
                    │   │         TaintRebalanceRecommendation()                 │
                    │   └─────────────────────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │   ┌─────────────────────────────────────────────────────────┐
                    │   │              Apply Taint                               │
                    │   │  spot-instance-terminating:true                       │
                    │   │  Effect: NoSchedule                                   │
                    │   └─────────────────────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │   ┌─────────────────────────────────────────────────────────┐
                    │   │         ✅ PreDrainTask Complete                        │
                    │   │         Node Ready for Draining                        │
                    │   └─────────────────────────────────────────────────────────┘
                    │                       │
                    │                       ▼
                    │   ┌─────────────────────────────────────────────────────────┐
                    │   │            Pod Draining Process                         │
                    │   │            AWS NTH continues                            │
                    │   └─────────────────────────────────────────────────────────┘
                    │
                    └─────────────────────────────────────────────────────────────┘
```

## Key Components

### 1. IMDS Monitoring
- **Component**: AWS Node Termination Handler
- **Action**: Polls IMDS endpoint every 2 seconds
- **Endpoint**: `http://169.254.169.254/latest/meta-data/events/recommendations/rebalance`

### 2. Spot Guard PreDrainTask
- **Trigger**: Rebalance recommendation detected
- **Purpose**: Scale replacement capacity BEFORE tainting the node
- **Location**: `pkg/monitor/rebalancerecommendation/rebalance-recommendation-monitor.go`

### 3. Scaling Logic (ScaleUpWithFallback)
- **Primary**: Attempts to scale spot ASG by +1
- **Fallback**: Scales on-demand ASG if spot fails
- **Validation**: Waits for new instance to reach "InService" state
- **Location**: `pkg/spotguard/spotguard.go`

### 4. Capacity Issue Detection
- **Method**: Monitors ASG scaling activities
- **Keywords**: "InsufficientInstanceCapacity", "Insufficient capacity", etc.
- **Timeout**: Configurable (default 120 seconds)

### 5. Node Tainting
- **Taint Key**: `spot-instance-terminating`
- **Taint Value**: `true`
- **Effect**: `NoSchedule`
- **Timing**: Only after successful scaling (or scaling failure)

## Configuration Requirements

```bash
ENABLE_SPOT_GUARD=true
SPOT_ASG_NAME=my-spot-asg
ON_DEMAND_ASG_NAME=my-ondemand-asg
ENABLE_REBALANCE_MONITORING=true
SPOT_GUARD_SCALE_TIMEOUT=120
SPOT_GUARD_CAPACITY_CHECK_TIMEOUT=120
```

## Success Paths

1. **Spot Success**: Spot ASG scales → New instance InService → Node tainted
2. **On-Demand Fallback**: Spot fails → On-demand ASG scales → New instance InService → Node tainted
3. **Both Fail**: Scaling fails → Node still tainted (prevents new pod scheduling)

## Key Benefits

- **Scale First, Taint Later**: Ensures replacement capacity before cordoning
- **Automatic Fallback**: Seamless transition from spot to on-demand
- **Capacity Detection**: Intelligent detection of spot capacity issues
- **Fault Tolerance**: Node tainted even if scaling fails
