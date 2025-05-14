# Buildkitd Stack Helm Chart

This Helm chart deploys a "stack" consisting of:
1.  An instance of [earthly/buildkitd](https://github.com/earthly/buildkitd) as a StatefulSet.
2.  A `buildkitd-autoscaler` service that scales the `buildkitd` StatefulSet from 0-to-1 and 1-to-0 based on TCP connection activity.

This chart combines the functionality of the `o8t-buildkitd` chart and the `buildkitd-autoscaler` chart into a single deployable unit.

## Prerequisites

*   Kubernetes 1.19+
*   Helm 3.2.0+

## Purpose

This chart provides a `buildkitd` instance suitable for CI/CD build pipelines, with the added benefit of automatic scaling to zero when idle to conserve resources. The autoscaler listens for incoming connections, scales up `buildkitd` when the first connection arrives, proxies traffic, and scales `buildkitd` down after a configurable idle period once all connections are closed.

## Chart Structure

The chart is organized with two main components, each configurable under its respective key in the `values.yaml` file:

*   `buildkitd`: Configures the `earthly/buildkitd` StatefulSet, its persistent storage, service, etc. Refer to the original `earthly/buildkitd` chart documentation for details on these values.
*   `autoscaler`: Configures the `buildkitd-autoscaler` deployment, its service, and behavior (e.g., idle timeout).

The autoscaler is automatically configured to target the `buildkitd` instance deployed by this chart.

## Configuration

The primary way to configure the chart is by creating a custom `values.yaml` file or by setting values via the `--set` flag during installation.

Refer to the `values.yaml` file in this chart for all available options. Key sections include:

### Buildkitd Configuration (`buildkitd.*`)

Example:
```yaml
buildkitd:
  replicaCount: 1 # Typically managed by the autoscaler, but initial state can be set.
  image:
    repository: earthly/buildkitd
    tag: "v0.13.1" # Specify desired buildkitd version
  persistence:
    enabled: true
    storageClassName: "gp3"
    size: 50Gi
  service:
    port: 8372 # Port buildkitd listens on
    # ... other buildkitd values
```

### Autoscaler Configuration (`autoscaler.*`)

Example:
```yaml
autoscaler:
  image:
    repository: your-repo/buildkitd-autoscaler # REPLACE with your autoscaler image
    tag: "latest" # REPLACE with your autoscaler image tag
  autoscalerConfig:
    listenAddr: ":8080" # Port the autoscaler proxy will listen on
    idleTimeout: "5m0s" # Time to wait before scaling buildkitd to 0
    # ... other autoscaler config values
  service:
    type: ClusterIP
    port: 8080 # Port for the autoscaler service
    # ... other autoscaler service values
```
**Important:** You MUST configure `autoscaler.image.repository` and `autoscaler.image.tag` to point to your `buildkitd-autoscaler` Docker image.

## Installation

To install the chart with the release name `my-buildkitd-stack`:

```bash
helm install my-buildkitd-stack ./buildkitd-proxy/helm/buildkitd-stack \
  --namespace buildkitd-system \
  --create-namespace \
  -f my-custom-values.yaml # Optional: if you have a custom values file
```

Replace `my-custom-values.yaml` with your own values file if needed.
Clients should then connect to the autoscaler's service (e.g., `my-buildkitd-stack-autoscaler.buildkitd-system.svc.cluster.local:8080`).

## How it Works

1.  Clients connect to the `buildkitd-stack-autoscaler` service.
2.  If `buildkitd` is at 0 replicas, the autoscaler scales the `buildkitd-stack-buildkitd` StatefulSet to 1.
3.  The autoscaler waits for the `buildkitd` pod to be ready and then proxies the connection.
4.  When all connections are closed, an idle timer starts.
5.  If the timer expires, the autoscaler scales `buildkitd` back to 0.