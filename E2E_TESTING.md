# End-to-End (E2E) Testing Guide for buildkitd-autoscaler

This document outlines the steps for manual E2E testing of the `buildkitd-autoscaler` service.

## 1. Prerequisites

*   **Docker:** Ensure Docker is installed and running.
*   **Local Kubernetes Cluster:** A local Kubernetes cluster is required. Examples include:
    *   [Kind (Kubernetes in Docker)](https://kind.sigs.k8s.io/)
    *   [Minikube](https://minikube.sigs.k8s.io/docs/start/)
*   **kubectl:** The Kubernetes command-line tool, configured to interact with your local cluster.

## 2. Build the Docker Image

Build the `buildkitd-autoscaler` Docker image with a development tag:

```bash
docker build -t your-repo/buildkitd-autoscaler:dev .
```

Replace `your-repo` with your actual Docker repository or a local-only tag if not pushing.

## 3. Load the Image into the Local Cluster

Load the newly built Docker image into your local Kubernetes cluster.

**For Kind:**

```bash
kind load docker-image your-repo/buildkitd-autoscaler:dev
```

**For Minikube:**

```bash
minikube image load your-repo/buildkitd-autoscaler:dev
```

## 4. Deploy a Mock `buildkitd` StatefulSet

Deploy a mock `buildkitd` service. This example uses `socat` to listen on a port and echo back input, simulating a `buildkitd` instance.

Save the following YAML as `mock-buildkitd.yaml`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mock-buildkitd
  namespace: default # Or your test namespace
spec:
  serviceName: "mock-buildkitd-headless"
  replicas: 0 # Start with 0 replicas
  selector:
    matchLabels:
      app: mock-buildkitd
  template:
    metadata:
      labels:
        app: mock-buildkitd
    spec:
      containers:
      - name: socat
        image: alpine/socat # Using socat as it's versatile
        # Listen on port 1234 and echo back input
        command: ["socat", "TCP4-LISTEN:1234,fork", "EXEC:cat"]
        ports:
        - containerPort: 1234
          name: grpc
---
apiVersion: v1
kind: Service
metadata:
  name: mock-buildkitd-headless
  namespace: default # Or your test namespace
spec:
  clusterIP: None # Headless service
  selector:
    app: mock-buildkitd
  ports:
  - name: grpc
    port: 1234
    targetPort: 1234
```

Apply this manifest to your cluster:

```bash
kubectl apply -f mock-buildkitd.yaml
```

## 5. Update Autoscaler Deployment Configuration

Modify the autoscaler's Kubernetes deployment manifest ([`deploy/kubernetes/04-deployment.yaml`](deploy/kubernetes/04-deployment.yaml:1)) to:
*   Use the test image: `your-repo/buildkitd-autoscaler:dev`
*   Point to the mock `buildkitd` StatefulSet and Service:
    *   `BUILDKITD_STATEFULSET_NAME`: `mock-buildkitd`
    *   `BUILDKITD_HEADLESS_SERVICE_NAME`: `mock-buildkitd-headless`
    *   `BUILDKITD_TARGET_PORT`: `1234`
    *   `BUILDKITD_NAMESPACE`: `default` (or your test namespace)
*   Optionally, adjust `SCALE_DOWN_IDLE_TIMEOUT` for faster testing (e.g., `30s`).

Example snippet from `04-deployment.yaml` (modify the `env` section):
```yaml
# ...
spec:
  template:
    spec:
      containers:
        - name: buildkitd-proxy-autoscaler
          image: your-repo/buildkitd-autoscaler:dev # <-- Update image
          env:
            - name: BUILDKITD_STATEFULSET_NAME
              value: "mock-buildkitd" # <-- Update
            - name: BUILDKITD_NAMESPACE
              value: "default" # <-- Update (if different)
            - name: BUILDKITD_HEADLESS_SERVICE_NAME
              value: "mock-buildkitd-headless" # <-- Update
            - name: BUILDKITD_TARGET_PORT
              value: "1234" # <-- Update
            - name: SCALE_DOWN_IDLE_TIMEOUT
              value: "30s" # Optional: adjust for testing
# ...
```

## 6. Apply Autoscaler Manifests

Apply the autoscaler's Kubernetes manifests. If you have a `kustomization.yaml` in `deploy/kubernetes/`:

```bash
kubectl apply -k deploy/kubernetes/
```

Otherwise, apply them individually or recursively:

```bash
kubectl apply -f deploy/kubernetes/ --recursive
```

Ensure the `buildkitd-proxy-autoscaler` pod is running:
```bash
kubectl get pods -n <namespace-of-autoscaler> -w
```

## 7. Port-Forward to the Autoscaler Service

Port-forward from your local machine to the `buildkitd-proxy-svc` to send test connections. The service is defined in [`deploy/kubernetes/05-service.yaml`](deploy/kubernetes/05-service.yaml:1). Assuming it listens on port 80:

```bash
kubectl port-forward svc/buildkitd-proxy-svc <local_port>:80 -n <namespace-of-autoscaler>
# Example: kubectl port-forward svc/buildkitd-proxy-svc 8080:80 -n buildkitd-proxy
```
Replace `<local_port>` with an available port on your machine (e.g., 8080) and `<namespace-of-autoscaler>` with the namespace where the autoscaler is deployed.

## 8. Test Scale-Up

Open a new terminal and send a TCP connection to the local port you forwarded. You can use `netcat` (`nc`) or `telnet`.

```bash
nc localhost <local_port>
# Example: nc localhost 8080
```

*   **Observe:** The `buildkitd-proxy-autoscaler` logs should indicate a new connection and scaling up.
*   **Verify:** Check the `mock-buildkitd` StatefulSet replicas:
    ```bash
    kubectl get statefulset mock-buildkitd -n default -w # Or your test namespace
    ```
    The replicas should scale from 0 to 1.

## 9. Test Proxying (Basic)

While `nc` is connected, type some text and press Enter. The `socat` container in `mock-buildkitd` is configured to echo it back. You should see your input returned in the `nc` terminal. This confirms basic proxying functionality.

## 10. Test Scale-Down

Close the `nc` connection (e.g., by pressing `Ctrl+C` in the `nc` terminal).

*   **Observe:** The `buildkitd-proxy-autoscaler` logs should indicate the connection closed and the scale-down idle timer starting.
*   **Verify:** After the `SCALE_DOWN_IDLE_TIMEOUT` duration, check the `mock-buildkitd` StatefulSet replicas again:
    ```bash
    kubectl get statefulset mock-buildkitd -n default -w
    ```
    The replicas should scale down from 1 to 0.

## 11. Cleanup

Delete the resources created during testing:

```bash
kubectl delete -f mock-buildkitd.yaml
kubectl delete -k deploy/kubernetes/ # Or delete individual resources
# Or: kubectl delete ns <namespace-of-autoscaler> buildkitd-proxy (if you used a dedicated namespace)
```

If you loaded images into Kind/Minikube and want to remove them:
```bash
# Kind
kind delete cluster --name <your-kind-cluster-name> # If you want to delete the whole cluster
# Or manually remove image from nodes if needed

# Minikube
minikube image rm your-repo/buildkitd-autoscaler:dev
# minikube stop
# minikube delete