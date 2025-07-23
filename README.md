# Buildkitd Auto-Scaler Service

## Overview

The `buildkitd-autoscaler` is a TCP proxy and Kubernetes controller designed to automatically scale-to-zero a
`buildkitd` deployment.

It is primarily build and tested for deployments of
[earthbuild/buildkit](https://github.com/earthbuild/buildkit) but does is generally agnostic and should work
with vanilla [moby/buildkit](https://github.com/moby/buildkit) as well.

It will scale `buildkitd` StatefulSet from 0 to 1 when the first TCP connection is received and back from 1
to 0 when all connections are closed and an idle timeout period has elapsed.

This helps in optimizing resource usage by running `buildkitd` only when it's actively needed while remaining
agnostic to the buildkit client (a CI pipeline or a user connecting via ingress to the buildkit backend).

Since buildkitd is deployed as a `StatefulSet` with a PVC in this chart's design, scaling to zero does not
disrupt the buildkit cache.

## Configuration

The application can be configured using command-line flags or environment variables. Environment variables take precedence over command-line flags.

| Flag                      | Environment Variable                | Description                                     | Default        |
| ------------------------- | ----------------------------------- | ----------------------------------------------- | -------------- |
| `--listen-addr`           | `PROXY_LISTEN_ADDR`                 | Proxy listen address and port                   | `:8080`        |
| `--sts-name`              | `BUILDKITD_STATEFULSET_NAME`        | Name of the buildkitd StatefulSet               | `buildkitd`    |
| `--sts-namespace`         | `BUILDKITD_STATEFULSET_NAMESPACE`   | Namespace of the buildkitd StatefulSet          | `default`      |
| `--headless-service-name` | `BUILDKITD_HEADLESS_SERVICE_NAME` | Name of the buildkitd Headless Service        | `buildkitd-headless` |
| `--target-port`           | `BUILDKITD_TARGET_PORT`             | Target port on buildkitd pods                   | `8372`         |
| `--idle-timeout`          | `SCALE_DOWN_IDLE_TIMEOUT`           | Duration for scale-down idle timer              | `2m0s`         |
| `--kubeconfig`            | `KUBECONFIG_PATH`                   | Path to kubeconfig file (for local development) | (none)         |
| `--ready-wait-timeout`    | `READY_WAIT_TIMEOUT`                | Timeout for waiting for StatefulSet to be ready | `5m0s`         |

*Note on `READY_WAIT_TIMEOUT`: This is not a direct flag but an internal constant (`waitForReadyTimeout` in [`main.go`](main.go:36)) set to 5 minutes. It defines how long the autoscaler will wait for the `buildkitd` StatefulSet to report 1 ready replica after scaling up.*

## Deployment (Helm Chart)

The service is best deployed to a Kubernetes cluster using the provided Helm chart.

**Prerequisites:**

* Helm CLI installed.
* A running Kubernetes cluster.
* Your Docker image for the autoscaler pushed to a container registry.

**Chart Location:**
The Helm chart is located in the `helm/buildkitd-stack/` directory.

**Installation Steps:**

1. **Configure Values:**
    * The primary way to configure the chart is by creating a custom `values.yaml` file or by setting values via the `--set` flag during installation.
    * Navigate to the chart directory: `cd helm/buildkitd-stack/`
    * Review `values.yaml` for all available options. Key configuration sections:

    **Autoscaler Configuration:**
        * `autoscaler.image.repository`: Docker image repository for the autoscaler (e.g., `your-repo/buildkitd-proxy`).
        * `autoscaler.image.tag`: Tag of your autoscaler Docker image.
        * `autoscaler.namespaceOverride`: Target namespace for deployment. If `autoscaler.namespace.create` is true, Helm will create this namespace.
        * `autoscaler.autoscalerConfig.proxyListenAddr`: Proxy listen address and port (default: `:8372`).
        * `autoscaler.autoscalerConfig.scaleDownIdleTimeout`: Duration for scale-down idle timer (default: `2m0s`).
        * `autoscaler.autoscalerConfig.readyWaitTimeout`: Timeout for waiting for buildkitd to become ready (default: `5m0s`).
        * `autoscaler.autoscalerConfig.logLevel`: Log level for the autoscaler (default: `debug`).
        * `autoscaler.service.type`: Service type for the autoscaler (`ClusterIP`, `NodePort`, `LoadBalancer`).
        * `autoscaler.service.port`: External port for the autoscaler service (default: `8372`).
        * `autoscaler.resources`: CPU/memory requests and limits for the autoscaler pod.

    **Buildkitd Configuration:**
        * `buildkitd.replicaCount`: Initial number of replicas (default: `0` for scale-to-zero).
        * `buildkitd.image.repository`: Docker image repository for buildkitd (default: `earthly/buildkitd`).
        * `buildkitd.image.tag`: Tag of the buildkitd Docker image (default: `v0.8.15`).
        * `buildkitd.persistence.enabled`: Enable persistent volume for buildkitd cache (default: `true`).
        * `buildkitd.persistence.size`: Size of the persistent volume (default: `50Gi`).
        * `buildkitd.persistence.storageClassName`: Storage class for the persistent volume.
        * `buildkitd.service.port`: Port for buildkitd gRPC service (default: `8372`).
        * `buildkitd.resources`: CPU/memory requests and limits for buildkitd pods.
        * `buildkitd.podAnnotations`: Pod annotations for buildkitd (useful for Istio integration).
        * `buildkitd.extraEnvVars`: Additional environment variables for buildkitd.
        * `buildkitd.initContainers`: Init containers for buildkitd (e.g., for multi-arch support).
        * `buildkitd.nodeSelector`: Node selection constraints.
        * `buildkitd.tolerations`: Tolerations for pod scheduling.
        * `buildkitd.affinity`: Affinity rules for pod scheduling.

2. **Install the Chart:**
    Once you have your configuration ready (e.g., in a `my-custom-values.yaml` file or as `--set` parameters):

    ```bash
    # Example installation:
    helm install my-buildkitd-autoscaler ./helm/buildkitd-stack \
      --namespace buildkitd-scaler-system \
      --create-namespace \
      -f my-custom-values.yaml # Optional: if you have a custom values file
    ```

    * Replace `my-buildkitd-autoscaler` with your desired release name.
    * Replace `buildkitd-scaler-system` with your target namespace.
    * If not using a custom values file, use `--set` for each parameter you need to override, for example:

        ```bash
        helm install my-buildkitd-autoscaler ./helm/buildkitd-stack \
          --namespace buildkitd-scaler-system \
          --create-namespace \
          --set autoscaler.image.repository=your-repo/buildkitd-proxy \
          --set autoscaler.image.tag=v0.1.0 \
          --set autoscaler.autoscalerConfig.scaleDownIdleTimeout=5m0s \
          --set buildkitd.persistence.size=100Gi
        ```

**Upgrading the Chart:**

```bash
helm upgrade my-buildkitd-autoscaler ./helm/buildkitd-stack \
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

* Clients (e.g., `docker build --builder tcp://<buildkitd-proxy-service-ip>:<port>`) should be configured to connect to this proxy service.
* When the first client connects, the autoscaler will:
    1. Scale the target `buildkitd` StatefulSet to 1 replica (if it's currently at 0).
    2. Wait for the `buildkitd` pod to become ready.
    3. Proxy the connection to the `buildkitd` pod (e.g., `buildkitd-0.buildkitd-headless.default.svc.cluster.local:8372`).
* Subsequent connections will be proxied directly as long as at least one `buildkitd` pod is ready.
* When the last client disconnects, an idle timer (default 2 minutes) starts.
* If no new connections are made before the timer expires, the autoscaler will scale the `buildkitd` StatefulSet back down to 0 replicas.

For a detailed end-to-end testing scenario, refer to [`E2E_TESTING.md`](E2E_TESTING.md:0).

## Usage with Istio - Buildkitd Network Interface Configuration

When running in an Istio-enabled cluster, you may need to configure the sidecar proxy to allow buildkitd to
access the correct network interface (see [this issue](https://phabricator.wikimedia.org/T330433)).

Configure the necessary annotation through the Helm chart values:

```yaml
# values.yaml
buildkitd:
  podAnnotations:
    # Allow buildkitd to access the outside world through the correct interface
    traffic.sidecar.istio.io/kubevirtInterfaces: "cni0"
```

Or set it directly during installation:

```bash
helm install my-buildkitd-autoscaler ./helm/buildkitd-stack \
  --set buildkitd.podAnnotations."traffic\.sidecar\.istio\.io/kubevirtInterfaces"="cni0"
```

To determine the correct interface for your cluster (`cni0` in this example), you can exec into a running buildkitd pod and check the available interfaces:

```bash
kubectl exec -it buildkitd-0 -n <namespace> -c buildkitd -- ip addr
```

Additionally, note that the buildkitd grpc traffic does not work with envoy proxy's strict http2 settings so
it appears to be necessary to handle buildkitd traffic as TCP traffic not HTTP/2 traffic in your istio mesh.

## Development

### Integration

The best way to develop the entire application stack in this repository is to use [tilt](https://tilt.dev/).

Simply:

```bash
tilt up
```

To start everything in your k8s-context-of-choice with automatic reloading of application and chart changes.

### Building

#### Go Binary

To build the Go binary locally:

```bash
go build .
```

This will produce a `buildkitd-autoscaler` (or `go-buildkitd-proxy` based on `go.mod`) executable in the current directory.

#### Docker Image

To build a single multi-platform OCI image for `linux/arm64` and `linux/amd64`:

```bash
earthly +image
```

Remember to replace `your-repo` with your actual Docker repository/namespace. It's recommended to use a specific version tag instead of `latest` for production deployments.

## Design Notes & Future Enhancements

### Proof of Concept Scope

This service is currently a Proof of Concept (PoC) primarily focused on:

* Scaling a single `buildkitd` instance (StatefulSet with pod name `buildkitd-0`) from 0-to-1 and 1-to-0.
* Basic TCP connection counting for triggering scaling events.

### Resource Requests and Limits

Default resource values are not provided to allow deployment in all environments. You will need to monitor the autoscaler's performance under your specific load and adjust these values accordingly.

### Multi-arch Support

In order to enable support for multiple architectures, the node must have QEMU enabled. The easiest way to this is to use [tonistiigi/binfmt](https://github.com/tonistiigi/binfmt). To ensure buildkit is always running on a node where QEMU is enabled, this can be run as an initContainer, e.g.
```yaml

```

### Potential Areas of Future Exploration

* **N-Instance Load Balancing:** Extend the proxy to support scaling to N `buildkitd` instances and distribute load among them (e.g., round-robin, least connections). This would likely involve more sophisticated service discovery and routing.
* **More Sophisticated Readiness/Liveness Probes:** Implement more detailed health checks for the `buildkitd` instances beyond just pod readiness.
* **Metrics and Observability:** Expose Prometheus metrics for active connections, scaling events, proxy latency, etc.
* **Advanced Configuration Options:** More granular control over scaling behavior, timeouts, and Kubernetes interactions.
* **Horizontal Pod Autoscaler (HPA) Integration:** Explore integration with HPA for more dynamic scaling based on custom metrics if N-instance support is added.
* **Leader Election for Proxy HA:** If running multiple instances of the autoscaler proxy for HA, implement leader election to ensure only one instance actively manages the scaling of the `buildkitd` StatefulSet.
