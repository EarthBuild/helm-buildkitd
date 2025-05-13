package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/kubernetes"
)

const (
	listenAddr = ":8080"
	// Default K8s and Buildkitd configuration (hardcoded for now)
	defaultBuildkitdStatefulSetName = "buildkitd"
	defaultBuildkitdNamespace       = "default"
	defaultBuildkitdTargetPort      = "8273" // String for FQDN construction
	defaultBuildkitdHeadlessSvcName = "buildkitd-headless"
	defaultScaleDownIdleTimeout     = 2 * time.Minute
	waitForReadyTimeout             = 5 * time.Minute
)

// Global variables
var (
	kubeClientset            *kubernetes.Clientset
	buildkitdStatefulSetName string
	buildkitdNamespace       string
	buildkitdTargetPort      string
	buildkitdHeadlessSvcName string
	scaleDownIdleTimeout     time.Duration

	activeConnectionCount atomic.Int64
	scaleDownTimer        *time.Timer
	scaleDownTimerMutex   sync.Mutex
)

func main() {
	// Initialize configurable parameters (hardcoded for now)
	buildkitdStatefulSetName = defaultBuildkitdStatefulSetName
	buildkitdNamespace = defaultBuildkitdNamespace
	buildkitdTargetPort = defaultBuildkitdTargetPort
	buildkitdHeadlessSvcName = defaultBuildkitdHeadlessSvcName
	scaleDownIdleTimeout = defaultScaleDownIdleTimeout

	var err error
	kubeClientset, err = InitKubeClient()
	if err != nil {
		log.Fatalf("Failed to initialize Kubernetes client: %v. This service requires K8s.", err)
	}
	log.Println("Successfully initialized Kubernetes client.")

	// Initial check: if buildkitd is scaled to 0, ensure it is.
	// This handles cases where the autoscaler might have crashed and restarted
	// while buildkitd was running. For simplicity, we'll assume it should be 0
	// if no connections are immediately present. A more robust solution might
	// check last known state or rely on existing connections to trigger scale up.
	currentStatus, err := GetStatefulSetStatus(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName)
	if err == nil && currentStatus.ReadyReplicas > 0 && activeConnectionCount.Load() == 0 {
		log.Printf("Initial state: %d ready replicas found with 0 active connections. Initiating scale down to 0.", currentStatus.ReadyReplicas)
		_, scaleErr := ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 0)
		if scaleErr != nil {
			log.Printf("Error during initial scale down: %v", scaleErr)
		} else {
			log.Printf("Successfully scaled down %s/%s to 0 replicas on startup.", buildkitdNamespace, buildkitdStatefulSetName)
		}
	} else if err != nil {
		log.Printf("Could not get initial status for %s/%s: %v. Assuming 0 replicas.", buildkitdNamespace, buildkitdStatefulSetName, err)
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()
	log.Printf("TCP proxy listening on %s for Buildkitd service %s/%s", listenAddr, buildkitdNamespace, buildkitdStatefulSetName)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(clientConn)
	}
}

