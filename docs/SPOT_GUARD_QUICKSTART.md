# Spot Guard Quick Start Guide

## What Was Implemented

Spot Guard is now fully integrated into AWS Node Termination Handler with the following flow:

```
1. Detect Rebalance Recommendation (IMDS)
         ↓
2. Scale Up Spot ASG (+1)
         ↓
3. Wait for InService (with timeout)
         ↓
    ┌────┴────┐
    ↓         ↓
SUCCESS    TIMEOUT/CAPACITY ISSUE
    ↓         ↓
    |    4. Scale Up On-Demand ASG (+1)
    |         ↓
    |    5. Wait for InService
    └────┬────┘
         ↓
6. Taint the Original Node
         ↓
7. Drain Pods
```

## Quick Test

### Step 1: Build the Application

```bash
cd /home/hardwin/repos/aws-node-termination-handler
make build
```

### Step 2: Set Up Environment Variables

```bash
export ENABLE_SPOT_GUARD=true
export SPOT_ASG_NAME="your-spot-asg-name"
export ON_DEMAND_ASG_NAME="your-ondemand-asg-name"
export SPOT_GUARD_SCALE_TIMEOUT=120
export SPOT_GUARD_CAPACITY_CHECK_TIMEOUT=120
export ENABLE_REBALANCE_MONITORING=true
export AWS_REGION="us-east-1"  # Your region
export NODE_NAME=$(hostname)
export DRY_RUN=false
```

### Step 3: Run Locally (with proper IAM credentials)

```bash
./build/node-termination-handler \
  --enable-spot-guard=true \
  --spot-asg-name=$SPOT_ASG_NAME \
  --on-demand-asg-name=$ON_DEMAND_ASG_NAME \
  --enable-rebalance-monitoring=true \
  --node-name=$NODE_NAME \
  --log-level=debug
```

### Step 4: Deploy to Kubernetes

Update your NTH DaemonSet with the new environment variables:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: aws-node-termination-handler
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: aws-node-termination-handler
  template:
    metadata:
      labels:
        app: aws-node-termination-handler
    spec:
      serviceAccountName: aws-node-termination-handler
      hostNetwork: true
      containers:
      - name: aws-node-termination-handler
        image: your-registry/aws-node-termination-handler:spot-guard
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: ENABLE_SPOT_GUARD
          value: "true"
        - name: SPOT_ASG_NAME
          value: "your-spot-asg"
        - name: ON_DEMAND_ASG_NAME
          value: "your-ondemand-asg"
        - name: SPOT_GUARD_SCALE_TIMEOUT
          value: "120"
        - name: SPOT_GUARD_CAPACITY_CHECK_TIMEOUT
          value: "120"
        - name: ENABLE_REBALANCE_MONITORING
          value: "true"
        - name: ENABLE_REBALANCE_DRAINING
          value: "true"
        - name: AWS_REGION
          value: "us-east-1"
        - name: LOG_LEVEL
          value: "info"
```

### Step 5: Required IAM Permissions

Add to your NTH IAM role or service account:

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

## Verify It's Working

### Check Logs

```bash
# Kubernetes
kubectl logs -n kube-system -l app=aws-node-termination-handler --tail=100

# Look for:
# "Spot Guard enabled - Spot ASG: xxx, On-Demand ASG: yyy"
```

### Testing Rebalance Recommendations

**Important**: AWS EC2 rebalance recommendations cannot be manually triggered - they are generated automatically by AWS based on internal capacity assessments.

#### Testing Options:

**Option 1: Use Mock IMDS Server (Recommended for Development)**
```bash
# Start mock IMDS server
python3 test-mock-imds.py

# In another terminal, run NTH pointing to mock server
export INSTANCE_METADATA_URL="http://localhost:8080"
./build/node-termination-handler --enable-spot-guard=true ...
```

**Option 2: Dry Run Mode**
```bash
# Test logic without affecting cluster
./test-spot-guard-dry-run.sh
```

**Option 3: Use Test Infrastructure**
```bash
# Run comprehensive tests with LocalStack
./test/e2e/rebalance-recommendation-sqs-test
```

**Option 4: Wait for Real Events**
- Deploy to a test cluster and wait for actual rebalance recommendations
- Monitor logs: `kubectl logs -n kube-system -l app=aws-node-termination-handler`

### Monitor ASG Scaling

```bash
# Check spot ASG
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names $SPOT_ASG_NAME \
  --region $AWS_REGION

