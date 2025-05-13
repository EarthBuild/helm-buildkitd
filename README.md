# Buildkitd Auto-Scaler Service

## Overview

The `buildkitd-autoscaler` is a TCP proxy and Kubernetes controller designed to automatically scale a `buildkitd` StatefulSet. Its primary purpose is to scale `buildkitd` instances from 0 to 1 when the first TCP connection is received and back from 1 to 0 when all connections are closed and an idle timeout period has elapsed. This helps in optimizing resource usage by running `buildkitd` only when it's actively needed.

## Features

*   **TCP Proxy:** Listens for incoming TCP connections and forwards them to the `buildkitd` service.
*   **Connection Counting:** Actively tracks the number of open TCP connections.
*   **StatefulSet Scaling:**
    *   Scales up the `buildkitd` StatefulSet from 0 to 1 replica upon receiving the first connection.
    *   Scales down the `buildkitd` StatefulSet from 1 to 0 replicas when the last connection closes and an idle timeout expires.
*   **Configurable Idle Timeout:** Allows configuration of the duration to wait after the last connection closes before scaling down.
*   **Kubernetes Integration:** Interacts with the Kubernetes API to manage the `buildkitd` StatefulSet.
*   **Graceful Shutdown:** Handles termination signals to allow active connections to complete before exiting.

## Configuration

The application can be configured using command-line flags or environment variables. Environment variables take precedence over command-line flags.

| Flag                      | Environment Variable                | Description                                     | Default        |
| ------------------------- | ----------------------------------- | ----------------------------------------------- | -------------- |
| `--listen-addr`           | `PROXY_LISTEN_ADDR`                 | Proxy listen address and port                   | `:8080`        |
| `--sts-name`              | `BUILDKITD_STATEFULSET_NAME`        | Name of the buildkitd StatefulSet               | `buildkitd`    |
| `--sts-namespace`         | `BUILDKITD_STATEFULSET_NAMESPACE`   | Namespace of the buildkitd StatefulSet          | `default`      |
| `--headless-service-name` | `BUILDKITD_HEADLESS_SERVICE_NAME` | Name of the buildkitd Headless Service        | `buildkitd-headless` |
| `--target-port`           | `BUILDKITD_TARGET_PORT`             | Target port on buildkitd pods                   | `8273`         |
| `--idle-timeout`          | `SCALE_DOWN_IDLE_TIMEOUT`           | Duration for scale-down idle timer              | `2m0s`         |
| `--kubeconfig`            | `KUBECONFIG_PATH`                   | Path to kubeconfig file (for local development) | (none)         |
| `--ready-wait-timeout`    | `READY_WAIT_TIMEOUT`                | Timeout for waiting for StatefulSet to be ready | `5m0s`         |

*Note on `READY_WAIT_TIMEOUT`: This is not a direct flag but an internal constant (`waitForReadyTimeout` in [`main.go`](main.go:36)) set to 5 minutes. It defines how long the autoscaler will wait for the `buildkitd` StatefulSet to report 1 ready replica after scaling up.*

## Building

### Go Binary

To build the Go binary locally:

```bash
go build .
```

This will produce a `buildkitd-autoscaler` (or `go-buildkitd-proxy` based on `go.mod`) executable in the current directory.

### Docker Image

To build the Docker image:

```bash
docker build -t your-repo/buildkitd-autoscaler:latest .
```

Remember to replace `your-repo` with your actual Docker repository/namespace. It's recommended to use a specific version tag instead of `latest` for production deployments.

## Deployment (Helm Chart)

The service is best deployed to a Kubernetes cluster using the provided Helm chart.

**Prerequisites:**
*   Helm CLI installed.
*   A running Kubernetes cluster.
*   Your Docker image for the autoscaler pushed to a container registry.

**Chart Location:**
The Helm chart is located in the `helm/buildkitd-autoscaler/` directory.

**Installation Steps:**

1.  **Configure Values:**
    *   The primary way to configure the chart is by creating a custom `values.yaml` file or by setting values via the `--set` flag during installation.
    *   Navigate to the chart directory: `cd helm/buildkitd-autoscaler/`
    *   Review `values.yaml` for all available options. Key values you will likely need to customize:
        *   `image.repository`: Set this to your Docker image repository (e.g., `your-dockerhub-username/buildkitd-autoscaler`).
        *   `image.tag`: Set this to the tag of your Docker image (e.g., `v1.0.0` or the specific commit SHA).
        *   `namespaceOverride`: If you want to install the chart into a specific namespace (e.g., `buildkitd-scaler-system`), set this value. If `namespace.create` is true, Helm will attempt to create this namespace.
        *   `autoscalerConfig.buildkitdStatefulSetName`: Name of your target buildkitd StatefulSet.
        *   `autoscalerConfig.buildkitdStatefulSetNamespace`: Namespace where your buildkitd StatefulSet resides. This is important for the autoscaler to find and manage the correct StatefulSet.
        *   `autoscalerConfig.buildkitdHeadlessServiceName`: Name of the headless service for your buildkitd StatefulSet.
        *   `autoscalerConfig.buildkitdTargetPort`: The gRPC port your buildkitd instances listen on.
        *   `service.type`, `service.port`: How the autoscaler proxy itself is exposed.
        *   `resources`: Adjust CPU/memory requests and limits for the autoscaler pod.

