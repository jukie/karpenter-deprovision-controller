# Karpenter Deprovision Controller

## Overview
This tool is designed to interact with Kubernetes clusters using the Karpenter autoscaler and assists with node lifecycle management. 
It focuses on managing expired nodes with pods that block deprovisioning, ensuring that blocking annotations are removed from pods to allow for smooth node termination.

## Features
- **Annotation Management**: Removes annotations from pods that block node deprovisioning (`karpenter.sh/do-not-disrupt=true`) during the configured disruption schedule.

## Running locally
1. Clone the repository:
   ```bash
   git clone https://github.com/jukie/karpenter-deprovision-controller
   cd karpenter-deprovision-controller
   go build .
   KUBECONFIG=/path/to/config ./karpenter-deprovision-controller --dry-run=true
   ``` 