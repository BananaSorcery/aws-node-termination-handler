# 🔍 Node Type Detection - Complete Flow

## Real-World Example: Pod Starting on On-Demand Node

### **Scenario Setup**
```yaml
Configuration:
  spotGuard:
    enabled: true
    spotASGName: "eks-spot-workers"
    onDemandASGName: "eks-ondemand-workers"

Cluster:
  - Spot Nodes: ip-10-0-1-50, ip-10-0-1-51 (in eks-spot-workers ASG)
  - On-Demand Node: ip-10-0-2-100 (in eks-ondemand-workers ASG)

Event: NTH pod starts on ip-10-0-2-100
```

## 🎬 **Step-by-Step Detection Process**

### **Step 1: Pod Initialization**
```bash
# Pod starts on node ip-10-0-2-100
$ kubectl get pod aws-node-termination-handler-xyz -o wide
NAME                                  NODE            
aws-node-termination-handler-xyz     ip-10-0-2-100

# NTH reads its own node name from environment
nodeName := os.Getenv("NODE_NAME")  // "ip-10-0-2-100"
```

### **Step 2: Create Node Detector**
```go
// In cmd/node-termination-handler.go
nodeDetector := spotguard.NewNodeDetector(
    imds,                          // IMDS client
    asgClient,                     // AWS Auto Scaling client
    clientset,                     // Kubernetes client
    nthConfig.NodeName,            // "ip-10-0-2-100"
)
```

### **Step 3: Attempt Detection**
```go
isOnDemandNode, err := nodeDetector.IsOnDemandNode(nthConfig.OnDemandAsgName)
```

## 🔍 **Method 1: ASG Membership Check**

### **Step 3.1: Query IMDS for Instance ID**
```bash
# Pod makes HTTP request to IMDS
GET http://169.254.169.254/latest/meta-data/instance-id

Response: i-0987654321fedcba0
```

```go
instanceID, err := nd.imds.GetMetadataInfo("instance-id", false)
// instanceID = "i-0987654321fedcba0"
```

**Log Output:**
```
DEBUG: Detecting node type
       nodeName=ip-10-0-2-100
       onDemandASG=eks-ondemand-workers
DEBUG: Checking ASG membership
       instanceID=i-0987654321fedcba0
       onDemandASG=eks-ondemand-workers
```

### **Step 3.2: Query AWS Auto Scaling API**
```go
result, err := nd.asgClient.DescribeAutoScalingInstances(&autoscaling.DescribeAutoScalingInstancesInput{
    InstanceIds: []*string{&instanceID},
})
```

**AWS API Call:**
```bash
# Equivalent AWS CLI command:
$ aws autoscaling describe-auto-scaling-instances \
  --instance-ids i-0987654321fedcba0

{
  "AutoScalingInstances": [
    {
      "InstanceId": "i-0987654321fedcba0",
      "AutoScalingGroupName": "eks-ondemand-workers",
      "AvailabilityZone": "us-west-2a",
      "LifecycleState": "InService",
      "HealthStatus": "Healthy"
    }
  ]
}
```

### **Step 3.3: Compare ASG Names**
```go
asgName := *result.AutoScalingInstances[0].AutoScalingGroupName
// asgName = "eks-ondemand-workers"

isOnDemand := asgName == onDemandASGName
// isOnDemand = "eks-ondemand-workers" == "eks-ondemand-workers"
// isOnDemand = TRUE ✅
```

**Log Output:**
```
INFO: Detected node type via ASG membership
      instanceID=i-0987654321fedcba0
      currentASG=eks-ondemand-workers
      onDemandASG=eks-ondemand-workers
      isOnDemand=true
```

### **Step 3.4: Result**
```go
return true, nil  // Successfully detected as on-demand!
```

## ✅ **Decision: Start Self-Monitor**

