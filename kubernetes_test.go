package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNamespace     = "test-ns"
	testStsName       = "test-sts"
	testHeadlessSvc   = "test-headless"
	testBuildkitdPort = "1234"
)

// newTestStatefulSet is a helper function to create a new appsv1.StatefulSet object
// with specified name, namespace, and replica count. It's used for setting up
// test scenarios with fake Kubernetes clients.
func newTestStatefulSet(name, namespace string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas, // Assume current matches desired initially for simplicity
			ReadyReplicas:   replicas, // Assume all replicas are ready initially
			UpdatedReplicas: replicas,
		},
	}
}

// TestGetStatefulSetStatus_Found tests the GetStatefulSetStatus function
// when the StatefulSet exists and is found by the client.
// It verifies that the returned status matches the mock StatefulSet's data.
func TestGetStatefulSetStatus_Found(t *testing.T) {
	clientset := fake.NewSimpleClientset(newTestStatefulSet(testStsName, testNamespace, 3))
	status, err := GetStatefulSetStatus(clientset, testNamespace, testStsName)

	if err != nil {
		t.Fatalf("GetStatefulSetStatus() error = %v, wantErr %v", err, false)
	}
	if status == nil {
		t.Fatal("GetStatefulSetStatus() status is nil")
	}
	if status.DesiredReplicas != 3 {
		t.Errorf("GetStatefulSetStatus() DesiredReplicas = %d, want %d", status.DesiredReplicas, 3)
	}
	if status.ReadyReplicas != 3 {
		t.Errorf("GetStatefulSetStatus() ReadyReplicas = %d, want %d", status.ReadyReplicas, 3)
	}
}

// TestGetStatefulSetStatus_NotFound tests the GetStatefulSetStatus function
// when the specified StatefulSet does not exist.
// It checks that an error is returned and that the error indicates a "not found" condition.
func TestGetStatefulSetStatus_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset() // No objects
	_, err := GetStatefulSetStatus(clientset, testNamespace, testStsName)

	if err == nil {
		t.Fatal("GetStatefulSetStatus() expected an error for not found, got nil")
	}
	if !apierrors.IsNotFound(err) { // Check the original error wrapped
		// The function wraps the error, so we need to check the cause or type.
		// For this test, we'll check if the error message contains "not found"
		// as a simpler check, assuming the function correctly wraps apierrors.IsNotFound.
		// A more robust check would involve errors.Is(err, someSpecificErrorType) if GetStatefulSetStatus returned a custom error type.
		// Or, check the wrapped error directly if possible.
		// For now, let's check the error message.
		expectedMsg := fmt.Sprintf("StatefulSet %s in namespace %s not found", testStsName, testNamespace)
		if err.Error() != expectedMsg+": "+apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, testStsName).Error() {
			t.Errorf("GetStatefulSetStatus() error = %v, want error containing '%s'", err, expectedMsg)
		}
	}
}

// TestScaleStatefulSet_Success tests the ScaleStatefulSet function for a successful scaling operation.
// It uses a fake client with a reactor to simulate a successful patch operation
// and verifies that the returned StatefulSet reflects the updated replica count.
func TestScaleStatefulSet_Success(t *testing.T) {
	initialReplicas := int32(1)
	targetReplicas := int32(3)
	sts := newTestStatefulSet(testStsName, testNamespace, initialReplicas)
	clientset := fake.NewSimpleClientset(sts)

	// Reactor to simulate successful patch
	clientset.PrependReactor("patch", "statefulsets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		patchAction := action.(k8stesting.PatchAction)
		if patchAction.GetName() == testStsName && patchAction.GetNamespace() == testNamespace {
			// Simulate applying the patch
			updatedSts := sts.DeepCopy()
			updatedSts.Spec.Replicas = &targetReplicas  // Simulate the patch effect
			updatedSts.Status.Replicas = targetReplicas // Assume controller updates this too
			updatedSts.Status.ReadyReplicas = targetReplicas
			return true, updatedSts, nil
		}
		return false, nil, fmt.Errorf("unexpected patch action: %+v", action)
	})

	updatedSts, err := ScaleStatefulSet(clientset, testNamespace, testStsName, targetReplicas)
	if err != nil {
		t.Fatalf("ScaleStatefulSet() error = %v, wantErr %v", err, false)
	}
	if updatedSts == nil {
		t.Fatal("ScaleStatefulSet() returned nil StatefulSet")
	}
	if *updatedSts.Spec.Replicas != targetReplicas {
		t.Errorf("ScaleStatefulSet() Spec.Replicas = %d, want %d", *updatedSts.Spec.Replicas, targetReplicas)
	}
}

