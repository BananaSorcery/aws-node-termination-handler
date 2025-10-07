# EKS Spot Guard - AWS Node Termination Handler Enhancement

This repository contains an enhanced version of the [AWS Node Termination Handler](https://github.com/aws/aws-node-termination-handler) with automatic ASG scaling capabilities for spot instance rebalance recommendations.

## What's Enhanced

This fork adds the ability to:
- **Automatically scale up spot instances** when receiving rebalance recommendations
- **Fallback to on-demand instances** if spot capacity is unavailable  
- **Only taint nodes after successful scaling** to ensure pod migration
- **Coordinate scaling and tainting** in the correct sequence

## Documentation

- [Spot Guard Flow Documentation](docs/SPOT_GUARD_FLOW.md) - Detailed implementation flow and architecture
- [Original AWS NTH Documentation](README.md) - Base functionality documentation

## Key Requirements

1. **Scale First, Taint Later**: Ensure new instances are ready before tainting the terminating node
2. **Spot Instance Scaling**: Scale up spot ASG when rebalance recommendation is received
3. **On-Demand Fallback**: Scale on-demand ASG if spot capacity is unavailable
4. **Validation**: Wait for new instances to reach "InService" state before proceeding

## Quick Start

1. Review the [Spot Guard Flow](docs/SPOT_GUARD_FLOW.md)
2. Configure your ASG names and scaling parameters
3. Deploy using the provided Helm charts

## Development Context

This enhancement addresses the critical timing issue where nodes are tainted before replacement instances are ready, causing pod scheduling problems during spot instance termination.

## Original Repository

Based on: https://github.com/aws/aws-node-termination-handler