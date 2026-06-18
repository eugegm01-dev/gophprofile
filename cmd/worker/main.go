package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/service"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq"
)

func main() {
	cfg := config.MustLoad()

	// 1. Подключаемся к БД
	db, err := repository.NewPostgres(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	// 2. Подключаемся к RabbitMQ как consumer
	consumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ.URL, cfg.RabbitMQ.Queue)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer consumer.Close()

	// 3. Создаем репозиторий
	avatarRepo := repository.NewAvatarRepository(db)

	// 4. Обработчик сообщений
	handler := func(body []byte) error {
		var event service.AvatarUploadEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}

		log.Printf("🖼️ Processing avatar %s for user %s (S3: %s)",
			event.AvatarID, event.UserID, event.S3Key)

		// Здесь в будущем будет логика ресайза картинок
		// Пока просто меняем статус на "completed"
		return avatarRepo.UpdateProcessingStatus(context.Background(), event.AvatarID, "completed")
	}

	// 5. Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("🛑 Received shutdown signal")
		cancel()
	}()

	// 6. Запускаем consumer
	log.Println("🚀 Worker starting...")
	if err := consumer.Consume(ctx, handler); err != nil {
		log.Printf("Consumer error: %v", err)
	}

	log.Println("👋 Worker stopped gracefully")
}