2.  **Install the Chart:**
    Once you have your configuration ready (e.g., in a `my-custom-values.yaml` file or as `--set` parameters):
    ```bash
    # Example installation:
    helm install my-buildkitd-autoscaler ./helm/buildkitd-autoscaler \
      --namespace buildkitd-scaler-system \
      --create-namespace \
      -f my-custom-values.yaml # Optional: if you have a custom values file
    ```
    *   Replace `my-buildkitd-autoscaler` with your desired release name.
    *   Replace `buildkitd-scaler-system` with your target namespace.
    *   If not using a custom values file, use `--set` for each parameter you need to override, for example:
        ```bash
        helm install my-buildkitd-autoscaler ./helm/buildkitd-autoscaler \
          --namespace buildkitd-scaler-system \
          --create-namespace \
          --set image.repository=your-repo/buildkitd-autoscaler \
          --set image.tag=v0.1.0 \
          --set autoscalerConfig.buildkitdStatefulSetNamespace=default \
          --set autoscalerConfig.buildkitdStatefulSetName=buildkitd
        ```

**Upgrading the Chart:**
```bash
helm upgrade my-buildkitd-autoscaler ./helm/buildkitd-autoscaler \
  --namespace buildkitd-scaler-system \
  -f my-custom-values.yaml # Or using --set
```

**Uninstalling the Chart:**
```bash
helm uninstall my-buildkitd-autoscaler --namespace buildkitd-scaler-system
```

*(The old manual deployment instructions using raw Kubernetes manifests from `deploy/kubernetes/` are now superseded by the Helm chart. The `deploy/kubernetes/` directory can be removed after confirming the Helm chart is satisfactory.)*

## Usage

Once deployed, the `buildkitd-autoscaler` service (e.g., `buildkitd-proxy-service` as defined in [`deploy/kubernetes/05-service.yaml`](deploy/kubernetes/05-service.yaml:1)) will listen for TCP connections on its configured port (default `:8080`).

*   Clients (e.g., `docker build --builder tcp://<buildkitd-proxy-service-ip>:<port>`) should be configured to connect to this proxy service.
*   When the first client connects, the autoscaler will:
    1.  Scale the target `buildkitd` StatefulSet to 1 replica (if it's currently at 0).
    2.  Wait for the `buildkitd` pod to become ready.
    3.  Proxy the connection to the `buildkitd` pod (e.g., `buildkitd-0.buildkitd-headless.default.svc.cluster.local:8273`).
*   Subsequent connections will be proxied directly as long as at least one `buildkitd` pod is ready.
*   When the last client disconnects, an idle timer (default 2 minutes) starts.
*   If no new connections are made before the timer expires, the autoscaler will scale the `buildkitd` StatefulSet back down to 0 replicas.

For a detailed end-to-end testing scenario, refer to [`E2E_TESTING.md`](E2E_TESTING.md:0).

## Design Notes & Future Enhancements

### Proof of Concept Scope

This service is currently a Proof of Concept (PoC) primarily focused on:
*   Scaling a single `buildkitd` instance (StatefulSet with pod name `buildkitd-0`) from 0-to-1 and 1-to-0.
*   Basic TCP connection counting for triggering scaling events.

### Resource Requests and Limits

The Kubernetes deployment manifest ([`deploy/kubernetes/04-deployment.yaml`](deploy/kubernetes/04-deployment.yaml:1)) includes default resource requests and limits for the autoscaler container:
```yaml
resources:
  requests:
    cpu: "50m"
    memory: "32Mi"
  limits:
    cpu: "100m"
    memory: "64Mi"
```
These are conservative starting values. You may need to monitor the autoscaler's performance under your specific load and adjust these values accordingly.

### Potential Future Enhancements

*   **N-Instance Load Balancing:** Extend the proxy to support scaling to N `buildkitd` instances and distribute load among them (e.g., round-robin, least connections). This would likely involve more sophisticated service discovery and routing.
*   **More Sophisticated Readiness/Liveness Probes:** Implement more detailed health checks for the `buildkitd` instances beyond just pod readiness.
*   **Metrics and Observability:** Expose Prometheus metrics for active connections, scaling events, proxy latency, etc.
*   **Advanced Configuration Options:** More granular control over scaling behavior, timeouts, and Kubernetes interactions.
*   **Horizontal Pod Autoscaler (HPA) Integration:** Explore integration with HPA for more dynamic scaling based on custom metrics if N-instance support is added.
*   **Leader Election for Proxy HA:** If running multiple instances of the autoscaler proxy for HA, implement leader election to ensure only one instance actively manages the scaling of the `buildkitd` StatefulSet.