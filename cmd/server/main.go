package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/server"
	"github.com/gubaevem/gophprofile/internal/service"
	kafkapkg "github.com/gubaevem/gophprofile/pkg/kafka"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq"
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

	// Подключаем RabbitMQ (для воркера)
	mqPublisher, err := rabbitmq.NewPublisher(cfg.RabbitMQ.URL, cfg.RabbitMQ.Queue)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer mqPublisher.Close()

	mqDeletePublisher, err := rabbitmq.NewPublisher(cfg.RabbitMQ.URL, cfg.RabbitMQ.QueueDelete)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ (delete): %v", err)
	}
	defer mqDeletePublisher.Close()

	// Создаем Kafka producer (для аналитики/аудита)
	var kafkaProducer *kafkapkg.Producer
	if len(cfg.Kafka.Brokers) > 0 {
		kafkaProducer, err = kafkapkg.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.AvatarEvents)
		if err != nil {
			log.Printf("Warning: failed to create Kafka producer: %v", err)
		} else {
			defer kafkaProducer.Close()
			log.Println("✅ Kafka producer initialized")
		}
	}

	// 2. Инициализация слоев (передаем ВСЕ 5 зависимостей)
	avatarRepo := repository.NewAvatarRepository(db)
	avatarService := service.NewAvatarService(
		avatarRepo,
		s3Client,
		mqPublisher,
		mqDeletePublisher,
		kafkaProducer, // может быть nil, сервис это обрабатывает
	)
	avatarHandler := server.NewAvatarHandler(avatarService)

	// 3. Роутер
	mux := http.NewServeMux()
	healthHandler := server.NewHealthHandler(db, s3Client, mqPublisher)
	mux.HandleFunc("GET /health", healthHandler.Check)

	// API endpoints
	mux.HandleFunc("POST /api/v1/avatars", avatarHandler.Upload)
	mux.HandleFunc("GET /api/v1/avatars/{id}/metadata", avatarHandler.GetMetadata)
	mux.HandleFunc("GET /api/v1/avatars/{id}", avatarHandler.Get)
	mux.HandleFunc("GET /api/v1/users/{user_id}/avatars", avatarHandler.GetUserAvatars)
	mux.HandleFunc("GET /web/gallery/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "static", "gallery.html"))
	})
	mux.HandleFunc("DELETE /api/v1/avatars/{id}", avatarHandler.Delete)

	// Web interface
	mux.HandleFunc("GET /web/upload", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "static", "index.html"))
	})

	// Static files
	staticDir := filepath.Join("web", "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Root redirect to /web/upload
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/upload", http.StatusFound)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("🚀 Server starting on %s", addr)
	log.Printf("🌐 Web interface: http://localhost:%d/web/upload", cfg.Server.Port)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Запускаем сервер в горутине
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Ждём сигнал завершения
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped gracefully")
}
