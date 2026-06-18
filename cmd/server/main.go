package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/server"
	"github.com/gubaevem/gophprofile/internal/service"
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

	// Подключаем RabbitMQ
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

	// 2. Инициализация слоев
	avatarRepo := repository.NewAvatarRepository(db)
	avatarService := service.NewAvatarService(avatarRepo, s3Client, mqPublisher, mqDeletePublisher)
	avatarHandler := server.NewAvatarHandler(avatarService)

	// 3. Роутер
	mux := http.NewServeMux()

	// API endpoints
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
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