```go
if isOnDemandNode {
    log.Info().
        Str("nodeName", "ip-10-0-2-100").
        Str("onDemandASG", "eks-ondemand-workers").
        Msg("Detected on-demand node, starting Spot Guard self-monitor")
    
    selfMonitor := spotguard.NewSelfMonitor(...)
    go selfMonitor.Start(context.Background())
}
```

**Log Output:**
```
INFO: Detected on-demand node, starting Spot Guard self-monitor
      nodeName=ip-10-0-2-100
      onDemandASG=eks-ondemand-workers
INFO: Spot Guard self-monitor started for on-demand scale-down
```

---

## 🔄 **Alternative: Detection via Node Labels**

If ASG check had failed, here's what would happen:

### **Step 4.1: Query Kubernetes Node**
```bash
$ kubectl get node ip-10-0-2-100 -o yaml
```

```yaml
apiVersion: v1
kind: Node
metadata:
  name: ip-10-0-2-100
  labels:
    eks.amazonaws.com/capacityType: ON_DEMAND
    node.kubernetes.io/instance-type: t3.large
    topology.kubernetes.io/zone: us-west-2a
```

### **Step 4.2: Check Labels**
```go
node, err := nd.clientset.CoreV1().Nodes().Get(context.Background(), "ip-10-0-2-100", metav1.GetOptions{})

// Check EKS label
if capacityType, exists := node.Labels["eks.amazonaws.com/capacityType"]; exists {
    isOnDemand := capacityType == "ON_DEMAND"
    // capacityType = "ON_DEMAND"
    // isOnDemand = TRUE ✅
}
```

**Log Output:**
```
DEBUG: ASG detection failed, trying node labels
       nodeName=ip-10-0-2-100
DEBUG: Checking node labels for capacity type
       nodeName=ip-10-0-2-100
       labelCount=15
INFO: Detected node type via EKS label
      nodeName=ip-10-0-2-100
      capacityType=ON_DEMAND
      isOnDemand=true
```

---

## 🎭 **Comparison: Spot Node vs On-Demand Node**

### **Spot Node Detection:**

```bash
# Pod starts on spot node ip-10-0-1-50

# ASG Check:
instanceID = "i-0123456789abcdef0"
AWS API → ASG = "eks-spot-workers"
Compare → "eks-spot-workers" != "eks-ondemand-workers" ❌
Result → isOnDemand = FALSE

# Log Output:
INFO: Detected node type via ASG membership
      instanceID=i-0123456789abcdef0
      currentASG=eks-spot-workers
      onDemandASG=eks-ondemand-workers
      isOnDemand=false
INFO: Detected spot node, self-monitor will not start (scale-up only mode)
      nodeName=ip-10-0-1-50
```

### **On-Demand Node Detection:**

```bash
# Pod starts on on-demand node ip-10-0-2-100

# ASG Check:
instanceID = "i-0987654321fedcba0"
AWS API → ASG = "eks-ondemand-workers"
Compare → "eks-ondemand-workers" == "eks-ondemand-workers" ✅
Result → isOnDemand = TRUE

# Log Output:
INFO: Detected node type via ASG membership
      instanceID=i-0987654321fedcba0
      currentASG=eks-ondemand-workers
      onDemandASG=eks-ondemand-workers
      isOnDemand=true
INFO: Detected on-demand node, starting Spot Guard self-monitor
      nodeName=ip-10-0-2-100
      onDemandASG=eks-ondemand-workers
```

---

## 🧪 **Testing Detection**

### **Test 1: Verify Detection on Spot Nodes**
```bash
# Get spot node
SPOT_NODE=$(kubectl get nodes -l eks.amazonaws.com/capacityType=SPOT -o jsonpath='{.items[0].metadata.name}')

# Check NTH logs on spot node
kubectl logs -n kube-system -l app=aws-node-termination-handler \
  --field-selector spec.nodeName=$SPOT_NODE | grep "Detected"

# Expected:
# "Detected spot node, self-monitor will not start (scale-up only mode)"
```

