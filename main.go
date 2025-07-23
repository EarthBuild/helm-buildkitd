package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog" // New import
	"net"
	"os"
	"os/signal" // New import
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall" // New import
	"time"

	"k8s.io/client-go/kubernetes"
)

// homeDir returns the home directory for the current user.
// It checks the HOME environment variable first, then USERPROFILE for Windows.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

// Application constants defining default values for configuration parameters.
const (
	// defaultProxyListenAddr is the default address and port the proxy will listen on.
	defaultProxyListenAddr = ":8080"
	// defaultBuildkitdStatefulSetName is the default name of the BuildKitd StatefulSet.
	defaultBuildkitdStatefulSetName = "buildkitd"
	// defaultBuildkitdNamespace is the default Kubernetes namespace for the BuildKitd StatefulSet.
	defaultBuildkitdNamespace = "default"
	// defaultBuildkitdTargetPort is the default target port on BuildKitd pods.
	defaultBuildkitdTargetPort = "8372" // String for FQDN construction
	// defaultBuildkitdHeadlessSvcName is the default name of the BuildKitd headless service.
	defaultBuildkitdHeadlessSvcName = "buildkitd-headless"
	// defaultScaleDownIdleTimeoutStr is the default string representation of the idle timeout before scaling down.
	defaultScaleDownIdleTimeoutStr = "2m0s"
	// waitForReadyTimeout is the duration to wait for the StatefulSet to become ready after scaling.
	waitForReadyTimeout = 5 * time.Minute
)

// Global configuration variables, populated from command-line flags or environment variables.
var (
	// proxyListenAddr is the address and port the proxy listens on.
	proxyListenAddr string
	// buildkitdStatefulSetName is the name of the BuildKitd StatefulSet to manage.
	buildkitdStatefulSetName string
	// buildkitdNamespace is the Kubernetes namespace of the BuildKitd StatefulSet.
	buildkitdNamespace string
	// buildkitdHeadlessSvcName is the name of the BuildKitd headless service used for DNS discovery.
	buildkitdHeadlessSvcName string
	// buildkitdTargetPort is the target port on the BuildKitd pods to proxy connections to.
	buildkitdTargetPort string
	// scaleDownIdleTimeout is the duration of inactivity before scaling down BuildKitd to zero replicas.
	scaleDownIdleTimeout time.Duration
	// kubeconfigPath is the path to the kubeconfig file, used for out-of-cluster development.
	kubeconfigPath string
)

// Global runtime variables used by the application.
var (
	// kubeClientset is the Kubernetes API client.
	kubeClientset *kubernetes.Clientset
	// activeConnectionCount tracks the number of currently active proxied connections.
	activeConnectionCount atomic.Int64
	// scaleDownTimer is a timer that triggers scaling down to zero replicas when no connections are active for scaleDownIdleTimeout.
	scaleDownTimer *time.Timer
	// scaleDownTimerMutex protects access to scaleDownTimer.
	scaleDownTimerMutex sync.Mutex
	// logger is the structured logger for the application.
	logger *slog.Logger // New global logger
	// shutdownWg is a WaitGroup to ensure graceful shutdown of active connections.
	shutdownWg sync.WaitGroup // WaitGroup for graceful shutdown
)

