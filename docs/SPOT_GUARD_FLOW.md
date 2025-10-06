# EKS Spot Instance Termination Handler Flow

## Overview
This Golang application handles spot instance termination gracefully when receiving rebalance recommendations from IMDS (Instance Metadata Service).

## Flow Steps

### 1. IMDS Rebalance Check (AWS NTH)
- **Component**: AWS Node Termination Handler
- **Action**: Poll IMDS endpoint for rebalance recommendations
- **Endpoint**: `http://169.254.169.254/latest/meta-data/events/recommendations/rebalance`
- **Frequency**: Every 5 seconds (configurable)
- **Response Handling**: 
  - If no rebalance recommendation: Continue polling
  - If rebalance recommendation found: Trigger webhook to custom scaling component

### 2. Custom ASG Scaling Component
- **Component**: Custom scaling service (your extension)
- **Trigger**: Webhook from AWS NTH
- **Action**: Scale up spot instance ASG by +1
- **Target**: Spot instance Auto Scaling Group
- **Method**: AWS Auto Scaling API call
- **Timeout**: 30 seconds for scale-up operation

### 3. Scale Up Validation (Custom Component)
- **3.1 Success Path**:
  - Spot instance successfully launched
  - New instance reaches "InService" state
  - Notify AWS NTH to proceed with tainting

- **3.2 Failure Path**:
  - Scale-up fails due to capacity constraints
  - **Indicators for capacity unavailability**:
    - ASG scaling activity shows "InProgress" but no new instances after 2 minutes
    - CloudWatch metrics show no capacity available in the AZ
    - ASG activity history shows "Insufficient capacity" error
  - **Fallback Action**: Scale up on-demand instance ASG by +1
  - **Target**: On-demand instance Auto Scaling Group (separate ASG)

### 4. Node Tainting (AWS NTH)
- **Component**: AWS Node Termination Handler
- **Action**: Apply taint to the spot instance receiving rebalance recommendation
- **Taint Key**: `spot-instance-terminating`
- **Taint Value**: `true`
- **Taint Effect**: `NoSchedule`
- **Purpose**: Prevent new pods from being scheduled on this instance

## Additional Considerations

### Error Handling
- **IMDS Unavailable**: Retry with exponential backoff
- **AWS API Failures**: Implement retry logic with jitter
- **ASG Scaling Failures**: Log detailed error and attempt fallback
- **Taint Application Failure**: Retry with exponential backoff

### Monitoring & Logging
- **Metrics**: Track rebalance events, scaling success/failure rates
- **Logs**: Structured logging for all operations
- **Alerts**: Notify on repeated failures or capacity issues

### Configuration
- **Polling Interval**: 5 seconds (configurable)
- **Scale-up Timeout**: 30 seconds
- **Retry Attempts**: 3 attempts with exponential backoff
- **ASG Names**: Configurable via environment variables

### Security
- **IAM Permissions**: 
  - `autoscaling:UpdateAutoScalingGroup`
  - `autoscaling:DescribeAutoScalingGroups`
  - `autoscaling:DescribeScalingActivities`
  - `ec2:DescribeInstances`
  - `eks:UpdateNodegroupConfig` (for tainting)

### Edge Cases
- **Multiple Rebalance Events**: Handle concurrent rebalance recommendations
- **ASG Already Scaling**: Wait for current scaling to complete
- **Instance Not in ASG**: Skip tainting if instance not managed by ASG
- **Pod Disruption Budgets**: Respect PDBs when tainting nodes

## Implementation Strategy

### Option 1: Custom Fork (Recommended for Your Use Case)
- **Fork AWS NTH**: Create a custom fork with your ASG scaling logic
- **Benefits**: 
  - **Full control over timing** - scale first, then taint
  - Leverage all existing IMDS and Kubernetes integration
  - Single service to deploy and monitor
  - No coordination complexity
- **Effort**: Medium - requires understanding NTH's codebase
- **Why Better**: Your requirement needs tight coordination between scaling and tainting

### Option 2: Sidecar with Coordination
- **Deploy AWS NTH**: With webhook and disabled immediate tainting
- **Custom Service**: Handles ASG scaling and calls back to NTH
- **Communication**: Webhook + callback API
- **Benefits**: 
  - Clean separation of concerns
  - Can reuse existing AWS NTH deployments
- **Effort**: Medium - requires coordination logic
- **Complexity**: Higher due to service coordination

### Option 3: Plugin Architecture
- **Modify AWS NTH**: Add plugin interface for custom scaling logic
- **Benefits**: 
  - Clean integration
  - Reusable for other custom logic
- **Effort**: High - requires significant NTH modifications

## Recommended Implementation (Option 1 - Custom Fork)
**Why this is better for your specific requirement:**

1. **Timing Control**: You control exactly when tainting happens
2. **Atomic Operations**: Scale → Validate → Taint in one service
3. **Simpler Deployment**: Single service to manage
4. **Better Error Handling**: If scaling fails, no tainting occurs

**Modified Flow:**
1. **IMDS Check**: Poll for rebalance recommendations
2. **Scale Up**: Increase spot ASG capacity
3. **Validate**: Wait for new instance to be ready
4. **Fallback**: If spot fails, scale on-demand ASG
5. **Taint**: Only taint after successful scaling

## Technical Requirements (Custom Fork Approach)
- **Custom NTH Fork**: 
  - IMDS polling for rebalance recommendations
  - ASG scaling logic (spot + on-demand fallback)
  - Kubernetes client for node tainting
  - AWS SDK for ASG operations
  - Health check endpoint
- **Dependencies**:
  - AWS SDK for Go v2
  - Kubernetes client-go
  - Prometheus metrics (reuse NTH's existing setup)
- **Configuration**:
  - Spot ASG name
  - On-demand ASG name
  - Scaling timeout settings
  - Taint configuration
