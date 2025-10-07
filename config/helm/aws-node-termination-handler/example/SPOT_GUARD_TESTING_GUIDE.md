# Spot Guard Testing Guide

This guide explains how to test the Spot Guard feature using the provided example values file.

## Quick Testing Commands

### 1. Basic Template Generation
```bash
# Navigate to the helm chart directory
cd config/helm/aws-node-termination-handler

# Generate the complete manifest
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml

# Generate only the deployment
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml -s templates/deployment.yaml

# Generate with debug output
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml --debug
```

### 2. Validate Configuration
```bash
# Check if Spot Guard environment variables are present
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -i "SPOT_GUARD"

# Verify IMDS mode is enabled
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -i "ENABLE_SPOT_INTERRUPTION_DRAINING"
```

### 3. Test Different Scenarios
```bash
# Test with different ASG names
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set spotGuard.spotASGName="test-spot-asg" \
  --set spotGuard.onDemandASGName="test-ondemand-asg"

# Test with different timing configurations
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set spotGuard.minimumWaitDuration="10m" \
  --set spotGuard.checkInterval="1m"

# Test with Spot Guard disabled
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set spotGuard.enabled=false
```

## Expected Output

When you run the template generation, you should see:

### 1. Environment Variables
The deployment should include these Spot Guard environment variables:
```yaml
- name: SPOT_GUARD_ENABLED
  value: "true"
- name: SPOT_GUARD_SPOT_ASG_NAME
  value: "my-spot-asg"
- name: SPOT_GUARD_ON_DEMAND_ASG_NAME
  value: "my-ondemand-asg"
- name: SPOT_GUARD_MINIMUM_WAIT_DURATION
  value: "5m"
- name: SPOT_GUARD_CHECK_INTERVAL
  value: "30s"
- name: SPOT_GUARD_SPOT_STABILITY_DURATION
  value: "2m"
- name: SPOT_GUARD_MAX_CLUSTER_UTILIZATION
  value: "75"
- name: SPOT_GUARD_POD_EVICTION_TIMEOUT
  value: "5m"
- name: SPOT_GUARD_CLEANUP_INTERVAL
  value: "10m"
- name: SPOT_GUARD_MAX_EVENT_AGE
  value: "24h"
```

### 2. IMDS Configuration
The deployment should include IMDS-related environment variables:
```yaml
- name: ENABLE_SPOT_INTERRUPTION_DRAINING
  value: "true"
- name: ENABLE_REBALANCE_DRAINING
  value: "true"
- name: ENABLE_SCHEDULED_EVENT_DRAINING
  value: "true"
- name: ENABLE_REBALANCE_MONITORING
  value: "true"
- name: ENABLE_SCHEDULED_EVENT_MONITORING
  value: "true"
```

## Testing Checklist

### ✅ Basic Configuration
- [ ] Spot Guard is enabled (`SPOT_GUARD_ENABLED=true`)
- [ ] ASG names are set correctly
- [ ] IMDS mode is enabled (no SQS configuration)
- [ ] All timing parameters are present

### ✅ Security Configuration
- [ ] Pod security context is set
- [ ] Security context is configured
- [ ] Resource limits are defined
- [ ] Non-root user is configured

### ✅ Logging Configuration
- [ ] Debug logging is enabled (`logLevel: debug`)
- [ ] JSON logging is enabled (`jsonLogging: true`)

### ✅ Testing Annotations
- [ ] Test annotations are present
- [ ] Test labels are applied

## Troubleshooting

### Common Issues

1. **Missing Environment Variables**
   ```bash
   # Check if all Spot Guard env vars are present
   helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -A 20 "env:"
   ```

2. **IMDS Mode Not Enabled**
   ```bash
   # Verify SQS is disabled and IMDS is enabled
   helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -E "(ENABLE_SQS|ENABLE_SPOT)"
   ```

3. **Configuration Validation**
   ```bash
   # Validate the entire configuration
   helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml --validate
   ```

## Customization for Your Environment

### Update ASG Names
```bash
# Replace with your actual ASG names
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set spotGuard.spotASGName="your-spot-asg" \
  --set spotGuard.onDemandASGName="your-ondemand-asg"
```

### Adjust Timing Parameters
```bash
# For faster testing (shorter wait times)
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set spotGuard.minimumWaitDuration="2m" \
  --set spotGuard.checkInterval="15s" \
  --set spotGuard.spotStabilityDuration="1m"
```

### Enable Production Settings
```bash
# For production deployment
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml \
  --set logLevel="info" \
  --set spotGuard.minimumWaitDuration="30m" \
  --set spotGuard.checkInterval="5m"
```

## Next Steps

1. **Test the Template**: Run the helm template command to verify the configuration
2. **Validate Configuration**: Check that all environment variables are present
3. **Customize for Your Environment**: Update ASG names and timing parameters
4. **Deploy to Test Cluster**: Use the generated manifest for actual testing
5. **Monitor Logs**: Check the debug logs to verify Spot Guard is working correctly

## Support

If you encounter any issues during testing:

1. Check the generated manifest for missing environment variables
2. Verify that IMDS mode is properly configured
3. Ensure all Spot Guard parameters are present
4. Review the debug logs for any configuration errors


