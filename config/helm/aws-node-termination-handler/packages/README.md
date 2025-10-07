# AWS Node Termination Handler with Spot Guard

This is a forked and enhanced version of the AWS Node Termination Handler with the innovative **Spot Guard** feature for automatic on-demand scale-down.

## 🚀 New Features

### Spot Guard - Automatic On-Demand Scale-Down
- **Cost Optimization**: Automatically scales down on-demand instances when spot capacity is restored
- **Intelligent Monitoring**: Continuous monitoring of spot ASG health and Kubernetes node readiness
- **Safety First**: Multi-criteria health checks including pod safety and cluster buffer protection
- **Configurable**: Flexible timing and threshold configurations

## 📦 Installation

### Test if index.yaml is accessible

```bash
# Test if index.yaml is accessible
curl -I https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/index.yaml

# Test if package is accessible
curl -I https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/aws-node-termination-handler-1.0.0.tgz
```

### Add Helm Repository
```bash
helm repo add aws-nth-enhanced https://raw.githubusercontent.com/BananaSorcery/aws-node-termination-handler/main/config/helm/aws-node-termination-handler/packages/
helm repo update
```

### Install with Spot Guard
```bash
helm install aws-nth aws-nth-enhanced/aws-node-termination-handler \
  --set spotGuard.enabled=true \
  --set spotGuard.spotASGName=your-spot-asg \
  --set spotGuard.onDemandASGName=your-ondemand-asg
```

### Install IMDS Mode (Default)
```bash
helm install aws-nth aws-nth-enhanced/aws-node-termination-handler
```

### Install SQS Mode
```bash
helm install aws-nth aws-nth-enhanced/aws-node-termination-handler \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/123456789012/my-queue
```

## ⚙️ Spot Guard Configuration

```yaml
spotGuard:
  enabled: true                    # Enable Spot Guard
  spotASGName: "your-spot-asg"     # Your spot ASG name
  onDemandASGName: "your-ondemand-asg" # Your on-demand ASG name
  minimumWaitDuration: "10m"        # Wait time before scale-down
  checkInterval: "30s"             # Check frequency
  spotStabilityDuration: "2m"      # Stability requirement
  maxClusterUtilization: 75        # Cluster buffer percentage
  podEvictionTimeout: "5m"         # Pod eviction timeout
  cleanupInterval: "10m"           # Event cleanup frequency
  maxEventAge: "24h"              # Event retention period
```

## 🔧 Configuration Options

### IMDS Mode (Default)
- Spot interruption handling
- Rebalance recommendation monitoring
- Scheduled event handling
- ASG lifecycle handling

### SQS Mode
- Queue-based event processing
- EventBridge integration
- Lifecycle hook support

## 📊 Benefits

- **Cost Savings**: Automatic on-demand scale-down when spot capacity is available
- **Reliability**: Enhanced safety checks and cluster buffer protection
- **Flexibility**: Support for both IMDS and SQS modes
- **Production Ready**: Comprehensive configuration options and security best practices

## 🆚 Version History

- **v1.0.0**: Initial release with Spot Guard feature
  - Added automatic on-demand scale-down
  - Enhanced safety checks
  - Multi-criteria health monitoring
  - Comprehensive configuration options

## 📚 Documentation

For detailed configuration and usage instructions, see the main repository documentation.

## 🤝 Contributing

This is a forked version with enhancements. For contributions to the original project, please refer to the upstream repository.
