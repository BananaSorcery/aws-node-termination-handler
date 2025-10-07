# 🧪 Spot Guard Testing Strategy

## Overview

This guide provides a comprehensive testing strategy for Spot Guard using both AWS EC2 Spot Interrupter and EC2 Metadata Mock.

## 🎯 **Testing Phases**

### **Phase 1: Local Development Testing**
**Tool**: EC2 Metadata Mock  
**Purpose**: Test NTH logic without AWS dependencies

### **Phase 2: Integration Testing**
**Tool**: EC2 Spot Interrupter  
**Purpose**: Test real AWS integration and ASG scaling

### **Phase 3: Production Validation**
**Tool**: Real AWS Events  
**Purpose**: Validate in production environment

---

## 🔧 **Phase 1: Local Development Testing**

### **Setup EC2 Metadata Mock**

```bash
# Install EC2 Metadata Mock
go install github.com/aws/amazon-ec2-metadata-mock@latest

# Or use Docker
docker run -d --name ec2-metadata-mock \
  -p 1338:1338 \
  -e MOCK_DELAY_SEC=5 \
  -e MOCK_TRIGGER_TIME=2023-01-01T00:00:00Z \
  public.ecr.aws/aws-ec2/amazon-ec2-metadata-mock:latest
```

### **Test Rebalance Recommendation**

```bash
# Start metadata mock with rebalance recommendation
ec2-metadata-mock \
  --mock-delay-sec 5 \
  --mock-trigger-time 2023-01-01T00:00:00Z \
  --mock-file-path ./test-data/rebalance-recommendation.json
```

### **Test Data File**

Create `test-data/rebalance-recommendation.json`:

```json
{
  "instance-action": {
    "action": "rebalance-recommendation",
    "time": "2023-01-01T00:00:00Z"
  }
}
```

### **Test NTH Response**

```bash
# Check if NTH detects the mock event
curl -s http://localhost:1338/latest/meta-data/spot/instance-action

# Monitor NTH logs
kubectl logs -n kube-system -l app=aws-node-termination-handler -f
```

---

## 🚀 **Phase 2: Integration Testing**

### **Setup EC2 Spot Interrupter**

```bash
# Install EC2 Spot Interrupter
brew tap aws/tap
brew install ec2-spot-interrupter

# Or use Go
go install github.com/aws/amazon-ec2-spot-interrupter@latest
```

### **Test with Real Instances**

```bash
# Get your instance ID
INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)

# Test rebalance recommendation
ec2-spot-interrupter -d 5m --instance-ids i-02c820469b8a6a0a0
```
### **Interactive Testing**

```bash
# Use interactive mode
ec2-spot-interrupter --interactive
```

### **Test ASG Scaling**

```bash
# Test with multiple instances
ec2-spot-interrupter \
  --instance-ids i-1234567890abcdef0 i-0987654321fedcba0 \
  --delay 15s \
  --region us-west-2
```

---

## 🔍 **Phase 3: Production Validation**

### **Real AWS Events**

1. **Wait for Natural Events**: Let AWS send real rebalance recommendations
2. **Monitor ASG Scaling**: Watch your ASG scale up/down
3. **Validate Spot Guard**: Ensure on-demand nodes are scaled down when spot is stable

### **Monitoring Commands**

```bash
# Monitor NTH logs
kubectl logs -n kube-system -l app=aws-node-termination-handler -f

# Check ASG scaling activities
aws autoscaling describe-scaling-activities \
  --auto-scaling-group-name your-spot-asg

# Monitor spot instance status
aws ec2 describe-spot-instance-requests \
  --spot-instance-request-ids sir-1234567890abcdef0
```

---

## 📊 **Testing Scenarios**

### **Scenario 1: Spot Interruption**
1. **Trigger**: Real spot interruption or EC2 Spot Interrupter
2. **Expected**: NTH detects interruption, scales up spot ASG
3. **Fallback**: If spot fails, scales up on-demand ASG
4. **Validation**: Check ASG desired capacity increased

