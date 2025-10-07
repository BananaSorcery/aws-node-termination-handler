# Spot Interruption Draining Analysis

## **What does `enableSpotInterruptionDraining: true` do?**

Based on the code analysis, here's exactly what this configuration does:

## **🔍 Core Functionality**

### **1. Monitors IMDS for Spot Interruption Termination Notices (ITNs)**
- **IMDS Path**: `/latest/meta-data/spot/instance-action`
- **Purpose**: Detects when AWS is about to reclaim a spot instance
- **Frequency**: Checks every 2 seconds via the main monitoring loop

### **2. Creates SpotInterruptionMonitor**
```go
// From cmd/node-termination-handler.go:194-197
if nthConfig.EnableSpotInterruptionDraining {
    imdsSpotMonitor := spotitn.NewSpotInterruptionMonitor(imds, interruptionChan, cancelChan, nthConfig.NodeName)
    monitoringFns[spotITN] = imdsSpotMonitor
}
```

### **3. Processes Spot ITN Events**
When a spot interruption is detected:
1. **Creates InterruptionEvent** with:
   - `Kind: "SPOT_ITN"`
   - `Monitor: "SPOT_ITN_MONITOR"`
   - `EventID: "spot-itn-{hash}"`
   - `StartTime: {interruption_time}`
   - `Description: "Spot ITN received. Instance will be interrupted at {time}"`

2. **Applies PreDrainTask**: `setInterruptionTaint`
   - Taints the node with `aws-node-termination-handler/spot-itn: {eventID}`
   - Effect: `NoSchedule` (prevents new pods from being scheduled)

3. **Triggers Node Draining**:
   - Cordons the node (marks as unschedulable)
   - Drains pods from the node
   - Evicts pods gracefully with termination grace period

## **📊 IMDS Data Structure**

### **Spot ITN Response Format**
```json
{
  "action": "terminate",
  "time": "2024-01-15T10:30:00Z"
}
```

### **Event Processing Flow**
```
1. IMDS Query: GET /latest/meta-data/spot/instance-action
   ↓
2. Parse JSON: {action: "terminate", time: "2024-01-15T10:30:00Z"}
   ↓
3. Create Event: SpotITNKind with interruption time
   ↓
4. Apply Taint: aws-node-termination-handler/spot-itn: {eventID}
   ↓
5. Drain Node: Cordon + Drain + Pod Eviction
```

## **🆚 Comparison: Spot Interruption vs Rebalance Recommendation**

| Aspect | Spot Interruption | Rebalance Recommendation |
|--------|-------------------|---------------------------|
| **IMDS Path** | `/latest/meta-data/spot/instance-action` | `/latest/meta-data/events/recommendations/rebalance` |
| **Event Kind** | `SPOT_ITN` | `REBALANCE_RECOMMENDATION` |
| **Timing** | **Immediate** - Instance will be terminated | **Early Warning** - Instance may be interrupted |
| **Action** | `terminate` | `rebalance` |
| **Urgency** | **High** - Must drain immediately | **Medium** - Can plan ahead |
| **Use Case** | Spot instance termination | Capacity rebalancing |

## **🔧 Code Implementation Details**

### **SpotInterruptionMonitor.Monitor()**
```go
func (m SpotInterruptionMonitor) Monitor() error {
    interruptionEvent, err := m.checkForSpotInterruptionNotice()
    if err != nil {
        return err
    }
    if interruptionEvent != nil && interruptionEvent.Kind == monitor.SpotITNKind {
        m.InterruptionChan <- *interruptionEvent
    }
    return nil
}
```

### **checkForSpotInterruptionNotice()**
```go
func (m SpotInterruptionMonitor) checkForSpotInterruptionNotice() (*monitor.InterruptionEvent, error) {
    instanceAction, err := m.IMDS.GetSpotITNEvent()
    // ... error handling ...
    
    return &monitor.InterruptionEvent{
        EventID:      fmt.Sprintf("spot-itn-%x", hash.Sum(nil)),
        Kind:         monitor.SpotITNKind,
        Monitor:      SpotITNMonitorKind,
        StartTime:    interruptionTime,
        NodeName:     nodeName,
        Description:  fmt.Sprintf("Spot ITN received. Instance will be interrupted at %s \n", instanceAction.Time),
        PreDrainTask: setInterruptionTaint,
    }, nil
}
```

### **setInterruptionTaint()**
```go
func setInterruptionTaint(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
    err := n.TaintSpotItn(interruptionEvent.NodeName, interruptionEvent.EventID)
    if err != nil {
        return fmt.Errorf("Unable to taint node with taint %s:%s: %w", node.SpotInterruptionTaint, interruptionEvent.EventID, err)
    }
    return nil
}
```

## **🎯 When to Use Spot Interruption Draining**

### **✅ Use When:**
- Running **spot instances** that can be terminated
- Need **immediate response** to spot interruptions
- Want **graceful pod eviction** before instance termination
- Using **spot instances for cost optimization**

### **❌ Don't Use When:**
- Running **only on-demand instances**
- Using **SQS mode** (conflicts with IMDS mode)
- Don't need spot interruption handling

## **🔗 Relationship to Spot Guard**

### **Spot Interruption vs Spot Guard**
- **Spot Interruption**: Handles **immediate** spot instance termination
- **Spot Guard**: Handles **proactive** on-demand scale-down when spot capacity is restored

### **They Work Together**
```
1. Spot Interruption: Handles spot instance termination
   ↓
2. Rebalance Recommendation: Triggers fallback to on-demand
   ↓
3. Spot Guard: Monitors spot capacity and scales down on-demand when spot is healthy
```

## **📋 Configuration Examples**

### **Minimal Spot Interruption Only**
```yaml
enableSqsTerminationDraining: false
enableSpotInterruptionDraining: true
# All others can be false
```

### **Full Spot Handling (Interruption + Guard)**
```yaml
enableSqsTerminationDraining: false
enableSpotInterruptionDraining: true
enableRebalanceMonitoring: true
enableRebalanceDraining: true
spotGuard:
  enabled: true
```

## **🧪 Testing Commands**

### **Test Spot Interruption Configuration**
```bash
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -i "ENABLE_SPOT_INTERRUPTION"
```

### **Verify Monitor Creation**
```bash
helm template aws-node-termination-handler . -f SPOT_GUARD_IMDS_TEST.yaml | grep -A 5 -B 5 "SPOT_ITN"
```

## **⚠️ Important Notes**

1. **IMDS Mode Only**: Only works when `enableSqsTerminationDraining: false`
2. **Spot Instances Only**: Only relevant for spot instances
3. **Immediate Action**: Triggers immediate draining when ITN is received
4. **Taint Application**: Automatically taints nodes to prevent new pod scheduling
5. **Graceful Eviction**: Respects pod termination grace periods

## **✅ Summary**

`enableSpotInterruptionDraining: true` enables NTH to:
- **Monitor** IMDS for spot interruption termination notices
- **Detect** when AWS is about to reclaim a spot instance
- **Taint** the node to prevent new pod scheduling
- **Drain** the node gracefully before termination
- **Evict** pods with proper grace periods

This is **essential** for running spot instances in production, as it ensures graceful handling of spot interruptions without data loss or service disruption.


