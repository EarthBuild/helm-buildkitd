package main

import (
	"flag"
	"os"
	"strings"
	"testing"
	"time"
)

// Helper function to reset flags and environment variables for testing
func resetFlagsAndEnv(t *testing.T) {
	t.Helper()
	// Reset standard command-line arguments
	os.Args = []string{os.Args[0]} // Keep the program name

	// Create a new flag set for the test to avoid conflicts with other tests
	// or global flag state. Note: This doesn't easily allow testing the global
	// flag variables directly unless they are re-registered or main's flag registration
	// is refactored. For this test, we'll assume we are testing the logic of
	// how os.Getenv and flag defaults interact, rather than flag.Parse() itself on globals.

	// Clear environment variables set by tests
	os.Unsetenv("PROXY_LISTEN_ADDR")
	os.Unsetenv("BUILDKITD_STATEFULSET_NAME")
	os.Unsetenv("BUILDKITD_STATEFULSET_NAMESPACE")
	os.Unsetenv("BUILDKITD_HEADLESS_SERVICE_NAME")
	os.Unsetenv("BUILDKITD_TARGET_PORT")
	os.Unsetenv("SCALE_DOWN_IDLE_TIMEOUT")
	os.Unsetenv("KUBECONFIG_PATH")

	// Reset global config vars to their zero values or known state before each test run
	// This is important because flags might have been parsed in previous tests or main()
	proxyListenAddr = ""
	buildkitdStatefulSetName = ""
	buildkitdNamespace = ""
	buildkitdHeadlessSvcName = ""
	buildkitdTargetPort = ""
	scaleDownIdleTimeout = 0
	kubeconfigPath = "" // Assuming defaultKubeconfig logic will repopulate if necessary
}

// This is a simplified version of the config loading logic from main()
// It's hard to test main() directly due to its side effects (listening, k8s client init).
// This helper simulates the core config loading part.
func loadConfigForTest(testArgs []string) error {
	// Create a new FlagSet for this specific test execution
	fs := flag.NewFlagSet("testConfig", flag.ContinueOnError)

	// Register flags with the new FlagSet, using the global variables
	// This means the global variables will be populated by fs.Parse()
	fs.StringVar(&proxyListenAddr, "listen-addr", defaultProxyListenAddr, "Proxy listen address")
	fs.StringVar(&buildkitdStatefulSetName, "sts-name", defaultBuildkitdStatefulSetName, "Buildkitd StatefulSet name")
	fs.StringVar(&buildkitdNamespace, "sts-namespace", defaultBuildkitdNamespace, "Buildkitd StatefulSet namespace")
	fs.StringVar(&buildkitdHeadlessSvcName, "headless-service-name", defaultBuildkitdHeadlessSvcName, "Buildkitd Headless Service name")
	fs.StringVar(&buildkitdTargetPort, "target-port", defaultBuildkitdTargetPort, "Buildkitd target port")
	scaleDownIdleTimeoutStr := fs.String("idle-timeout", defaultScaleDownIdleTimeoutStr, "Scale-down idle timer duration")
	// kubeconfigPath needs special handling for default as in main.go
	defaultKubeconfigTest := ""
	if home := homeDir(); home != "" {
		// Assuming homeDir() is accessible and works as expected.
		// For a pure unit test, homeDir might need to be mocked or its behavior controlled.
		// For simplicity here, we call it directly.
		defaultKubeconfigTest = home + "/.kube/config" // Simplified, actual uses filepath.Join
	}
	fs.StringVar(&kubeconfigPath, "kubeconfig", defaultKubeconfigTest, "Path to kubeconfig")

	// Parse the test arguments
	if err := fs.Parse(testArgs); err != nil {
		return err
	}

	// Environment variable overrides (mirroring main.go logic)
	if envVal := os.Getenv("PROXY_LISTEN_ADDR"); envVal != "" {
		proxyListenAddr = envVal
	}
	if envVal := os.Getenv("BUILDKITD_STATEFULSET_NAME"); envVal != "" {
		buildkitdStatefulSetName = envVal
	}
	if envVal := os.Getenv("BUILDKITD_STATEFULSET_NAMESPACE"); envVal != "" {
		buildkitdNamespace = envVal
	}
	if envVal := os.Getenv("BUILDKITD_HEADLESS_SERVICE_NAME"); envVal != "" {
		buildkitdHeadlessSvcName = envVal
	}
	if envVal := os.Getenv("BUILDKITD_TARGET_PORT"); envVal != "" {
		buildkitdTargetPort = envVal
	}
	if envVal := os.Getenv("SCALE_DOWN_IDLE_TIMEOUT"); envVal != "" {
		*scaleDownIdleTimeoutStr = envVal
	}
	if envVal := os.Getenv("KUBECONFIG_PATH"); envVal != "" {
		kubeconfigPath = envVal
	}

	var err error
	scaleDownIdleTimeout, err = time.ParseDuration(*scaleDownIdleTimeoutStr)
	return err
}

