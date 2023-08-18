# Replikator

A simple operator to replicate Kubernetes secrets across namespaces.

Why? Because none of the existing solutions seemed to be able to selectively replicate subkeys, particularly the `ca.crt` of a `kubernetes.io/tls` secret.

## Getting Started

### Prerequisites

* [kapp](https://carvel.dev/kapp/)

### Installing

#### Cert-Manager

```shell
kapp deploy -a cert-manager -f https://github.com/cert-manager/cert-manager/releases/download/v1.12.0/cert-manager.yaml
```

#### Replikator

```shell
kapp deploy -a replikator -f https://github.com/gpu-ninja/replikator/releases/latest/download/replikator.yaml
```

## Usage

Refer to the [examples](./examples) directory for how to use the replicator to replicate a certificate authority secret across namespaces.

For available configuration options, refer to the [constants](./internal/constants/constants.go) file.