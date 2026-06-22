package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gubaevem/gophprofile/internal/model"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/service"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq"
	"github.com/gubaevem/gophprofile/pkg/s3"
	"github.com/gubaevem/gophprofile/pkg/validator"
)

type AvatarHandler struct {
	service *service.AvatarService
}

type HealthHandler struct {
	db     *repository.Postgres
	s3     *s3.Client
	rabbit *rabbitmq.Publisher
}

func NewHealthHandler(db *repository.Postgres, s3 *s3.Client, rabbit *rabbitmq.Publisher) *HealthHandler {
	return &HealthHandler{db: db, s3: s3, rabbit: rabbit}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	status := map[string]string{
		"status":   "ok",
		"postgres": "ok",
		"s3":       "ok",
		"rabbitmq": "ok",
	}
	httpCode := http.StatusOK

	// Проверяем PostgreSQL
	if err := h.db.Pool().Ping(ctx); err != nil {
		status["postgres"] = "error: " + err.Error()
		status["status"] = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	// Проверяем S3
	if _, err := h.s3.Minio().BucketExists(ctx, h.s3.BucketName()); err != nil {
		status["s3"] = "error: " + err.Error()
		status["status"] = "degraded"
		httpCode = http.StatusServiceUnavailable
	}

	// Проверяем RabbitMQ
	if h.rabbit == nil || h.rabbit.IsClosed() {
		status["rabbitmq"] = "error: connection closed"
		status["status"] = "degraded"
		httpCode = http.StatusServiceUnavailable
	} // Или просто проверяем что connection открыт

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Printf("Failed to encode health status: %v", err)
	}
}

func NewAvatarHandler(service *service.AvatarService) *AvatarHandler {
	return &AvatarHandler{service: service}
}

// POST /api/v1/avatars
func (h *AvatarHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 1. Достаем User ID из заголовка
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		http.Error(w, `{"error":"X-User-ID header is required"}`, http.StatusBadRequest)
		return
	}

	// 2. Парсим multipart форму (лимит 10 МБ)
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		http.Error(w, `{"error":"invalid multipart form or file too large"}`, http.StatusBadRequest)
		return
	}

	// 3. Достаем файл по имени поля "file"
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file is required"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 4. Читаем байты файла
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, `{"error":"failed to read file"}`, http.StatusInternalServerError)
		return
	}

	// 4.1. Валидация через magic bytes (защита от подмены расширения)
	if err := validator.ValidateImageByMagicBytes(data, handler.Header.Get("Content-Type")); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// 4.2. Определяем реальный MIME-тип
	realMimeType := validator.DetectMimeType(data)

	// 5. Вызываем бизнес-логику (используем реальный MIME-тип)
	avatar, err := h.service.Upload(r.Context(), userID, handler.Filename, realMimeType, data)
	if err != nil {
		log.Printf("Internal error: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// 6. Возвращаем успешный JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(avatar); err != nil {
		log.Printf("Failed to encode response: %v", err)
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}

// GET /api/v1/avatars/{id}
func (h *AvatarHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"avatar id is required"}`, http.StatusBadRequest)
		return
	}

	size := r.URL.Query().Get("size")

	avatar, data, err := h.service.GetWithSize(r.Context(), id, size)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, `{"error":"avatar not found"}`, http.StatusNotFound)
			return
		}
		log.Printf("get avatar error: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", avatar.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", avatar.FileName))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

// DELETE /api/v1/avatars/{id}
func (h *AvatarHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	userID := r.Header.Get("X-User-ID")
	if id == "" || userID == "" {
		http.Error(w, `{"error":"id and X-User-ID are required"}`, http.StatusBadRequest)
		return
	}

	err := h.service.Delete(r.Context(), id, userID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, `{"error":"avatar not found"}`, http.StatusNotFound)
		case errors.Is(err, service.ErrAccessDenied):
			http.Error(w, `{"error":"access denied"}`, http.StatusForbidden)
		default:
			log.Printf("Delete error: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent) // 204 No Content
}

// GET /api/v1/avatars/{id}/metadata
func (h *AvatarHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"avatar id is required"}`, http.StatusBadRequest)
		return
	}

	metadata, err := h.service.GetMetadata(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, `{"error":"avatar not found"}`, http.StatusNotFound)
		case errors.Is(err, service.ErrAccessDenied):
			http.Error(w, `{"error":"access denied"}`, http.StatusForbidden)
		default:
			log.Printf("GetMetadata error: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		log.Printf("Failed to encode metadata response: %v", err)
	}
}

// GET /api/v1/users/{user_id}/avatars
func (h *AvatarHandler) GetUserAvatars(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		http.Error(w, `{"error":"user_id is required"}`, http.StatusBadRequest)
		return
	}

	avatars, err := h.service.GetUserAvatars(r.Context(), userID)
	if err != nil {
		log.Printf("Internal error: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	if avatars == nil {
		avatars = []*model.Avatar{} // пустой массив вместо null
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(avatars); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}
