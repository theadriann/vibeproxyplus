package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theadriann/vibeproxyplus/internal/proxy"
)

func main() {
	listenPort := flag.Int("port", 8317, "Port to listen on")
	targetPort := flag.Int("target", 8318, "CLIProxyAPIPlus port to forward to")
	flag.Parse()

	handler := proxy.NewThinkingProxy(*targetPort)

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", *listenPort),
		Handler: handler,
	}

	// Start server in goroutine
	go func() {
		log.Printf("ThinkingProxy listening on :%d -> :%d", *listenPort, *targetPort)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Graceful shutdown with 5s timeout
	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Stopped")
}
