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

## Deployment

The service is designed to be deployed to a Kubernetes cluster. Manifests are provided in the [`deploy/kubernetes/`](deploy/kubernetes/05-service.yaml) directory.

1.  **Namespace (Optional):**
    If you want to deploy the autoscaler and its RBAC components into a dedicated namespace (e.g., `buildkitd-scaler-system` as defined in [`00-namespace.yaml`](deploy/kubernetes/00-namespace.yaml:1)), apply it first:
    ```bash
    kubectl apply -f deploy/kubernetes/00-namespace.yaml
    ```
    If you use a different namespace, ensure you update the `RoleBinding` and `Deployment` manifests accordingly.

2.  **Update Image Name:**
    Before applying the deployment manifest, update the image name in [`deploy/kubernetes/04-deployment.yaml`](deploy/kubernetes/04-deployment.yaml:1) to point to the image you built and pushed to your registry:
    ```yaml
    # In deploy/kubernetes/04-deployment.yaml
    spec:
      template:
        spec:
          containers:
          - name: buildkitd-autoscaler
            image: your-repo/buildkitd-autoscaler:your-tag # <-- UPDATE THIS
    ```
    **Important:** For production, use a specific image tag (e.g., `your-repo/buildkitd-autoscaler:v1.0.0`) instead of `latest`.

3.  **Review Configuration in Manifests:**
    *   Ensure the `BUILDKITD_STATEFULSET_NAME`, `BUILDKITD_STATEFULSET_NAMESPACE`, `BUILDKITD_HEADLESS_SERVICE_NAME`, and `BUILDKITD_TARGET_PORT` environment variables in [`deploy/kubernetes/04-deployment.yaml`](deploy/kubernetes/04-deployment.yaml:1) match your `buildkitd` setup.
    *   If you are not using the `buildkitd-scaler-system` namespace, update the namespace in all relevant manifests (ServiceAccount, Role, RoleBinding, Deployment, Service).

4.  **Apply Manifests:**
    Apply all manifests. If you are using the `buildkitd-scaler-system` namespace and have updated the image:
    ```bash
    kubectl apply -R -f deploy/kubernetes/
    ```
    If you are deploying to a different namespace, you might need to apply them individually or adjust the `kubectl apply -R` command after modifying namespaces in the YAML files. For example, if deploying to the `default` namespace (and assuming your buildkitd is also in `default`):
    *   Update `RoleBinding` to reference the `default` namespace for the `ServiceAccount`.
    *   Update `Deployment` and `Service` to reside in the `default` namespace.
    *   Then apply: `kubectl apply -R -f deploy/kubernetes/ -n default` (or your target namespace).

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