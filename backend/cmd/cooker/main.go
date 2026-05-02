package main

import (
	"fmt"
	"log"

	"github.com/cooker-ci/cooker/internal/config"
	"github.com/cooker-ci/cooker/internal/server"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			log.Printf("shutdown: store close: %v", err)
		}
	}()

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Cooker starting on %s", addr)
	if err := srv.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
