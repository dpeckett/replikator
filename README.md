# Replikator

A simple operator to replicate Kubernetes secrets across namespaces.

Why? Because none of the existing solutions seemed to be able to selectively replicate subkeys, particularly the `ca.crt` of a `kubernetes.io/tls` secret.

## Getting Started

### Prerequisites

* [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
* [kapp](https://carvel.dev/kapp/)

### Installing

#### Dependencies

```shell
PROMETHEUS_VERSION="v0.68.0"
CERT_MANAGER_VERSION="v1.12.0"

kapp deploy -y -a prometheus-crds -f "https://github.com/prometheus-operator/prometheus-operator/releases/download/${PROMETHEUS_VERSION}/stripped-down-crds.yaml"
kapp deploy -y -a cert-manager -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
```

#### Operator

```shell
kapp deploy -y -a replikator -f https://github.com/gpu-ninja/replikator/releases/latest/download/replikator.yaml
```

### Secret Replication

#### Replicate a Certificate Authority

```shell
kubectl apply -f examples
```