### **Test 2: Verify Detection on On-Demand Nodes**
```bash
# Get on-demand node
OD_NODE=$(kubectl get nodes -l eks.amazonaws.com/capacityType=ON_DEMAND -o jsonpath='{.items[0].metadata.name}')

# Check NTH logs on on-demand node
kubectl logs -n kube-system -l app=aws-node-termination-handler \
  --field-selector spec.nodeName=$OD_NODE | grep "Detected"

# Expected:
# "Detected on-demand node, starting Spot Guard self-monitor"
```

### **Test 3: Verify ASG Detection**
```bash
# Manually test ASG detection
aws autoscaling describe-auto-scaling-instances \
  --instance-ids $(kubectl get node $OD_NODE -o jsonpath='{.spec.providerID}' | cut -d'/' -f5)

# Should show:
# "AutoScalingGroupName": "eks-ondemand-workers"
```

### **Test 4: Verify Label Detection**
```bash
# Check node labels
kubectl get node $OD_NODE -o jsonpath='{.metadata.labels.eks\.amazonaws\.com/capacityType}'

# Should show:
# ON_DEMAND
```

---

## 🎯 **Key Points**

### **Why ASG Check is Primary:**
1. ✅ **Most Accurate**: Directly from AWS source of truth
2. ✅ **Can't Be Tampered**: Labels can be changed, ASG membership can't
3. ✅ **Works Everywhere**: EKS, Karpenter, self-managed, all work
4. ✅ **Definitive**: No ambiguity

### **Why Label Check is Fallback:**
1. ✅ **Fast**: No AWS API call needed
2. ✅ **Usually Accurate**: EKS/Karpenter set labels correctly
3. ⚠️ **Can Be Wrong**: If labels are manually modified
4. ⚠️ **Requires Labels**: Some clusters may not have them

### **Why Spot is Safe Default:**
1. ✅ **Conservative**: Better to miss monitoring than scale down wrong node
2. ✅ **No Harm**: Spot nodes don't need self-monitoring anyway
3. ✅ **Fail-Safe**: If detection completely fails, nothing bad happens

---

## 📊 **Detection Decision Tree**

```
Pod Starts
    ↓
Can get instance ID from IMDS?
    ├─ YES → Query AWS ASG API
    │        ├─ Success → Compare ASG names
    │        │            ├─ Match on-demand ASG → ON-DEMAND ✅
    │        │            └─ Different ASG → SPOT ✅
    │        └─ Fail → Try node labels ↓
    └─ NO → Try node labels ↓

Can get node from Kubernetes?
    ├─ YES → Check labels
    │        ├─ eks.amazonaws.com/capacityType = "ON_DEMAND" → ON-DEMAND ✅
    │        ├─ eks.amazonaws.com/capacityType = "SPOT" → SPOT ✅
    │        ├─ karpenter.sh/capacity-type != "spot" → ON-DEMAND ✅
    │        ├─ node.kubernetes.io/lifecycle != "spot" → ON-DEMAND ✅
    │        └─ No labels found → Assume SPOT (safe default) ⚠️
    └─ NO → Assume SPOT (safe default) ⚠️
```

---

## 🚀 **Summary**

**The detection approach is:**
1. **Reliable**: Uses AWS API as primary source
2. **Resilient**: Has fallback to Kubernetes labels
3. **Safe**: Defaults to spot (no monitoring) if uncertain
4. **Fast**: Usually completes in < 1 second
5. **Accurate**: Works with EKS, Karpenter, self-managed clusters

**For your 20-node deployment:**
- Each pod independently detects its own node type
- No coordination needed between pods
- Detection happens once at startup
- Result determines if self-monitor starts or not

✅ **Spot nodes**: No self-monitor (just handle scale-up)  
✅ **On-demand nodes**: Start self-monitor (handle scale-down)  

This ensures only on-demand nodes run the monitoring loop, making the system efficient and scalable! 🎯
