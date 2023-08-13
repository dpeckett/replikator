# TLS Replicator

A simple operator to replicate Kubernetes TLS secrets across namespaces.

Why? Because the off the shelf options couldn't seem to do something as simple as replicating only the certificate authority data and not the private key.

## Getting Started

### Prerequisites

* [kapp](https://carvel.dev/kapp/)

### Installing

#### Cert-Manager

```shell
kapp deploy -a cert-manager -f https://github.com/cert-manager/cert-manager/releases/download/v1.12.0/cert-manager.yaml
```

#### TLS Replicator

```shell
kapp deploy -a tls-replicator -f https://github.com/gpu-ninja/tls-replicator/releases/latest/download/tls-replicator.yaml
```

## Usage

Refer to the [examples](./examples) directory for how to use the replicator to replicate a certificate authority secret across namespaces.

For available configuration options, refer to the [constants](./internal/constants/constants.go) file.