func TestConfigLoading_Defaults(t *testing.T) {
	resetFlagsAndEnv(t)

	if err := loadConfigForTest([]string{}); err != nil {
		t.Fatalf("loadConfigForTest failed: %v", err)
	}

	if proxyListenAddr != defaultProxyListenAddr {
		t.Errorf("Expected proxyListenAddr to be %s, got %s", defaultProxyListenAddr, proxyListenAddr)
	}
	if buildkitdStatefulSetName != defaultBuildkitdStatefulSetName {
		t.Errorf("Expected buildkitdStatefulSetName to be %s, got %s", defaultBuildkitdStatefulSetName, buildkitdStatefulSetName)
	}
	if buildkitdNamespace != defaultBuildkitdNamespace {
		t.Errorf("Expected buildkitdNamespace to be %s, got %s", defaultBuildkitdNamespace, buildkitdNamespace)
	}
	if buildkitdHeadlessSvcName != defaultBuildkitdHeadlessSvcName {
		t.Errorf("Expected buildkitdHeadlessSvcName to be %s, got %s", defaultBuildkitdHeadlessSvcName, buildkitdHeadlessSvcName)
	}
	if buildkitdTargetPort != defaultBuildkitdTargetPort {
		t.Errorf("Expected buildkitdTargetPort to be %s, got %s", defaultBuildkitdTargetPort, buildkitdTargetPort)
	}
	expectedDefaultTimeout, _ := time.ParseDuration(defaultScaleDownIdleTimeoutStr)
	if scaleDownIdleTimeout != expectedDefaultTimeout {
		t.Errorf("Expected scaleDownIdleTimeout to be %v, got %v", expectedDefaultTimeout, scaleDownIdleTimeout)
	}
	// Kubeconfig default is environment-dependent, harder to assert precisely without mocking homeDir
	// We can check it's not empty if a home dir was likely found.
	if os.Getenv("HOME") != "" || os.Getenv("USERPROFILE") != "" {
		if kubeconfigPath == "" {
			t.Error("Expected kubeconfigPath to be set by default, got empty")
		}
		// A more specific check could be `strings.Contains(kubeconfigPath, ".kube/config")`
		// but this depends on homeDir() implementation details.
	} else {
		// If no home dir, defaultKubeconfigTest would be "", so kubeconfigPath should be ""
		if kubeconfigPath != "" {
			t.Errorf("Expected kubeconfigPath to be empty when no home dir, got %s", kubeconfigPath)
		}
	}
}

func TestConfigLoading_Flags(t *testing.T) {
	resetFlagsAndEnv(t)

	testArgs := []string{
		"-listen-addr=:9090",
		"-sts-name=my-buildkitd",
		"-sts-namespace=test-ns",
		"-headless-service-name=my-headless",
		"-target-port=1235",
		"-idle-timeout=5m",
		"-kubeconfig=/tmp/test-kubeconfig",
	}

	if err := loadConfigForTest(testArgs); err != nil {
		t.Fatalf("loadConfigForTest failed: %v", err)
	}

	if proxyListenAddr != ":9090" {
		t.Errorf("Expected proxyListenAddr to be :9090, got %s", proxyListenAddr)
	}
	if buildkitdStatefulSetName != "my-buildkitd" {
		t.Errorf("Expected buildkitdStatefulSetName to be my-buildkitd, got %s", buildkitdStatefulSetName)
	}
	if buildkitdNamespace != "test-ns" {
		t.Errorf("Expected buildkitdNamespace to be test-ns, got %s", buildkitdNamespace)
	}
	if buildkitdHeadlessSvcName != "my-headless" {
		t.Errorf("Expected buildkitdHeadlessSvcName to be my-headless, got %s", buildkitdHeadlessSvcName)
	}
	if buildkitdTargetPort != "1235" {
		t.Errorf("Expected buildkitdTargetPort to be 1235, got %s", buildkitdTargetPort)
	}
	expectedTimeout, _ := time.ParseDuration("5m")
	if scaleDownIdleTimeout != expectedTimeout {
		t.Errorf("Expected scaleDownIdleTimeout to be %v, got %v", expectedTimeout, scaleDownIdleTimeout)
	}
	if kubeconfigPath != "/tmp/test-kubeconfig" {
		t.Errorf("Expected kubeconfigPath to be /tmp/test-kubeconfig, got %s", kubeconfigPath)
	}
}

