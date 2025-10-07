# Spot Guard Helm Chart Guide

## Quick Start

### 1. Install with Spot Guard Enabled

```bash
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/123456789012/my-queue \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=my-spot-asg \
  --set spotGuard.onDemandASGName=my-ondemand-asg
```

### 2. Install with Custom Values File

```bash
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler \
  --values SPOT_GUARD_EXAMPLE.yaml
```

## Configuration Options

### Required Settings

| Parameter | Description | Example |
|-----------|-------------|---------|
| `spotGuard.enabled` | Enable Spot Guard | `true` |
| `spotGuard.spotASGName` | Name of spot ASG | `"my-spot-nodegroup"` |
| `spotGuard.onDemandASGName` | Name of on-demand ASG | `"my-ondemand-nodegroup"` |

### Timing Settings

| Parameter | Default | Description |
|-----------|---------|-------------|
| `spotGuard.minimumWaitDuration` | `"10m"` | Min wait before scale-down |
| `spotGuard.checkInterval` | `"30s"` | How often to check |
| `spotGuard.spotStabilityDuration` | `"2m"` | Spot stability requirement |

### Safety Settings

| Parameter | Default | Description |
|-----------|---------|-------------|
| `spotGuard.maxClusterUtilization` | `75` | Max utilization % |
| `spotGuard.podEvictionTimeout` | `"5m"` | Pod eviction timeout |

### Maintenance Settings

| Parameter | Default | Description |
|-----------|---------|-------------|
| `spotGuard.cleanupInterval` | `"10m"` | Event cleanup interval |
| `spotGuard.maxEventAge` | `"24h"` | Max event age |

## Configuration Profiles

### Conservative (Production with Strict SLAs)

```yaml
spotGuard:
  enabled: true
  spotASGName: "my-spot-asg"
  onDemandASGName: "my-ondemand-asg"
  minimumWaitDuration: "15m"
  checkInterval: "60s"
  spotStabilityDuration: "5m"
  maxClusterUtilization: 70
```

### Balanced (Recommended)

```yaml
spotGuard:
  enabled: true
  spotASGName: "my-spot-asg"
  onDemandASGName: "my-ondemand-asg"
  minimumWaitDuration: "10m"
  checkInterval: "30s"
  spotStabilityDuration: "2m"
  maxClusterUtilization: 75
```

### Aggressive (Maximum Cost Savings)

```yaml
spotGuard:
  enabled: true
  spotASGName: "my-spot-asg"
  onDemandASGName: "my-ondemand-asg"
  minimumWaitDuration: "5m"
  checkInterval: "20s"
  spotStabilityDuration: "1m"
  maxClusterUtilization: 80
```

## Environment Variables

Spot Guard configurations are passed as environment variables:

| Environment Variable | Helm Value |
|---------------------|------------|
| `SPOT_GUARD_ENABLED` | `spotGuard.enabled` |
| `SPOT_GUARD_SPOT_ASG_NAME` | `spotGuard.spotASGName` |
| `SPOT_GUARD_ONDEMAND_ASG_NAME` | `spotGuard.onDemandASGName` |
| `SPOT_GUARD_MINIMUM_WAIT_DURATION` | `spotGuard.minimumWaitDuration` |
| `SPOT_GUARD_CHECK_INTERVAL` | `spotGuard.checkInterval` |
| `SPOT_GUARD_SPOT_STABILITY_DURATION` | `spotGuard.spotStabilityDuration` |
| `SPOT_GUARD_MAX_CLUSTER_UTILIZATION` | `spotGuard.maxClusterUtilization` |
| `SPOT_GUARD_POD_EVICTION_TIMEOUT` | `spotGuard.podEvictionTimeout` |
| `SPOT_GUARD_CLEANUP_INTERVAL` | `spotGuard.cleanupInterval` |
| `SPOT_GUARD_MAX_EVENT_AGE` | `spotGuard.maxEventAge` |

## IAM Permissions

Your service account IAM role needs these permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:SetDesiredCapacity",
        "autoscaling:DescribeTags",
        "autoscaling:DescribeAutoScalingInstances",
        "ec2:DescribeInstances",
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage"
      ],
      "Resource": "*"
    }
  ]
}
```

## Verification

### Check if Spot Guard is Enabled

```bash
kubectl get deployment -n kube-system aws-node-termination-handler -o yaml | grep SPOT_GUARD_ENABLED
```

### View Spot Guard Logs

```bash
kubectl logs -n kube-system -l app.kubernetes.io/name=aws-node-termination-handler | grep -i "spot guard\|spotguard"
```

### Check Configuration

```bash
kubectl get deployment -n kube-system aws-node-termination-handler -o yaml | grep -A 20 SPOT_GUARD
```

## Troubleshooting

### Spot Guard Not Working

1. **Check if enabled:**
   ```bash
   kubectl get deployment -n kube-system aws-node-termination-handler -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SPOT_GUARD_ENABLED")].value}'
   ```

2. **Verify ASG names:**
   ```bash
   kubectl get deployment -n kube-system aws-node-termination-handler -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="SPOT_GUARD_SPOT_ASG_NAME")].value}'
   ```

3. **Check logs for errors:**
   ```bash
   kubectl logs -n kube-system -l app.kubernetes.io/name=aws-node-termination-handler --tail=100 | grep -i error
   ```

### On-Demand Not Scaling Down

Check the logs for specific reasons:

```bash
kubectl logs -n kube-system -l app.kubernetes.io/name=aws-node-termination-handler -f | grep "Cannot scale down"
```

Common reasons:
- Minimum wait time not met
- Spot capacity not stable
- PodDisruptionBudget would be violated
- Cluster utilization too high

## Upgrading

### From No Spot Guard to Spot Guard Enabled

```bash
# Update existing deployment
helm upgrade aws-node-termination-handler \
  --namespace kube-system \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler \
  --reuse-values \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=my-spot-asg \
  --set spotGuard.onDemandASGName=my-ondemand-asg
```

### Update Configuration

```bash
helm upgrade aws-node-termination-handler \
  --namespace kube-system \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler \
  --reuse-values \
  --set spotGuard.minimumWaitDuration=15m
```

## Examples

### Example 1: Basic Setup

```bash
helm install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/123456789012/my-queue \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=eks-spot-nodes \
  --set spotGuard.onDemandASGName=eks-ondemand-fallback \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler
```

### Example 2: With IAM Role

```bash
helm install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/123456789012/my-queue \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/NTHRole \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=eks-spot-nodes \
  --set spotGuard.onDemandASGName=eks-ondemand-fallback \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler
```

### Example 3: Conservative Settings

```bash
helm install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/123456789012/my-queue \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=eks-spot-nodes \
  --set spotGuard.onDemandASGName=eks-ondemand-fallback \
  --set spotGuard.minimumWaitDuration=15m \
  --set spotGuard.spotStabilityDuration=5m \
  --set spotGuard.maxClusterUtilization=70 \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler
```

## Best Practices

1. **Start Conservative**: Begin with conservative settings in production
2. **Monitor Logs**: Watch logs for the first few scale-down events
3. **Adjust Gradually**: Fine-tune settings based on your workload
4. **Set Alerts**: Alert on repeated scale-down failures
5. **Resource Limits**: Increase memory if tracking many events

## Support

For issues or questions:
- Check logs: `kubectl logs -n kube-system -l app.kubernetes.io/name=aws-node-termination-handler`
- Review: `pkg/spotguard/LOGGING_GUIDE.md`
- GitHub Issues: https://github.com/aws/aws-node-termination-handler/issues

