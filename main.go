package main

import (
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time" // Added for Kubernetes operations timeout
)

const (
	listenAddr    = ":8080"
	targetSvcAddr = "localhost:8273" // Placeholder, will be configurable

	// Placeholder K8s values for testing
	kubeNamespace     = "default"
	kubeStatefulSet   = "buildkitd-statefulset" // Example name
	kubeTestScaling   = false                   // Set to true to test scaling functions
	kubeTestTargetRep = int32(1)                // Example target replica count for scaling
)

var activeConnections atomic.Int64

func main() {
	// Initialize Kubernetes client
	clientset, err := InitKubeClient()
	if err != nil {
		log.Printf("Failed to initialize Kubernetes client: %v. Proceeding without K8s features.", err)
		// Depending on requirements, you might choose to Fatalf here if K8s is essential
	} else {
		log.Println("Successfully initialized Kubernetes client.")

		// Placeholder: Get StatefulSet status
		status, err := GetStatefulSetStatus(clientset, kubeNamespace, kubeStatefulSet)
		if err != nil {
			log.Printf("Failed to get status for StatefulSet %s/%s: %v", kubeNamespace, kubeStatefulSet, err)
		} else {
			log.Printf("Status for StatefulSet %s/%s: Desired=%d, Current=%d, Ready=%d",
				kubeNamespace, kubeStatefulSet, status.DesiredReplicas, status.CurrentReplicas, status.ReadyReplicas)
		}

		if kubeTestScaling {
			// Placeholder: Scale StatefulSet (example: scale to kubeTestTargetRep)
			log.Printf("Attempting to scale StatefulSet %s/%s to %d replicas...", kubeNamespace, kubeStatefulSet, kubeTestTargetRep)
			_, err = ScaleStatefulSet(clientset, kubeNamespace, kubeStatefulSet, kubeTestTargetRep)
			if err != nil {
				log.Printf("Failed to scale StatefulSet %s/%s: %v", kubeNamespace, kubeStatefulSet, err)
			} else {
				log.Printf("Successfully initiated scaling for StatefulSet %s/%s to %d replicas.", kubeNamespace, kubeStatefulSet, kubeTestTargetRep)

				// Placeholder: Wait for readiness after scaling
				log.Printf("Waiting for StatefulSet %s/%s to reach %d ready replicas...", kubeNamespace, kubeStatefulSet, kubeTestTargetRep)
				err = WaitForStatefulSetReady(clientset, kubeNamespace, kubeStatefulSet, kubeTestTargetRep, 5*time.Minute) // 5 minute timeout
				if err != nil {
					log.Printf("Error waiting for StatefulSet %s/%s to become ready: %v", kubeNamespace, kubeStatefulSet, err)
				} else {
					log.Printf("StatefulSet %s/%s is ready with %d replicas.", kubeNamespace, kubeStatefulSet, kubeTestTargetRep)
				}
			}
		}
	}

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()
	log.Printf("TCP proxy listening on %s, forwarding to %s", listenAddr, targetSvcAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue // Continue accepting other connections
		}
		go handleConnection(clientConn)
	}
}

func handleConnection(clientConn net.Conn) {
	activeConnections.Add(1)
	log.Printf("Accepted connection from %s. Active connections: %d", clientConn.RemoteAddr(), activeConnections.Load())

	defer func() {
		clientConn.Close()
		activeConnections.Add(-1)
		log.Printf("Closed connection from %s. Active connections: %d", clientConn.RemoteAddr(), activeConnections.Load())
	}()

	targetConn, err := net.Dial("tcp", targetSvcAddr)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", targetSvcAddr, err)
		return // Close client connection via defer
	}
	log.Printf("Successfully connected to target %s for client %s", targetSvcAddr, clientConn.RemoteAddr())
	defer targetConn.Close()

	var wg sync.WaitGroup
	wg.Add(2) // One for client-to-target, one for target-to-client

	// Goroutine for client -> target
	go func() {
		defer wg.Done()
		defer targetConn.Close() // Close target if client closes or error
		_, err := io.Copy(targetConn, clientConn)
		if err != nil && err != io.EOF {
			log.Printf("Error copying from client %s to target %s: %v", clientConn.RemoteAddr(), targetSvcAddr, err)
		}
		// log.Printf("Client %s to target %s copy finished.", clientConn.RemoteAddr(), targetSvcAddr)
	}()

	// Goroutine for target -> client
	go func() {
		defer wg.Done()
		defer clientConn.Close() // Close client if target closes or error
		_, err := io.Copy(clientConn, targetConn)
		if err != nil && err != io.EOF {
			log.Printf("Error copying from target %s to client %s: %v", targetSvcAddr, clientConn.RemoteAddr(), err)
		}
		// log.Printf("Target %s to client %s copy finished.", targetSvcAddr, clientConn.RemoteAddr())
	}()

	wg.Wait() // Wait for both copy operations to complete
	log.Printf("Data transfer complete for client %s and target %s.", clientConn.RemoteAddr(), targetSvcAddr)
}