# Check on-demand ASG
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names $ON_DEMAND_ASG_NAME \
  --region $AWS_REGION

# Check recent scaling activities
aws autoscaling describe-scaling-activities \
  --auto-scaling-group-name $SPOT_ASG_NAME \
  --max-records 10 \
  --region $AWS_REGION
```

## Expected Log Output

### Successful Spot Scale-up:

```
INFO: Spot Guard enabled - Spot ASG: my-spot-asg, On-Demand ASG: my-ondemand-asg
INFO: Rebalance recommendation received. Instance will be cordoned at 2024-01-15T10:30:00Z
INFO: Spot Guard: Starting pre-drain scaling workflow
INFO: Spot Guard: Attempting to scale up spot ASG: my-spot-asg
INFO: Spot Guard: Scaling ASG my-spot-asg from 3 to 4 instances
INFO: Spot Guard: Waiting for new instance in ASG my-spot-asg (initial count: 3)
INFO: Spot Guard: New instance reached InService in ASG my-spot-asg (count: 4)
INFO: Spot Guard: Successfully scaled up spot ASG: my-spot-asg
INFO: Spot Guard: Pre-drain workflow completed successfully
```

### Fallback to On-Demand:

```
INFO: Spot Guard: Attempting to scale up spot ASG: my-spot-asg
INFO: Spot Guard: Scaling ASG my-spot-asg from 3 to 4 instances
WARN: Spot Guard: Timeout waiting for instance in ASG my-spot-asg
WARN: Spot Guard: Spot capacity appears unavailable (timeout waiting for InService)
WARN: Spot Guard: Falling back to on-demand ASG: my-ondemand-asg
INFO: Spot Guard: Scaling ASG my-ondemand-asg from 2 to 3 instances
INFO: Spot Guard: New instance reached InService in ASG my-ondemand-asg (count: 3)
INFO: Spot Guard: Successfully scaled up on-demand ASG: my-ondemand-asg
```

## Troubleshooting

### Error: "spot-asg-name is required when enable-spot-guard is true"

**Solution**: Make sure both `SPOT_ASG_NAME` and `ON_DEMAND_ASG_NAME` are set.

### Error: "enable-rebalance-monitoring or enable-rebalance-draining must be enabled"

**Solution**: Set either `ENABLE_REBALANCE_MONITORING=true` or `ENABLE_REBALANCE_DRAINING=true`.

### Error: "failed to describe ASG: AccessDenied"

**Solution**: Add the required IAM permissions (see Step 5 above).

### ASG not scaling

**Check:**
1. ASG exists in the same region as your cluster
2. ASG name is spelled correctly (case-sensitive)
3. ASG max size allows for +1 instance
4. IAM permissions are attached to the correct role

## Testing in Development

### Mock Rebalance Recommendation

You can test the logic by temporarily modifying IMDS response or using a test harness. For production testing, wait for an actual rebalance recommendation from AWS.

### Dry Run Mode

To test without actually tainting nodes:

```bash
export DRY_RUN=true
```

This will log what actions would be taken without executing them.

## Next Steps

1. **Monitor Performance**: Watch logs and ASG metrics for the first few days
2. **Tune Timeouts**: Adjust `SPOT_GUARD_SCALE_TIMEOUT` based on actual launch times
3. **Set Up Alerts**: Configure CloudWatch alarms for scaling failures
4. **Cost Analysis**: Track fallback frequency to optimize spot strategy

## Files Modified

- `pkg/config/config.go` - Added Spot Guard configuration
- `pkg/spotguard/spotguard.go` - New package for ASG scaling logic
- `pkg/monitor/rebalancerecommendation/rebalance-recommendation-monitor.go` - Integrated Spot Guard
- `cmd/node-termination-handler.go` - Initialize Spot Guard

## Summary

✅ Spot Guard fully integrated  
✅ Configuration validation added  
✅ Spot scaling with on-demand fallback  
✅ Comprehensive logging  
✅ Error handling and timeouts  
✅ Documentation complete  

Your Spot Guard implementation is ready to use! 🚀

