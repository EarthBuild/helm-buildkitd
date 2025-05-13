package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// InitKubeClient initializes and returns a Kubernetes clientset.
// It prioritizes in-cluster configuration and falls back to out-of-cluster
// configuration using kubeconfig.
func InitKubeClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not in cluster, try out-of-cluster config
		var kubeconfig *string
		if home := homeDir(); home != "" {
			kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		} else {
			kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
		}
		flag.Parse()

		// Use KUBECONFIG env var if set, otherwise use default path
		if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
			kubeconfig = &envKubeconfig
		}

		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("error building kubeconfig: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error creating Kubernetes clientset: %w", err)
	}
	return clientset, nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

// StatefulSetStatus holds the replica information for a StatefulSet.
type StatefulSetStatus struct {
	DesiredReplicas int32
	CurrentReplicas int32
	ReadyReplicas   int32
}

// GetStatefulSetStatus fetches the target buildkitd StatefulSet object and
// returns its replica status.
func GetStatefulSetStatus(clientset *kubernetes.Clientset, namespace, statefulSetName string) (*StatefulSetStatus, error) {
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
func ScaleStatefulSet(clientset *kubernetes.Clientset, namespace, statefulSetName string, targetReplicas int32) (*appsv1.StatefulSet, error) {
	patchPayload := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, targetReplicas))

	sts, err := clientset.AppsV1().StatefulSets(namespace).Patch(context.TODO(), statefulSetName, types.StrategicMergePatchType, patchPayload, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("error patching StatefulSet %s in namespace %s: %w", statefulSetName, namespace, err)
	}
	return sts, nil
}

// WaitForStatefulSetReady waits for the StatefulSet's status.readyReplicas
// to reach expectedReadyReplicas within the given timeout.
func WaitForStatefulSetReady(clientset *kubernetes.Clientset, namespace, statefulSetName string, expectedReadyReplicas int32, timeout time.Duration) error {
	return wait.PollImmediate(time.Second*5, timeout, func() (bool, error) {
		status, err := GetStatefulSetStatus(clientset, namespace, statefulSetName)
		if err != nil {
			// If the StatefulSet is not found, we might be in a scale-down-to-zero scenario or creation is delayed.
			// For scale-to-zero, if expected is 0 and it's not found, it could be considered ready.
			// However, the current logic expects GetStatefulSetStatus to return an error if not found.
			// We might need to refine this if IsNotFound should be treated as "0 replicas ready".
			// For now, log the error and continue polling or return error if it's persistent.
			fmt.Printf("Polling: Error getting StatefulSet status for %s/%s: %v. Retrying...\n", namespace, statefulSetName, err)
			// Do not return the error immediately, let PollImmediate retry.
			// If the error is persistent (e.g. auth issues), PollImmediate will eventually time out.
			// If it's a transient "NotFound" during creation, it might resolve.
			return false, nil // Continue polling
		}

		fmt.Printf("Polling: StatefulSet %s/%s - Desired: %d, Current: %d, Ready: %d (Expecting Ready: %d)\n",
			namespace, statefulSetName, status.DesiredReplicas, status.CurrentReplicas, status.ReadyReplicas, expectedReadyReplicas)

		if status.ReadyReplicas >= expectedReadyReplicas {
			// Additionally, ensure current replicas also match desired, indicating stability post-scaling
			if status.CurrentReplicas == status.DesiredReplicas && status.ReadyReplicas == status.DesiredReplicas {
				fmt.Printf("StatefulSet %s/%s is ready with %d replicas.\n", namespace, statefulSetName, status.ReadyReplicas)
				return true, nil // Condition met
			}
			fmt.Printf("Polling: StatefulSet %s/%s - Ready replicas met (%d), but current (%d) or desired (%d) not yet stable. Continuing...\n",
				namespace, statefulSetName, status.ReadyReplicas, status.CurrentReplicas, status.DesiredReplicas)
		}
		return false, nil // Condition not met, continue polling
	})
}