### **Scenario 2: Rebalance Recommendation**
1. **Trigger**: EC2 Spot Interrupter with rebalance recommendation
2. **Expected**: NTH detects rebalance, scales up spot ASG
3. **Fallback**: If spot fails, scales up on-demand ASG
4. **Validation**: Check ASG scaling activities

### **Scenario 3: Spot Stability**
1. **Trigger**: Wait for spot ASG to be stable
2. **Expected**: Spot Guard scales down on-demand ASG
3. **Validation**: Check on-demand ASG desired capacity decreased

### **Scenario 4: Capacity Issues**
1. **Trigger**: Limit spot ASG max capacity
2. **Expected**: Fallback to on-demand ASG
3. **Validation**: Check on-demand ASG scaling

---

## 🛠️ **Testing Tools Setup**

### **EC2 Metadata Mock Setup**

```bash
# Create test directory
mkdir -p test-data

# Create rebalance recommendation test data
cat > test-data/rebalance-recommendation.json << EOF
{
  "instance-action": {
    "action": "rebalance-recommendation",
    "time": "2023-01-01T00:00:00Z"
  }
}
EOF

# Create spot interruption test data
cat > test-data/spot-interruption.json << EOF
{
  "instance-action": {
    "action": "terminate",
    "time": "2023-01-01T00:00:00Z"
  }
}
EOF
```

### **EC2 Spot Interrupter Setup**

```bash
# Configure AWS credentials
aws configure

# Test connection
ec2-spot-interrupter --help

# List available instances
aws ec2 describe-instances \
  --query 'Reservations[].Instances[?State.Name==`running`].[InstanceId,Tags[?Key==`Name`].Value|[0]]' \
  --output table
```

---

## 📋 **Testing Checklist**

### **Pre-Testing Setup**
- [ ] EC2 Metadata Mock installed
- [ ] EC2 Spot Interrupter installed
- [ ] AWS credentials configured
- [ ] NTH deployed with Spot Guard enabled
- [ ] ASG names configured correctly
- [ ] IAM permissions set up

### **Local Testing**
- [ ] EC2 Metadata Mock running
- [ ] NTH detects mock events
- [ ] Spot Guard logic executes
- [ ] Logs show expected behavior

### **Integration Testing**
- [ ] EC2 Spot Interrupter working
- [ ] Real instances can be interrupted
- [ ] ASG scaling activities triggered
- [ ] NTH responds to real events

### **Production Validation**
- [ ] Real AWS events detected
- [ ] ASG scaling works correctly
- [ ] Spot Guard scale-down works
- [ ] Cost optimization achieved

---

## 🚨 **Important Considerations**

### **EC2 Spot Interrupter**
- **Real AWS Events**: Will trigger actual ASG scaling
- **Cost Impact**: May cause real scaling events
- **Instance Impact**: Will actually interrupt instances
- **Production Risk**: Test in non-production first

### **EC2 Metadata Mock**
- **Local Only**: Won't trigger real AWS events
- **Development**: Perfect for testing NTH logic
- **No Cost**: Free to use locally
- **Limited**: Won't test ASG integration

---

## 🎯 **Recommended Testing Flow**

1. **Start with EC2 Metadata Mock** for local development
2. **Use EC2 Spot Interrupter** for integration testing
3. **Validate with real AWS events** in production
4. **Monitor and tune** based on real usage

---

## 📞 **Support & Troubleshooting**

### **Common Issues**

1. **EC2 Metadata Mock not working**
   - Check if port 1338 is available
   - Verify mock data format
   - Check NTH configuration

2. **EC2 Spot Interrupter failing**
   - Verify AWS credentials
   - Check instance IDs are valid
   - Ensure FIS permissions

3. **NTH not detecting events**
   - Check NTH logs
   - Verify IMDS configuration
   - Test with curl commands

### **Debug Commands**

```bash
# Test IMDS access
curl -s http://169.254.169.254/latest/meta-data/spot/instance-action

# Check NTH logs
kubectl logs -n kube-system -l app=aws-node-termination-handler -f

# Verify ASG status
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names your-spot-asg
```

---

**This testing strategy ensures comprehensive validation of your Spot Guard implementation!** 🚀
