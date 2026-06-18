package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/server"
	"github.com/gubaevem/gophprofile/internal/service"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq" // Добавили
	pkgs3 "github.com/gubaevem/gophprofile/pkg/s3"
)

func main() {
	cfg := config.MustLoad()

	// 1. Инфраструктура
	db, err := repository.NewPostgres(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	s3Client, err := pkgs3.NewClient(&cfg.S3)
	if err != nil {
		log.Fatalf("Failed to connect to S3: %v", err)
	}

	// Подключаем RabbitMQ
	// Подключаем RabbitMQ (для загрузок)
	mqPublisher, err := rabbitmq.NewPublisher(cfg.RabbitMQ.URL, cfg.RabbitMQ.Queue)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer mqPublisher.Close()

	// Подключаем RabbitMQ (для удалений)
	mqDeletePublisher, err := rabbitmq.NewPublisher(cfg.RabbitMQ.URL, cfg.RabbitMQ.QueueDelete)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ (delete): %v", err)
	}
	defer mqDeletePublisher.Close()

	// 2. Инициализация слоев (Dependency Injection)
	avatarRepo := repository.NewAvatarRepository(db)
	// Передаем mqPublisher в сервис
	avatarService := service.NewAvatarService(avatarRepo, s3Client, mqPublisher, mqDeletePublisher)
	avatarHandler := server.NewAvatarHandler(avatarService)

	// 3. Роутер
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status": "ok", "service": "gophprofile"}`)); err != nil {
			log.Printf("Failed to write health response: %v", err)
		}
	})

	mux.HandleFunc("POST /api/v1/avatars", avatarHandler.Upload)
	mux.HandleFunc("GET /api/v1/avatars/{id}", avatarHandler.Get)
	mux.HandleFunc("DELETE /api/v1/avatars/{id}", avatarHandler.Delete)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("🚀 Server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
