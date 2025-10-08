# 🔄 NODE_NAME Environment Variable - Complete Flow

## From Helm Deployment to Application Code

### **Timeline: How NODE_NAME Gets to Your Code**

```
T+0s: User deploys Helm chart
  ↓
T+1s: Kubernetes creates DaemonSet
  ↓
T+2s: DaemonSet creates Pods (one per node)
  ↓
T+3s: Kubernetes Scheduler assigns each pod to a node
  ↓
T+4s: Downward API injects NODE_NAME into container
  ↓
T+5s: Application reads NODE_NAME from environment
  ↓
T+6s: Node detection uses NODE_NAME to query Kubernetes/AWS
```

---

## 📝 **Step 1: Helm Chart Definition**

**File:** `config/helm/aws-node-termination-handler/templates/daemonset.linux.yaml`

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: aws-node-termination-handler
spec:
  template:
    spec:
      containers:
      - name: aws-node-termination-handler
        image: my-nth-image:latest
        env:
          - name: NODE_NAME              ← Define env var
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName  ← Get from pod's spec.nodeName
```

**What this means:**
- "Create an environment variable called `NODE_NAME`"
- "Set its value to whatever `spec.nodeName` is for this pod"
- Kubernetes will fill in `spec.nodeName` when scheduling

---

## 📝 **Step 2: Helm Install**

```bash
$ helm install aws-nth ./config/helm/aws-node-termination-handler \
  --namespace kube-system \
  --set spotGuard.enabled=true
```

**What happens:**
1. Helm renders the template
2. Creates a DaemonSet resource in Kubernetes
3. DaemonSet controller creates one pod per node

---

## 📝 **Step 3: Kubernetes Schedules Pods**

### **For Node: ip-10-0-2-100**

```yaml
# Kubernetes creates this pod spec:
apiVersion: v1
kind: Pod
metadata:
  name: aws-node-termination-handler-xyz123
  namespace: kube-system
  labels:
    app: aws-node-termination-handler
spec:
  nodeName: ip-10-0-2-100  ← Kubernetes sets this during scheduling!
  containers:
  - name: aws-node-termination-handler
    image: my-nth-image:latest
    env:
    - name: NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName  ← Points to line 9!
```

**Key point:** `spec.nodeName` is set by the Kubernetes scheduler when it decides where to place the pod.

---

## 📝 **Step 4: Downward API Injection**

When the container starts, Kubernetes **automatically** resolves the `fieldRef`:

```
1. Container is about to start
2. Kubernetes looks at env definition: fieldPath: spec.nodeName
3. Kubernetes reads pod.spec.nodeName → "ip-10-0-2-100"
4. Kubernetes sets environment variable: NODE_NAME=ip-10-0-2-100
5. Container starts with NODE_NAME already in environment
```

**This happens BEFORE the application code runs!**

---

## 📝 **Step 5: Application Reads Environment**

**File:** `pkg/config/config.go`

```go
flag.StringVar(&config.NodeName, "node-name", getEnv(nodeNameConfigKey, ""), "The kubernetes node name")
```

**File:** `pkg/config/config.go` (helper function)

```go
func getEnv(key, fallback string) string {
    value := os.Getenv(key)  ← Reads from environment
    if value == "" {
        return fallback
    }
    return value
}
```

Where `nodeNameConfigKey` is defined as:

```go
const nodeNameConfigKey = "NODE_NAME"
```

**Result:**
```go
config.NodeName = "ip-10-0-2-100"
```

---

## 🔍 **Real Example: 3 Nodes**

### **Cluster State:**
```bash
$ kubectl get nodes
NAME                                          STATUS   ROLE
ip-10-0-1-50.us-west-2.compute.internal      Ready    <none>   (spot)
ip-10-0-1-51.us-west-2.compute.internal      Ready    <none>   (spot)
ip-10-0-2-100.us-west-2.compute.internal     Ready    <none>   (on-demand)
```

### **DaemonSet Creates 3 Pods:**

```bash
$ kubectl get pods -n kube-system -l app=aws-node-termination-handler -o wide
NAME                                  NODE
aws-node-termination-handler-abc123   ip-10-0-1-50
aws-node-termination-handler-def456   ip-10-0-1-51
aws-node-termination-handler-ghi789   ip-10-0-2-100
```

### **Each Pod Gets Its Own NODE_NAME:**

**Pod 1 (on ip-10-0-1-50):**
```bash
$ kubectl exec -n kube-system aws-node-termination-handler-abc123 -- env | grep NODE_NAME
NODE_NAME=ip-10-0-1-50
```

**Pod 2 (on ip-10-0-1-51):**
```bash
$ kubectl exec -n kube-system aws-node-termination-handler-def456 -- env | grep NODE_NAME
NODE_NAME=ip-10-0-1-51
```

**Pod 3 (on ip-10-0-2-100):**
```bash
$ kubectl exec -n kube-system aws-node-termination-handler-ghi789 -- env | grep NODE_NAME
NODE_NAME=ip-10-0-2-100
```

**Each pod automatically knows which node it's running on!** ✨

---

## 🎯 **Why This is Powerful**

### **Without Downward API:**
```go
// Application would need to:
1. Query Kubernetes API: "What pod am I?"
2. Parse pod spec to find nodeName
3. Handle authentication, errors, etc.