// TestScaleStatefulSet_Error tests the ScaleStatefulSet function when the Kubernetes API
// returns an error during the patch operation.
// It verifies that the function propagates the error correctly.
func TestScaleStatefulSet_Error(t *testing.T) {
	clientset := fake.NewSimpleClientset(newTestStatefulSet(testStsName, testNamespace, 1))

	// Reactor to simulate an error during patch
	clientset.PrependReactor("patch", "statefulsets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("simulated API error on patch")
	})

	_, err := ScaleStatefulSet(clientset, testNamespace, testStsName, 3)
	if err == nil {
		t.Fatal("ScaleStatefulSet() expected an error, got nil")
	}
	expectedErrMsg := "simulated API error on patch"
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("ScaleStatefulSet() error = %q, want error containing %q", err.Error(), expectedErrMsg)
	}
}

// TestWaitForStatefulSetReady_BecomesReady tests the WaitForStatefulSetReady function
// when the StatefulSet eventually reaches the desired ready replica count.
// It uses a reactor to simulate the StatefulSet's status changing over multiple polls.
func TestWaitForStatefulSetReady_BecomesReady(t *testing.T) {
	sts := newTestStatefulSet(testStsName, testNamespace, 0) // Start with 0 ready
	clientset := fake.NewSimpleClientset(sts)
	expectedReadyReplicas := int32(1)

	// Simulate the StatefulSet becoming ready after a few polls
	pollCount := 0
	clientset.PrependReactor("get", "statefulsets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		pollCount++
		currentSts := sts.DeepCopy()
		if pollCount >= 2 { // Becomes ready on the second GET
			currentSts.Spec.Replicas = &expectedReadyReplicas
			currentSts.Status.Replicas = expectedReadyReplicas
			currentSts.Status.ReadyReplicas = expectedReadyReplicas
		} else { // Still at 0
			currentSts.Spec.Replicas = &expectedReadyReplicas // Controller has updated spec
			currentSts.Status.Replicas = 0
			currentSts.Status.ReadyReplicas = 0
		}
		return true, currentSts, nil
	})

	// Initialize logger for WaitForStatefulSetReady
	// In a real test setup, you might pass a test-specific logger or mock slog.Default()
	logger = slog.New(slog.NewTextHandler(io.Discard, nil)) // Discard logs for this test

	err := WaitForStatefulSetReady(clientset, testNamespace, testStsName, expectedReadyReplicas, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForStatefulSetReady() error = %v, wantErr %v", err, false)
	}
	if pollCount < 2 {
		t.Errorf("Expected at least 2 polls for readiness, got %d", pollCount)
	}
}

// TestWaitForStatefulSetReady_Timeout tests the WaitForStatefulSetReady function
// when the StatefulSet does not become ready within the specified timeout.
// It verifies that a timeout error (context.DeadlineExceeded or similar) is returned.
func TestWaitForStatefulSetReady_Timeout(t *testing.T) {
	sts := newTestStatefulSet(testStsName, testNamespace, 0) // Start with 0, never becomes ready
	sts.Spec.Replicas = int32Ptr(1)                          // Controller desires 1
	clientset := fake.NewSimpleClientset(sts)

	// Reactor to always return 0 ready replicas
	clientset.PrependReactor("get", "statefulsets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		currentSts := sts.DeepCopy()
		// Spec might be 1, but status never reaches ready
		currentSts.Status.Replicas = 0
		currentSts.Status.ReadyReplicas = 0
		return true, currentSts, nil
	})

	logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	err := WaitForStatefulSetReady(clientset, testNamespace, testStsName, 1, 50*time.Millisecond) // Short timeout
	if err == nil {
		t.Fatal("WaitForStatefulSetReady() expected a timeout error, got nil")
	}
	if err != context.DeadlineExceeded && !strings.Contains(err.Error(), "timed out waiting for the condition") {
		// wait.PollImmediate wraps the context.DeadlineExceeded error.
		t.Errorf("WaitForStatefulSetReady() error = %v, want context.DeadlineExceeded or similar timeout error", err)
	}
}