func handleConnection(clientConn net.Conn) {
	isFirstConnection := activeConnectionCount.Add(1) == 1
	log.Printf("Accepted connection from %s. Active connections: %d", clientConn.RemoteAddr(), activeConnectionCount.Load())

	// Defer closing client connection and decrementing active connections
	defer func() {
		clientConn.Close()
		if activeConnectionCount.Add(-1) == 0 {
			// Last connection closed, start scale-down timer
			scaleDownTimerMutex.Lock()
			if scaleDownTimer != nil {
				scaleDownTimer.Stop() // Stop any existing timer
			}
			log.Printf("Last connection closed. Starting scale-down timer for %v.", scaleDownIdleTimeout)
			scaleDownTimer = time.AfterFunc(scaleDownIdleTimeout, func() {
				scaleDownTimerMutex.Lock()
				scaleDownTimer = nil // Timer has fired
				scaleDownTimerMutex.Unlock()

				if activeConnectionCount.Load() == 0 {
					log.Printf("Scale-down timer fired. Initiating scale down to 0 for %s/%s.", buildkitdNamespace, buildkitdStatefulSetName)
					_, err := ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 0)
					if err != nil {
						log.Printf("Failed to scale down StatefulSet %s/%s to 0: %v", buildkitdNamespace, buildkitdStatefulSetName, err)
					} else {
						log.Printf("Successfully scaled down StatefulSet %s/%s to 0 replicas.", buildkitdNamespace, buildkitdStatefulSetName)
					}
				} else {
					log.Printf("Scale-down timer fired, but active connections (%d) > 0. Scale down aborted.", activeConnectionCount.Load())
				}
			})
			scaleDownTimerMutex.Unlock()
		}
		log.Printf("Closed connection from %s. Active connections: %d", clientConn.RemoteAddr(), activeConnectionCount.Load())
	}()

	// If this is the first connection, cancel any pending scale-down timer
	if isFirstConnection {
		scaleDownTimerMutex.Lock()
		if scaleDownTimer != nil {
			log.Println("First active connection. Cancelling scale-down timer.")
			scaleDownTimer.Stop()
			scaleDownTimer = nil
		}
		scaleDownTimerMutex.Unlock()
	}

	// Determine target address and manage scale-up if needed
	var targetAddr string
	status, err := GetStatefulSetStatus(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName)
	if err != nil {
		log.Printf("Failed to get status for StatefulSet %s/%s: %v. Closing connection.", buildkitdNamespace, buildkitdStatefulSetName, err)
		return // Defer will close clientConn
	}

	log.Printf("StatefulSet %s/%s status: Desired=%d, Current=%d, Ready=%d",
		buildkitdNamespace, buildkitdStatefulSetName, status.DesiredReplicas, status.CurrentReplicas, status.ReadyReplicas)

	if isFirstConnection && status.ReadyReplicas == 0 {
		log.Printf("First connection and 0 ready replicas. Initiating scale up for %s/%s to 1 replica.", buildkitdNamespace, buildkitdStatefulSetName)
		_, err = ScaleStatefulSet(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 1)
		if err != nil {
			log.Printf("Failed to scale StatefulSet %s/%s to 1: %v. Closing connection.", buildkitdNamespace, buildkitdStatefulSetName, err)
			return // Defer will close clientConn
		}
		log.Printf("Successfully initiated scaling for %s/%s. Waiting for 1 ready replica...", buildkitdNamespace, buildkitdStatefulSetName)
		err = WaitForStatefulSetReady(kubeClientset, buildkitdNamespace, buildkitdStatefulSetName, 1, waitForReadyTimeout)
		if err != nil {
			log.Printf("Error waiting for StatefulSet %s/%s to become ready (1 replica): %v. Closing connection.", buildkitdNamespace, buildkitdStatefulSetName, err)
			return // Defer will close clientConn
		}
		log.Printf("StatefulSet %s/%s is ready with 1 replica.", buildkitdNamespace, buildkitdStatefulSetName)
	} else if status.ReadyReplicas == 0 {
		// Not the first connection, but still 0 ready replicas. This implies a problem or a very racy condition.
		// For robustness, we could attempt scale-up, but for now, we log and close.
		// This might happen if scale-down occurred between connections very quickly, or STS is stuck.
		log.Printf("Error: Non-first connection but 0 ready replicas for %s/%s. Waiting for scale-up or manual intervention. Closing connection.", buildkitdNamespace, buildkitdStatefulSetName)
		return // Defer will close clientConn
	}

	// Construct target FQDN for buildkitd-0
	targetAddr = fmt.Sprintf("%s-0.%s.%s.svc.cluster.local:%s",
		buildkitdStatefulSetName,
		buildkitdHeadlessSvcName,
		buildkitdNamespace,
		buildkitdTargetPort)

	log.Printf("Attempting to proxy connection for %s to %s", clientConn.RemoteAddr(), targetAddr)
	targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second) // Added timeout for dialing
	if err != nil {
		log.Printf("Failed to connect to target %s: %v. Closing connection.", targetAddr, err)
		return // Defer will close clientConn
	}
	log.Printf("Successfully connected to target %s for client %s", targetAddr, clientConn.RemoteAddr())
	defer targetConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	copyData := func(dst net.Conn, src net.Conn, connDesc string) {
		defer wg.Done()
		defer dst.Close() // Ensure the other side is closed if this copy finishes/errors
		_, copyErr := io.Copy(dst, src)
		if copyErr != nil && copyErr != io.EOF {
			log.Printf("Error copying data for %s (%s <-> %s): %v", connDesc, src.RemoteAddr(), dst.RemoteAddr(), copyErr)
		}
		// log.Printf("Finished copying for %s (%s <-> %s)", connDesc, src.RemoteAddr(), dst.RemoteAddr())
	}

	go copyData(targetConn, clientConn, fmt.Sprintf("client %s to target %s", clientConn.RemoteAddr(), targetAddr))
	go copyData(clientConn, targetConn, fmt.Sprintf("target %s to client %s", targetAddr, clientConn.RemoteAddr()))

	wg.Wait()
	log.Printf("Data transfer complete for client %s and target %s.", clientConn.RemoteAddr(), targetAddr)
}
