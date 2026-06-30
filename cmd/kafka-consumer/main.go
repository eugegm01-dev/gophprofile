package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/segmentio/kafka-go"
)

func main() {
	cfg := config.MustLoad()

	if len(cfg.Kafka.Brokers) == 0 {
		log.Fatal("Kafka brokers not configured")
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Kafka.Brokers,
		Topic:   cfg.Kafka.AvatarEvents,
		GroupID: "gophprofile-consumer",
	})
	defer reader.Close()

	log.Println("✅ Kafka consumer started, listening for events...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down consumer...")
		cancel()
	}()

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("Error reading message: %v", err)
			continue
		}

		var event map[string]any
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("Error unmarshaling event: %v", err)
			continue
		}

		log.Printf("📨 Received event: %+v", event)
		// Здесь можно добавить обработку событий (сохранение в БД, аналитику и т.д.)
	}

	log.Println("Consumer stopped")
}
