# Spot Guard Monitor Requirements Analysis

## **Question: Which NTH monitors are necessary for Spot Guard?**

Based on the code analysis, here's the definitive answer:

## **✅ REQUIRED Monitors**

### **1. `enableRebalanceMonitoring: true` - CRITICAL**
- **Why**: Spot Guard is triggered by rebalance recommendation events
- **What it does**: Monitors IMDS for rebalance recommendations
- **Without it**: Spot Guard has no events to track and won't work
- **Code reference**: Lines 206-222 in `cmd/node-termination-handler.go`

### **2. `enableRebalanceDraining: true` - RECOMMENDED**
- **Why**: Handles the actual draining when rebalance events occur
- **What it does**: Processes rebalance events and drains nodes
- **Without it**: Rebalance events won't trigger node draining
- **Impact**: Spot Guard needs rebalance events to be processed

## **❌ NOT REQUIRED for Spot Guard**

### **1. `enableSpotInterruptionDraining: true` - Optional**
- **Why**: Spot Guard doesn't use spot interruption events
- **What it does**: Handles spot instance termination notices
- **Impact**: Can be disabled without affecting Spot Guard

### **2. `enableScheduledEventDraining: true` - Optional**
- **Why**: Spot Guard doesn't use scheduled maintenance events
- **What it does**: Handles scheduled maintenance events
- **Impact**: Can be disabled without affecting Spot Guard

### **3. `enableScheduledEventMonitoring: true` - Optional**
- **Why**: Spot Guard doesn't use scheduled maintenance events
- **What it does**: Monitors for scheduled maintenance events
- **Impact**: Can be disabled without affecting Spot Guard

## **🔍 How Spot Guard Works**

```
1. Rebalance Recommendation Event
   ↓
2. NTH processes rebalance event (enableRebalanceDraining: true)
   ↓
3. Spot Guard records fallback event (enableRebalanceMonitoring: true)
   ↓
4. Background monitor checks spot capacity
   ↓
5. When spot is healthy, scale down on-demand
```

## **📋 Configuration Comparison**

### **Full Configuration (All Monitors)**
```yaml
enableSqsTerminationDraining: false
enableSpotInterruptionDraining: true
enableRebalanceDraining: true
enableScheduledEventDraining: true
enableRebalanceMonitoring: true
enableScheduledEventMonitoring: true
```

### **Minimal Configuration (Spot Guard Only)**
```yaml
enableSqsTerminationDraining: false
enableRebalanceMonitoring: true    # REQUIRED
enableRebalanceDraining: true      # RECOMMENDED
```

## **🎯 Answer to Your Question**

**"Does it always work similar in the Step-by-step process in SPOT_GUARD_INTEGRATION_COMPLETE.md?"**

**YES** - The step-by-step process will work the same way with minimal configuration because:

1. **Step 1**: Rebalance event detection - ✅ Works with `enableRebalanceMonitoring: true`
2. **Step 2**: Fallback event recording - ✅ Works with `enableRebalanceDraining: true`
3. **Step 3**: Background monitoring - ✅ Works independently
4. **Step 4**: Health checks - ✅ Works independently
5. **Step 5**: Scale-down execution - ✅ Works independently

## **🚀 Recommended Configurations**

### **For Testing (Minimal)**
```yaml
enableRebalanceMonitoring: true
enableRebalanceDraining: true
# All others can be false
```

### **For Production (Full)**
```yaml
enableRebalanceMonitoring: true
enableRebalanceDraining: true
enableSpotInterruptionDraining: true
enableScheduledEventDraining: true
enableScheduledEventMonitoring: true
```

## **⚠️ Important Notes**

1. **Spot Guard is independent** of other NTH monitors
2. **Only rebalance events** trigger Spot Guard
3. **Other monitors** (spot interruption, scheduled events) are for different use cases
4. **Minimal configuration** is sufficient for Spot Guard functionality
5. **Full configuration** provides comprehensive node termination handling

## **🧪 Testing Commands**

### **Test Minimal Configuration**
```bash
helm template aws-node-termination-handler . -f SPOT_GUARD_MINIMAL_TEST.yaml
```

### **Verify Required Monitors**
```bash
helm template aws-node-termination-handler . -f SPOT_GUARD_MINIMAL_TEST.yaml | grep -E "(ENABLE_REBALANCE|ENABLE_SPOT_GUARD)"
```

### **Check Spot Guard Environment Variables**
```bash
helm template aws-node-termination-handler . -f SPOT_GUARD_MINIMAL_TEST.yaml | grep -i "SPOT_GUARD"
```

## **✅ Conclusion**

**You can disable all monitors except:**
- `enableRebalanceMonitoring: true` (REQUIRED)
- `enableRebalanceDraining: true` (RECOMMENDED)

**Spot Guard will work exactly the same** with minimal configuration as with full configuration, because it only depends on rebalance events, not on spot interruptions or scheduled events.