func TestConfigLoading_EnvVars(t *testing.T) {
	resetFlagsAndEnv(t)

	os.Setenv("PROXY_LISTEN_ADDR", ":9999")
	os.Setenv("BUILDKITD_STATEFULSET_NAME", "env-buildkitd")
	os.Setenv("BUILDKITD_STATEFULSET_NAMESPACE", "env-ns")
	os.Setenv("BUILDKITD_HEADLESS_SERVICE_NAME", "env-headless")
	os.Setenv("BUILDKITD_TARGET_PORT", "4321")
	os.Setenv("SCALE_DOWN_IDLE_TIMEOUT", "10m")
	os.Setenv("KUBECONFIG_PATH", "/env/kubeconfig")

	if err := loadConfigForTest([]string{}); err != nil {
		t.Fatalf("loadConfigForTest failed: %v", err)
	}

	if proxyListenAddr != ":9999" {
		t.Errorf("Expected proxyListenAddr to be :9999, got %s", proxyListenAddr)
	}
	if buildkitdStatefulSetName != "env-buildkitd" {
		t.Errorf("Expected buildkitdStatefulSetName to be env-buildkitd, got %s", buildkitdStatefulSetName)
	}
	if buildkitdNamespace != "env-ns" {
		t.Errorf("Expected buildkitdNamespace to be env-ns, got %s", buildkitdNamespace)
	}
	if buildkitdHeadlessSvcName != "env-headless" {
		t.Errorf("Expected buildkitdHeadlessSvcName to be env-headless, got %s", buildkitdHeadlessSvcName)
	}
	if buildkitdTargetPort != "4321" {
		t.Errorf("Expected buildkitdTargetPort to be 4321, got %s", buildkitdTargetPort)
	}
	expectedTimeout, _ := time.ParseDuration("10m")
	if scaleDownIdleTimeout != expectedTimeout {
		t.Errorf("Expected scaleDownIdleTimeout to be %v, got %v", expectedTimeout, scaleDownIdleTimeout)
	}
	if kubeconfigPath != "/env/kubeconfig" {
		t.Errorf("Expected kubeconfigPath to be /env/kubeconfig, got %s", kubeconfigPath)
	}
}

func TestConfigLoading_EnvVarOverridesFlag(t *testing.T) {
	resetFlagsAndEnv(t)

	// Set flags
	testArgs := []string{
		"-listen-addr=:9090",
		"-sts-name=flag-buildkitd",
		"-idle-timeout=5m",
	}

	// Set environment variables that should override flags
	os.Setenv("PROXY_LISTEN_ADDR", ":9999")                  // Env overrides flag
	os.Setenv("BUILDKITD_STATEFULSET_NAME", "env-buildkitd") // Env overrides flag
	os.Setenv("SCALE_DOWN_IDLE_TIMEOUT", "10m")              // Env overrides flag

	// This flag is not overridden by env
	os.Setenv("BUILDKITD_STATEFULSET_NAMESPACE", defaultBuildkitdNamespace) // To ensure it takes default if not set by flag/env

	if err := loadConfigForTest(testArgs); err != nil {
		t.Fatalf("loadConfigForTest failed: %v", err)
	}

	if proxyListenAddr != ":9999" { // Env should win
		t.Errorf("Expected proxyListenAddr to be :9999 (env override), got %s", proxyListenAddr)
	}
	if buildkitdStatefulSetName != "env-buildkitd" { // Env should win
		t.Errorf("Expected buildkitdStatefulSetName to be env-buildkitd (env override), got %s", buildkitdStatefulSetName)
	}
	expectedTimeout, _ := time.ParseDuration("10m") // Env should win
	if scaleDownIdleTimeout != expectedTimeout {
		t.Errorf("Expected scaleDownIdleTimeout to be %v (env override), got %v", expectedTimeout, scaleDownIdleTimeout)
	}
	// Check a flag that wasn't overridden by env (should take its default or flag value)
	if buildkitdNamespace != defaultBuildkitdNamespace { // Should be default as not set by flag in this test case and env set to default
		t.Errorf("Expected buildkitdNamespace to be %s (default), got %s", defaultBuildkitdNamespace, buildkitdNamespace)
	}
}

func TestConfigLoading_InvalidIdleTimeout(t *testing.T) {
	resetFlagsAndEnv(t)
	testArgs := []string{"-idle-timeout=invalid"}
	err := loadConfigForTest(testArgs)
	if err == nil {
		t.Errorf("Expected error for invalid idle-timeout, got nil")
	} else {
		if !strings.Contains(err.Error(), "time: invalid duration") {
			t.Errorf("Expected error message to contain 'time: invalid duration', got '%v'", err)
		}
	}

	resetFlagsAndEnv(t)
	os.Setenv("SCALE_DOWN_IDLE_TIMEOUT", "alsoinvalid")
	err = loadConfigForTest([]string{})
	if err == nil {
		t.Errorf("Expected error for invalid SCALE_DOWN_IDLE_TIMEOUT env var, got nil")
	} else {
		if !strings.Contains(err.Error(), "time: invalid duration") { // Corrected .Rrror() to .Error()
			t.Errorf("Expected error message for env to contain 'time: invalid duration', got '%v'", err)
		}
	}
}

// Note: Testing the homeDir() function itself, especially its cross-platform behavior
// and interaction with os.Getenv("HOME") / os.Getenv("USERPROFILE"), would require
// more complex mocking of os.Getenv. For these config tests, we assume homeDir()
// works as intended or rely on the actual environment.