// main is the entry point of the buildkitd-autoscaler application.
// It initializes configuration, sets up the Kubernetes client, starts the TCP proxy listener,
// and handles graceful shutdown.
func main() {
	// Initialize logger
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})) // Using Debug level for more verbose output during dev
	slog.SetDefault(logger)

	// Define flags
	flag.StringVar(&proxyListenAddr, "listen-addr", defaultProxyListenAddr, "Proxy listen address and port (e.g., :8080). Env: PROXY_LISTEN_ADDR")
	flag.StringVar(&buildkitdStatefulSetName, "sts-name", defaultBuildkitdStatefulSetName, "Name of the buildkitd StatefulSet. Env: BUILDKITD_STATEFULSET_NAME")
	flag.StringVar(&buildkitdNamespace, "sts-namespace", defaultBuildkitdNamespace, "Namespace of the buildkitd StatefulSet. Env: BUILDKITD_STATEFULSET_NAMESPACE")
	flag.StringVar(&buildkitdHeadlessSvcName, "headless-service-name", defaultBuildkitdHeadlessSvcName, "Name of the buildkitd Headless Service. Env: BUILDKITD_HEADLESS_SERVICE_NAME")
	flag.StringVar(&buildkitdTargetPort, "target-port", defaultBuildkitdTargetPort, "Target port on buildkitd pods. Env: BUILDKITD_TARGET_PORT")
	scaleDownIdleTimeoutStr := flag.String("idle-timeout", defaultScaleDownIdleTimeoutStr, "Duration for scale-down idle timer (e.g., 2m0s). Env: SCALE_DOWN_IDLE_TIMEOUT")

	defaultKubeconfig := ""
	if home := homeDir(); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	}
	flag.StringVar(&kubeconfigPath, "kubeconfig", defaultKubeconfig, "Path to the kubeconfig file (for out-of-cluster development). Env: KUBECONFIG_PATH")

	flag.Parse()

	// Override with environment variables if set
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
	if err != nil {
		logger.Error("Invalid SCALE_DOWN_IDLE_TIMEOUT value", "value", *scaleDownIdleTimeoutStr, "error", err)
		os.Exit(1)
	}

	logger.Info("Configuration loaded",
		"listenAddr", proxyListenAddr,
		"stsName", buildkitdStatefulSetName,
		"stsNamespace", buildkitdNamespace,
		"headlessSvc", buildkitdHeadlessSvcName,
		"targetPort", buildkitdTargetPort,
		"idleTimeout", scaleDownIdleTimeout,
		"kubeconfig", kubeconfigPath,
	)

	kubeClientset, err = InitKubeClient(kubeconfigPath)
	if err != nil {
		logger.Error("Failed to initialize Kubernetes client. This service requires K8s.", "error", err)
		os.Exit(1)
	}
	logger.Info("Successfully initialized Kubernetes client.")

	// Initial check: if buildkitd is scaled to 0, ensure it is.
	currentStatus, err := GetStatefulSetStatus(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName)
	if err == nil && currentStatus.ReadyReplicas > 0 && activeConnectionCount.Load() == 0 {
		logger.Info("Initial state: ready replicas found with 0 active connections. Initiating scale down to 0.",
			"readyReplicas", currentStatus.ReadyReplicas,
			"statefulSet", buildkitdStatefulSetName,
			"namespace", buildkitdNamespace,
		)
		_, scaleErr := ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 0)
		if scaleErr != nil {
			logger.Error("Error during initial scale down", "error", scaleErr, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
		} else {
			logger.Info("Successfully scaled down StatefulSet to 0 replicas on startup.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
		}
	} else if err != nil {
		logger.Warn("Could not get initial status for StatefulSet. Assuming 0 replicas.", "error", err, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
	}

	listener, err := net.Listen("tcp", proxyListenAddr)
	if err != nil {
		logger.Error("Failed to listen on address", "address", proxyListenAddr, "error", err)
		os.Exit(1)
	}
	// defer listener.Close() // Moved to shutdown logic

	logger.Info("TCP proxy listening", "address", proxyListenAddr, "for_statefulset", buildkitdStatefulSetName, "namespace", buildkitdNamespace)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("Shutdown signal received, initiating graceful shutdown...", "signal", sig.String())

		// 1. Close the listener
		if err := listener.Close(); err != nil {
			logger.Error("Error closing network listener", "error", err)
		}

		// 2. Wait for active connections to finish with a timeout
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancelShutdown()

		done := make(chan struct{})
		go func() {
			logger.Info("Waiting for active connections to close...", "count", activeConnectionCount.Load())
			shutdownWg.Wait() // shutdownWg is incremented for each handleConnection
			close(done)
		}()

		select {
		case <-done:
			logger.Info("All active connections closed gracefully.")
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timeout reached, some connections may have been cut short.", "remaining_connections", activeConnectionCount.Load())
		}

		logger.Info("Graceful shutdown complete.")
		os.Exit(0)
	}()

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			// Check if the error is due to the listener being closed during shutdown
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				logger.Info("Listener closed, shutting down accept loop.")
				break // Exit loop if listener is closed
			}
			logger.Warn("Failed to accept connection", "error", err)
			continue
		}
		shutdownWg.Add(1) // Increment for the new connection
		go handleConnection(clientConn)
	}
	logger.Info("Exited connection accept loop.")
}

