# Spot Guard Implementation Guide

## Overview

Spot Guard is a feature integrated into AWS Node Termination Handler that automatically scales up replacement instances when EC2 rebalance recommendations are detected. It attempts to scale spot instances first, with automatic fallback to on-demand instances if spot capacity is unavailable.

## Architecture

### Components

1. **Configuration** (`pkg/config/config.go`)
   - New flags and environment variables for Spot Guard settings
   - Validation logic to ensure required parameters are provided

2. **Spot Guard Package** (`pkg/spotguard/spotguard.go`)
   - Core scaling logic for ASG operations
   - Capacity checking and fallback mechanism
   - Instance state monitoring

3. **Rebalance Monitor** (`pkg/monitor/rebalancerecommendation/rebalance-recommendation-monitor.go`)
   - Integration point for Spot Guard
   - Modified PreDrainTask to scale before tainting

4. **Main Handler** (`cmd/node-termination-handler.go`)
   - Initialization of Spot Guard when enabled
   - AWS SDK client setup for ASG operations

## Configuration

### Required Environment Variables/Flags

When `ENABLE_SPOT_GUARD=true`, the following are required:

- `ENABLE_SPOT_GUARD` - Enable Spot Guard feature (default: false)
- `SPOT_ASG_NAME` - Name of the spot instance Auto Scaling Group
- `ON_DEMAND_ASG_NAME` - Name of the on-demand instance Auto Scaling Group (fallback)
- `SPOT_GUARD_SCALE_TIMEOUT` - Timeout in seconds for spot scale-up (default: 120)
- `SPOT_GUARD_CAPACITY_CHECK_TIMEOUT` - Timeout to wait for InService state (default: 120)

### Additional Requirements

- `ENABLE_REBALANCE_MONITORING` or `ENABLE_REBALANCE_DRAINING` must be enabled
- `AWS_REGION` must be set if not automatically detected from instance metadata

### Example Configuration

```bash
# Environment Variables
export ENABLE_SPOT_GUARD=true
export SPOT_ASG_NAME=my-spot-asg
export ON_DEMAND_ASG_NAME=my-ondemand-asg
export SPOT_GUARD_SCALE_TIMEOUT=120
export SPOT_GUARD_CAPACITY_CHECK_TIMEOUT=120
export ENABLE_REBALANCE_MONITORING=true
export AWS_REGION=us-east-1
```

Or using CLI flags:

```bash
./node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=my-spot-asg \
  --on-demand-asg-name=my-ondemand-asg \
  --spot-guard-scale-timeout=120 \
  --spot-guard-capacity-check-timeout=120 \
  --enable-rebalance-monitoring=true \
  --aws-region=us-east-1 \
  --node-name=$NODE_NAME
```

## Workflow

### Normal Flow (Spot Capacity Available)

1. **Detection**: IMDS polling detects rebalance recommendation
2. **Scaling**: Spot Guard initiates spot ASG scale-up (+1 instance)
3. **Validation**: Waits for new instance to reach "InService" state
4. **Tainting**: Original node is tainted with `RebalanceRecommendationTaint`
5. **Draining**: Pods are drained from the tainted node
6. **Result**: Workloads migrate to the new spot instance

### Fallback Flow (Spot Capacity Unavailable)

1. **Detection**: IMDS polling detects rebalance recommendation
2. **Scaling Attempt**: Spot Guard initiates spot ASG scale-up
3. **Capacity Check**: Monitors for capacity issues:
   - Timeout waiting for InService
   - ASG scaling activities show "InsufficientCapacity" errors
4. **Fallback**: Automatically scales on-demand ASG (+1 instance)
5. **Validation**: Waits for on-demand instance to reach "InService"
6. **Tainting**: Original node is tainted
7. **Draining**: Pods are drained from the tainted node
8. **Result**: Workloads migrate to the new on-demand instance

### Failure Handling

If both spot and on-demand scaling fail:
- Node is still tainted to prevent new pod scheduling
- Error is logged for investigation
- Existing workloads remain until instance terminates

## IAM Permissions

