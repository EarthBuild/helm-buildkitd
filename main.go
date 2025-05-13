package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
)

const (
	listenAddr    = ":8080"
	targetSvcAddr = "localhost:8273" // Placeholder, will be configurable
)

var activeConnections atomic.Int64

func main() {
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