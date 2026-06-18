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
	pkgs3 "github.com/gubaevem/gophprofile/pkg/s3" // Добавили
)

func main() {
	cfg := config.MustLoad()

	db, err := repository.NewPostgres(&cfg.Database)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	defer db.Close()

	s3Client, err := pkgs3.NewClient(&cfg.S3) // Добавили
	if err != nil {
		log.Fatalf("S3 error: %v", err)
	}

	// Consumer для загрузок
	uploadConsumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ.URL, cfg.RabbitMQ.Queue)
	if err != nil {
		log.Fatalf("MQ Upload error: %v", err)
	}
	defer uploadConsumer.Close()

	// Consumer для удалений
	deleteConsumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ.URL, cfg.RabbitMQ.QueueDelete)
	if err != nil {
		log.Fatalf("MQ Delete error: %v", err)
	}
	defer deleteConsumer.Close()

	avatarRepo := repository.NewAvatarRepository(db)

	// Handler для загрузок (старый)
	uploadHandler := func(body []byte) error {
		var event service.AvatarUploadEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}
		log.Printf("🖼️ Processing avatar %s", event.AvatarID)
		return avatarRepo.UpdateProcessingStatus(context.Background(), event.AvatarID, "completed")
	}

	// Handler для удалений (новый)
	deleteHandler := func(body []byte) error {
		var event service.AvatarDeleteEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}
		log.Printf("🗑️ Deleting avatar %s from S3", event.AvatarID)
		return s3Client.Delete(context.Background(), event.S3Key)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем оба консьюмера в фоне
	go func() {
		if err := uploadConsumer.Consume(ctx, uploadHandler); err != nil {
			log.Printf("Upload consumer error: %v", err)
		}
	}()
	go func() {
		if err := deleteConsumer.Consume(ctx, deleteHandler); err != nil {
			log.Printf("Delete consumer error: %v", err)
		}
	}()

	log.Println("🚀 Worker starting (listening on 2 queues)...")
	<-sigChan
	log.Println("👋 Worker stopped gracefully")
}