// TestWaitForStatefulSetReady_NotFoundInitiallyThenAppears tests the scenario where
// the StatefulSet is initially not found, but then appears and becomes ready.
// This simulates cases where the StatefulSet is being created during the polling period.
func TestWaitForStatefulSetReady_NotFoundInitiallyThenAppears(t *testing.T) {
	clientset := fake.NewSimpleClientset() // STS doesn't exist initially
	expectedReadyReplicas := int32(1)
	notFoundError := apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, testStsName)

	pollCount := 0
	clientset.PrependReactor("get", "statefulsets", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		pollCount++
		if pollCount == 1 { // First call, not found
			return true, nil, notFoundError
		}
		// Subsequent calls, STS exists and is ready
		stsReady := newTestStatefulSet(testStsName, testNamespace, expectedReadyReplicas)
		return true, stsReady, nil
	})

	logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	err := WaitForStatefulSetReady(clientset, testNamespace, testStsName, expectedReadyReplicas, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForStatefulSetReady() error = %v; want nil. Poll count: %d", err, pollCount)
	}
	if pollCount < 2 {
		t.Errorf("Expected at least 2 polls (1 not found, 1 found & ready), got %d", pollCount)
	}
}

// int32Ptr is a helper function that returns a pointer to an int32 value.
// Useful for setting pointer fields in Kubernetes API objects.
func int32Ptr(i int32) *int32 { return &i }

// Mock homeDir for InitKubeClient tests if needed, or ensure KUBECONFIG_PATH is set.
// For simplicity, InitKubeClient tests are omitted here as they depend heavily on
// file system interaction or in-cluster environment which is harder to unit test
// without significant mocking. The primary focus is on the K8s interaction logic.

// TestInitKubeClient_InCluster (Conceptual - requires more setup for in-cluster mocking)
// func TestInitKubeClient_InCluster(t *testing.T) { ... }

// TestInitKubeClient_OutOfCluster (Conceptual - requires file system mocking or temp files)
// func TestInitKubeClient_OutOfCluster(t *testing.T) {
// 	// Setup: Create a temporary valid kubeconfig file
// 	// Set KUBECONFIG_PATH env var or pass path directly
// 	// Call InitKubeClient
// 	// Assert: clientset is not nil, no error
// 	// Cleanup: Remove temp file
// }

// TestInitKubeClient_Error (Conceptual)
// func TestInitKubeClient_Error(t *testing.T) {
// 	// Setup: Ensure no in-cluster config and provide invalid kubeconfig path
// 	// Call InitKubeClient
// 	// Assert: error is returned
// }

// Note: The global `logger` variable is used by kubernetes.go.
// For tests, it's initialized here to avoid nil pointer dereferences if slog.Default() isn't set up.
// A better approach for testability would be to pass the logger into the functions in kubernetes.go.
// TestMain is used to perform global setup for tests in this package.
// Here, it initializes a default logger to `io.Discard` to prevent panics
// in tested functions that might use the global logger instance if it's not
// otherwise initialized (e.g., if slog.SetDefault hasn't been called).
// This is a workaround for the global logger pattern used in main.go.
// A more robust approach would involve dependency injection for the logger.
func TestMain(m *testing.M) {
	// Setup default logger to avoid panics in tested functions if they call logger directly
	// and it hasn't been initialized (e.g. if slog.SetDefault hasn't been called in main).
	// This is a workaround for the global logger pattern.
	// In a real application, ensure logger is initialized before use or passed as a dependency.
	if logger == nil { // logger is the global var from main.go
		logger = slog.New(slog.NewTextHandler(io.Discard, nil)) // Default to discard for tests
		// slog.SetDefault(logger) // This would set the default for the whole test binary
	}
	os.Exit(m.Run())
}