The service account or EC2 instance role requires these permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:DescribeScalingActivities",
        "autoscaling:SetDesiredCapacity"
      ],
      "Resource": "*"
    }
  ]
}
```

## Monitoring and Logging

### Log Messages

Spot Guard produces structured logs for all operations:

```
INFO: Spot Guard enabled - Spot ASG: my-spot-asg, On-Demand ASG: my-ondemand-asg
INFO: Spot Guard: Starting pre-drain scaling workflow
INFO: Spot Guard: Attempting to scale up spot ASG: my-spot-asg
INFO: Spot Guard: Scaling ASG my-spot-asg from 3 to 4 instances
INFO: Spot Guard: Waiting for new instance in ASG my-spot-asg (initial count: 3)
INFO: Spot Guard: New instance reached InService in ASG my-spot-asg (count: 4)
INFO: Spot Guard: Successfully scaled up spot ASG: my-spot-asg
INFO: Spot Guard: Pre-drain workflow completed successfully
```

### Error Scenarios

```
WARN: Spot Guard: Timeout waiting for instance in ASG my-spot-asg
WARN: Spot Guard: Spot capacity appears unavailable
WARN: Spot Guard: Falling back to on-demand ASG: my-ondemand-asg
ERROR: Spot Guard: Failed to scale up replacement capacity
```

### Capacity Issues Detection

```
INFO: Spot Guard: Detected capacity issue in activity: InsufficientInstanceCapacity
WARN: Spot Guard: Detected capacity issue in ASG my-spot-asg
```

## Testing

### Prerequisites

1. Two Auto Scaling Groups:
   - One for spot instances
   - One for on-demand instances
2. IAM permissions configured
3. EC2 instance with IMDS access

### Manual Testing

#### Test 1: Spot Capacity Available

1. Deploy NTH with Spot Guard enabled
2. Trigger a rebalance recommendation (simulate via IMDS proxy)
3. Verify:
   - Spot ASG scales up by 1
   - New instance reaches InService
   - Original node gets tainted
   - Pods migrate successfully

#### Test 2: Spot Capacity Unavailable

1. Deploy NTH with Spot Guard enabled
2. Set spot ASG max size to prevent scaling
3. Trigger a rebalance recommendation
4. Verify:
   - Spot ASG scale attempt times out
   - On-demand ASG scales up by 1
   - Original node gets tainted
   - Pods migrate successfully

#### Test 3: Configuration Validation

```bash
# Should fail - missing required configs
./node-termination-handler --enable-spot-guard=true

# Should succeed
./node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=test-spot \
  --on-demand-asg-name=test-od \
  --enable-rebalance-monitoring=true \
  --node-name=$NODE_NAME
```

### Integration Testing

See `test/spot-guard/` directory for automated test scenarios (to be created).

## Troubleshooting

### Issue: Spot Guard not triggering

**Check:**
1. `ENABLE_SPOT_GUARD=true` is set
2. `ENABLE_REBALANCE_MONITORING` or `ENABLE_REBALANCE_DRAINING` is enabled
3. Rebalance recommendations are being detected (check IMDS polling logs)

### Issue: ASG not scaling

**Check:**
1. IAM permissions are correct
2. ASG names are correct and exist in the same region
3. ASG max size allows for scaling
4. AWS region is correctly configured

### Issue: Always falling back to on-demand

**Check:**
1. Spot capacity is actually available in the AZ
2. Timeout settings (`SPOT_GUARD_CAPACITY_CHECK_TIMEOUT`) are appropriate
3. ASG is configured to launch spot instances correctly
4. Check CloudWatch ASG metrics and scaling activities

### Issue: Timeout too short/long

**Adjust:**
- `SPOT_GUARD_SCALE_TIMEOUT`: Time to wait for spot scale-up
- `SPOT_GUARD_CAPACITY_CHECK_TIMEOUT`: Time to wait for InService state

Recommended values:
- Fast environments: 60-90 seconds
- Standard environments: 120 seconds (default)
- Slower environments: 180-240 seconds

## Performance Considerations

### Timing

- **IMDS Polling**: Every 2 seconds (NTH default)
- **ASG Status Checks**: Every 10 seconds
- **Default Timeout**: 120 seconds

### Resource Usage

- **Memory**: Minimal overhead (~5-10 MB for ASG client)
- **CPU**: Negligible (polling-based)
- **Network**: AWS API calls for ASG operations

### Scaling Impact

- **Spot Scale-up**: ~30-60 seconds typically
- **On-demand Scale-up**: ~20-40 seconds typically
- **Total workflow**: ~1-3 minutes from detection to tainting

## Best Practices

1. **ASG Configuration**:
   - Set appropriate max sizes for both ASGs
   - Configure health checks properly
   - Use mixed instance types for better availability

2. **Timeout Settings**:
   - Set timeouts based on your environment's typical launch times
   - Monitor and adjust based on actual performance

3. **Monitoring**:
   - Set up CloudWatch alarms for scaling failures
   - Monitor NTH logs for capacity issues
   - Track fallback frequency

4. **Cost Optimization**:
   - Monitor on-demand fallback rate
   - Adjust spot strategies if frequently falling back
   - Consider multiple spot instance types

## Future Enhancements

Potential improvements for future versions:

1. Prometheus metrics for Spot Guard operations
2. Kubernetes events for scaling operations
3. Configurable retry logic for failed scaling attempts
4. Support for scaling by more than +1 instance
5. Integration with Cluster Autoscaler
6. Advanced capacity issue detection algorithms

## References

- [AWS Node Termination Handler](https://github.com/aws/aws-node-termination-handler)
- [EC2 Rebalance Recommendations](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/rebalance-recommendations.html)
- [Auto Scaling Groups](https://docs.aws.amazon.com/autoscaling/ec2/userguide/what-is-amazon-ec2-auto-scaling.html)