// Complex and error-prone!
```

### **With Downward API:**
```go
// Application just reads environment:
nodeName := os.Getenv("NODE_NAME")

// Simple and reliable!
```

---

## 🧪 **Testing & Verification**

### **Test 1: Check Pod Spec**
```bash
POD=$(kubectl get pods -n kube-system -l app=aws-node-termination-handler -o jsonpath='{.items[0].metadata.name}')

# Check what node the pod is on
kubectl get pod -n kube-system $POD -o jsonpath='{.spec.nodeName}'
# Output: ip-10-0-2-100
```

### **Test 2: Check Environment Variable**
```bash
# Check what NODE_NAME is set to inside the container
kubectl exec -n kube-system $POD -- env | grep NODE_NAME
# Output: NODE_NAME=ip-10-0-2-100

# They match! ✅
```

### **Test 3: Check Downward API Configuration**
```bash
# See the Downward API configuration
kubectl get pod -n kube-system $POD -o yaml | grep -A 5 "NODE_NAME"
```

Output:
```yaml
- name: NODE_NAME
  valueFrom:
    fieldRef:
      apiVersion: v1
      fieldPath: spec.nodeName
```

### **Test 4: Verify in Application Logs**
```bash
# Check NTH logs to see what node name it detected
kubectl logs -n kube-system $POD | grep "nodeName"
```

Output:
```
INFO: Detecting node type
      nodeName=ip-10-0-2-100
      onDemandASG=eks-ondemand-workers
```

---

## 🎭 **What If NODE_NAME Wasn't Set?**

If we didn't use the Downward API, the application would fail:

```go
nodeName := os.Getenv("NODE_NAME")
// nodeName = "" (empty!)

// Later in code:
nodeDetector := spotguard.NewNodeDetector(imds, asgClient, clientset, nodeName)
// nodeName is empty, detection would fail!
```

**The Downward API ensures every pod knows its node name automatically!**

---

## 📊 **Complete Data Flow Diagram**

```
┌─────────────────────────────────────────────────────────────┐
│ 1. Helm Chart (daemonset.linux.yaml)                       │
│    env:                                                      │
│      - name: NODE_NAME                                      │
│        valueFrom:                                            │
│          fieldRef:                                           │
│            fieldPath: spec.nodeName                         │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. Kubernetes Scheduler                                     │
│    "I'll place this pod on ip-10-0-2-100"                  │
│    Sets: spec.nodeName = "ip-10-0-2-100"                   │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. Downward API (Kubernetes)                                │
│    Reads: pod.spec.nodeName = "ip-10-0-2-100"             │
│    Injects: NODE_NAME=ip-10-0-2-100 into container env     │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. Container Environment                                     │
│    NODE_NAME=ip-10-0-2-100                                  │
│    POD_NAME=aws-node-termination-handler-xyz123             │
│    NAMESPACE=kube-system                                     │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 5. Application Code (config.go)                             │
│    nodeName := os.Getenv("NODE_NAME")                      │
│    // nodeName = "ip-10-0-2-100"                           │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 6. Node Detection (node_detector.go)                        │
│    nodeDetector := NewNodeDetector(..., nodeName)          │
│    isOnDemand := nodeDetector.IsOnDemandNode(...)          │
└─────────────────────────────────────────────────────────────┘
```

---

## ✅ **Summary**

**How NODE_NAME gets set:**

1. ✅ **Helm Chart** defines: `fieldPath: spec.nodeName`
2. ✅ **Kubernetes Scheduler** sets: `spec.nodeName = "ip-10-0-2-100"`
3. ✅ **Downward API** injects: `NODE_NAME=ip-10-0-2-100`
4. ✅ **Application** reads: `os.Getenv("NODE_NAME")`

**Key Benefits:**
- ✅ Automatic - no manual configuration needed
- ✅ Reliable - Kubernetes guarantees the value is correct
- ✅ Simple - just read an environment variable
- ✅ Per-pod - each pod gets its own node name
- ✅ DaemonSet-friendly - works perfectly for one pod per node

**This is why DaemonSets work so well for node-specific operations!** 🚀
