package main

import (
	"context"
	// "flag" // No longer needed here
	"fmt"
	// "os" // No longer needed here
	// "path/filepath" // No longer needed here
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes" // Interface definition

	// "k8s.io/client-go/kubernetes" // Concrete type if needed elsewhere, but interface is preferred for params
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// InitKubeClient initializes and returns a Kubernetes clientset (concrete type).
// The functions using the clientset will accept kubernetes.Interface.
func InitKubeClient(kubeconfigPath string) (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not in cluster, try out-of-cluster config using the provided path
		if kubeconfigPath == "" {
			// If kubeconfigPath is not provided (e.g. empty string from flag default not overridden by env)
			// and in-cluster config failed, this is an error.
			// However, clientcmd.BuildConfigFromFlags handles empty path by trying default locations.
			// For clarity, we could log a warning or rely on clientcmd's behavior.
			// Let's assume clientcmd.BuildConfigFromFlags("", "") will check default paths.
			// If an explicit path was given and failed, that's a clearer error.
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("error building kubeconfig from path %q: %w", kubeconfigPath, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating Kubernetes clientset: %w", err)
	}
	return clientset, nil
}

// StatefulSetStatus holds the replica information for a StatefulSet.
type StatefulSetStatus struct {
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// GetStatefulSetStatus fetches the target buildkitd StatefulSet object and
// returns its replica status.
func GetStatefulSetStatus(clientset kubernetes.Interface, namespace, statefulSetName string) (*StatefulSetStatus, error) {
	sts, err := clientset.AppsV1().StatefulSets(namespace).Get(context.TODO(), statefulSetName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("StatefulSet %s in namespace %s not found: %w", statefulSetName, namespace, err)
		}
		return nil, fmt.Errorf("error getting StatefulSet %s in namespace %s: %w", statefulSetName, namespace, err)
	}

	status := &StatefulSetStatus{
		DesiredReplicas: *sts.Spec.Replicas,
		CurrentReplicas: sts.Status.Replicas,
		ReadyReplicas:   sts.Status.ReadyReplicas,
	}
	return status, nil
}

// ScaleStatefulSet modifies the spec.replicas field of the buildkitd StatefulSet.
func ScaleStatefulSet(clientset kubernetes.Interface, namespace, statefulSetName string, targetReplicas int32) (*appsv1.StatefulSet, error) {
	patchPayload := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, targetReplicas))

	sts, err := clientset.AppsV1().StatefulSets(namespace).Patch(context.TODO(), statefulSetName, types.StrategicMergePatchType, patchPayload, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("error patching StatefulSet %s in namespace %s: %w", statefulSetName, namespace, err)
	}
	return sts, nil
}

// WaitForStatefulSetReady waits for the StatefulSet's status.readyReplicas
// to reach expectedReadyReplicas within the given timeout.
func WaitForStatefulSetReady(clientset kubernetes.Interface, namespace, statefulSetName string, expectedReadyReplicas int32, timeout time.Duration) error {
	return wait.PollImmediate(time.Second*5, timeout, func() (bool, error) {
		status, err := GetStatefulSetStatus(clientset, namespace, statefulSetName)
		if err != nil {
			// If the StatefulSet is not found, we might be in a scale-down-to-zero scenario or creation is delayed.
			// For scale-to-zero, if expected is 0 and it's not found, it could be considered ready.
			// However, the current logic expects GetStatefulSetStatus to return an error if not found.
			// We might need to refine this if IsNotFound should be treated as "0 replicas ready".
			// For now, log the error and continue polling or return error if it's persistent.
			logger.Debug("Polling: Error getting StatefulSet status. Retrying...", "statefulSet", statefulSetName, "namespace", namespace, "error", err)
			// Do not return the error immediately, let PollImmediate retry.
			// If the error is persistent (e.g. auth issues), PollImmediate will eventually time out.
			// If it's a transient "NotFound" during creation, it might resolve.
			return false, nil // Continue polling
		}

		logger.Debug("Polling StatefulSet status",
			"statefulSet", statefulSetName, "namespace", namespace,
			"desiredReplicas", status.DesiredReplicas, "currentReplicas", status.CurrentReplicas, "readyReplicas", status.ReadyReplicas,
			"expectedReadyReplicas", expectedReadyReplicas)

		if status.ReadyReplicas >= expectedReadyReplicas {
			// Additionally, ensure current replicas also match desired, indicating stability post-scaling
			// And that desired replicas match the expected ready replicas (or more, if scaling up beyond 1)
			if status.CurrentReplicas == status.DesiredReplicas && status.ReadyReplicas == status.DesiredReplicas && status.DesiredReplicas >= expectedReadyReplicas {
				logger.Info("StatefulSet is ready.", "statefulSet", statefulSetName, "namespace", namespace, "readyReplicas", status.ReadyReplicas)
				return true, nil // Condition met
			}
			logger.Debug("Polling: StatefulSet ready replicas met, but current or desired not yet stable or matching expected.",
				"statefulSet", statefulSetName, "namespace", namespace,
				"readyReplicas", status.ReadyReplicas, "currentReplicas", status.CurrentReplicas, "desiredReplicas", status.DesiredReplicas)
		}
		return false, nil // Condition not met, continue polling
	})
}
