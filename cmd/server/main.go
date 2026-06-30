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
	"github.com/gubaevem/gophprofile/internal/observability"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/server"
	"github.com/gubaevem/gophprofile/internal/service"
	kafkapkg "github.com/gubaevem/gophprofile/pkg/kafka"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq"
	pkgs3 "github.com/gubaevem/gophprofile/pkg/s3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	cfg := config.MustLoad()

	// === OBSERVABILITY: Инициализация OpenTelemetry ===
	// Jaeger OTLP gRPC endpoint (проброшен в docker-compose на 4317)
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "jaeger:4317" // Дефолт для работы внутри docker-сети
	}
	shutdownOTEL, metricsHandler, err := observability.InitOTEL("gophprofile-server", otlpEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize OpenTelemetry: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownOTEL(ctx); err != nil {
			log.Printf("OTEL shutdown error: %v", err)
		}
	}()

	logger := observability.NewLogger()

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

	var kafkaProducer *kafkapkg.Producer
	if len(cfg.Kafka.Brokers) > 0 {
		kafkaProducer, err = kafkapkg.NewProducer(cfg.Kafka.Brokers, cfg.Kafka.AvatarEvents)
		if err != nil {
			logger.Warn("Failed to create Kafka producer", "error", err)
		} else {
			defer kafkaProducer.Close()
			logger.Info("Kafka producer initialized")
		}
	}

	// 2. Инициализация слоев
	avatarRepo := repository.NewAvatarRepository(db)
	avatarService := service.NewAvatarService(
		avatarRepo,
		s3Client,
		mqPublisher,
		mqDeletePublisher,
		kafkaProducer,
	)
	avatarHandler := server.NewAvatarHandler(avatarService, logger)

	// 3. Роутер
	mux := http.NewServeMux()
	healthHandler := server.NewHealthHandler(db, s3Client, mqPublisher, logger)

	mux.HandleFunc("GET /health", healthHandler.Check)

	// OBSERVABILITY: Эндпоинт для сбора метрик Прометеусом
	mux.Handle("/metrics", metricsHandler)

	// API endpoints
	mux.HandleFunc("POST /api/v1/avatars", avatarHandler.Upload)
	mux.HandleFunc("GET /api/v1/avatars/{id}/metadata", avatarHandler.GetMetadata)
	mux.HandleFunc("GET /api/v1/avatars/{id}", avatarHandler.Get)
	mux.HandleFunc("GET /api/v1/users/{user_id}/avatars", avatarHandler.GetUserAvatars)
	mux.HandleFunc("DELETE /api/v1/avatars/{id}", avatarHandler.Delete)

	// Web interface & Static
	mux.HandleFunc("GET /web/gallery/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "static", "gallery.html"))
	})
	mux.HandleFunc("GET /web/upload", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "static", "index.html"))
	})
	staticDir := filepath.Join("web", "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/upload", http.StatusFound)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// OBSERVABILITY: Оборачиваем весь роутер в otelhttp.
	// Это автоматически создаст спаны для всех входящих HTTP-запросов
	// и добавит стандартные RED-метрики (Request rate, Errors, Duration).
	handler := otelhttp.NewHandler(mux, "gophprofile-http-server")

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// Запускаем сервер
	go func() {
		logger.Info("Server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	logger.Info("Server stopped gracefully")
}
