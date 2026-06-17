package main

import (
	"log"
	"github.com/gubaevem/gophprofile/internal/config"
)

func main() {
	cfg := config.Load()
	log.Printf("Starting worker, connecting to %s", cfg.RabbitMQ.Host)
}