// handleConnection manages an incoming client connection.
// It increments the active connection count, potentially scales up buildkitd if it's the first connection
// and buildkitd is at zero replicas, proxies data between the client and the target buildkitd pod,
// and decrements the active connection count upon completion. It also manages the scale-down timer.
func handleConnection(clientConn net.Conn) {
	defer shutdownWg.Done() // Decrement for graceful shutdown when connection handling finishes

	remoteAddrStr := clientConn.RemoteAddr().String()
	currentActive := activeConnectionCount.Add(1)
	isFirstConnection := currentActive == 1

	logger.Debug("Accepted connection", "remoteAddr", remoteAddrStr, "activeConnections", currentActive)

	// Defer closing client connection and decrementing active connections
	defer func() {
		clientConn.Close()
		newActiveCount := activeConnectionCount.Add(-1)
		logger.Debug("Closed connection", "remoteAddr", remoteAddrStr, "activeConnections", newActiveCount)

		if newActiveCount == 0 {
			// Last connection closed, start scale-down timer
			scaleDownTimerMutex.Lock()
			if scaleDownTimer != nil {
				logger.Debug("Stopping existing scale-down timer as a new one will be started or not needed.")
				scaleDownTimer.Stop() // Stop any existing timer
			}
			logger.Info("Last connection closed. Starting scale-down timer.", "duration", scaleDownIdleTimeout)
			scaleDownTimer = time.AfterFunc(scaleDownIdleTimeout, func() {
				scaleDownTimerMutex.Lock()
				scaleDownTimer = nil // Timer has fired
				scaleDownTimerMutex.Unlock()

				if activeConnectionCount.Load() == 0 {
					logger.Info("Scale-down timer fired. Initiating scale down to 0.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
					_, err := ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 0)
					if err != nil {
						logger.Error("Failed to scale down StatefulSet to 0.", "error", err, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
					} else {
						logger.Info("Successfully scaled down StatefulSet to 0 replicas.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
					}
				} else {
					logger.Info("Scale-down timer fired, but active connections exist. Scale down aborted.", "activeConnections", activeConnectionCount.Load())
				}
			})
			scaleDownTimerMutex.Unlock()
		}
	}()

	// If this is the first connection, cancel any pending scale-down timer
	if isFirstConnection {
		scaleDownTimerMutex.Lock()
		if scaleDownTimer != nil {
			logger.Info("First active connection. Cancelling scale-down timer.")
			scaleDownTimer.Stop()
			scaleDownTimer = nil
		}
		scaleDownTimerMutex.Unlock()
	}

	// Determine target address and manage scale-up if needed
	var targetAddr string
	status, err := GetStatefulSetStatus(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName)
	if err != nil {
		logger.Error("Failed to get status for StatefulSet. Closing connection.", "error", err, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace, "remoteAddr", remoteAddrStr)
		return // Defer will close clientConn and decrement WaitGroup
	}

	logger.Debug("StatefulSet status",
		"statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace,
		"desiredReplicas", status.DesiredReplicas, "currentReplicas", status.CurrentReplicas, "readyReplicas", status.ReadyReplicas)

	if isFirstConnection && status.ReadyReplicas == 0 {
		logger.Info("First connection and 0 ready replicas. Initiating scale up to 1 replica.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
		_, err = ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 1)
		if err != nil {
			logger.Error("Failed to scale StatefulSet to 1. Closing connection.", "error", err, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace, "remoteAddr", remoteAddrStr)
			return
		}
		logger.Info("Successfully initiated scaling. Waiting for 1 ready replica...", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
		err = WaitForStatefulSetReady(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 1, waitForReadyTimeout)
		if err != nil {
			logger.Error("Error waiting for StatefulSet to become ready (1 replica). Closing connection.", "error", err, "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace, "remoteAddr", remoteAddrStr)
			return
		}
		logger.Info("StatefulSet is ready with 1 replica.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace)
	} else if status.ReadyReplicas == 0 {
		logger.Error("Non-first connection but 0 ready replicas. Waiting for scale-up or manual intervention. Closing connection.", "statefulSet", buildkitdStatefulSetName, "namespace", buildkitdNamespace, "remoteAddr", remoteAddrStr, "activeConnections", currentActive)
		return
	}

	// Construct target FQDN for buildkitd-0
	targetAddr = fmt.Sprintf("%s-0.%s.%s.svc.cluster.local:%s",
		buildkitdStatefulSetName,
		buildkitdHeadlessSvcName,
		buildkitdNamespace,
		buildkitdTargetPort)

	logger.Debug("Attempting to proxy connection", "remoteAddr", remoteAddrStr, "targetAddr", targetAddr)
	targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		logger.Error("Failed to connect to target. Closing connection.", "targetAddr", targetAddr, "error", err, "remoteAddr", remoteAddrStr)
		return
	}
	logger.Debug("Successfully connected to target", "targetAddr", targetAddr, "remoteAddr", remoteAddrStr)
	defer targetConn.Close()

	var copyWg sync.WaitGroup
	copyWg.Add(2)

	copyData := func(dst net.Conn, src net.Conn, direction string) {
		defer copyWg.Done()
		// It's important NOT to close dst here if src is clientConn, as clientConn.Close is handled by the main defer.
		// Similarly, targetConn.Close is handled by its own defer.
		// Closing here can lead to "use of closed network connection" if the other copy operation is still running.
		// The primary responsibility for closing connections lies with their respective defer statements in handleConnection.

		bytesCopied, copyErr := io.Copy(dst, src)
		logger.Debug("Data copy operation finished.", "direction", direction, "bytesCopied", bytesCopied, "remoteAddr", remoteAddrStr, "targetAddr", targetAddr)
		if copyErr != nil && copyErr != io.EOF {
			// Check if the error is "use of closed network connection", which might be expected if the other side closed.
			if opError, ok := copyErr.(*net.OpError); ok && opError.Err.Error() == "use of closed network connection" {
				logger.Debug("Copy error: use of closed network connection (likely expected).", "direction", direction, "error", copyErr)
			} else {
				logger.Warn("Error copying data.", "direction", direction, "error", copyErr, "remoteAddr", remoteAddrStr, "targetAddr", targetAddr)
			}
		}
		// Attempt to close the write side of the connection to signal the other end if it's a TCPConn
		if tcpDst, ok := dst.(*net.TCPConn); ok {
			tcpDst.CloseWrite()
		}
		if tcpSrc, ok := src.(*net.TCPConn); ok {
			tcpSrc.CloseRead()
		}
	}

	go copyData(targetConn, clientConn, fmt.Sprintf("client_to_target (client: %s, target: %s)", remoteAddrStr, targetAddr))
	go copyData(clientConn, targetConn, fmt.Sprintf("target_to_client (target: %s, client: %s)", targetAddr, remoteAddrStr))

	copyWg.Wait()
	logger.Debug("Data transfer complete.", "remoteAddr", remoteAddrStr, "targetAddr", targetAddr)
}
