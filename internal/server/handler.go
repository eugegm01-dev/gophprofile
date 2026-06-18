package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gubaevem/gophprofile/internal/service"
)

type AvatarHandler struct {
	service *service.AvatarService
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

	// 5. Вызываем бизнес-логику
	avatar, err := h.service.Upload(r.Context(), userID, handler.Filename, handler.Header.Get("Content-Type"), data)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// 6. Возвращаем успешный JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(avatar)
}

// GET /api/v1/avatars/{id}
func (h *AvatarHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Достаем ID из URL: /api/v1/avatars/{id}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"avatar id is required"}`, http.StatusBadRequest)
		return
	}

	// Вызываем бизнес-логику
	avatar, data, err := h.service.Get(r.Context(), id)
	if err != nil {
		if err.Error() == "avatar not found" {
			http.Error(w, `{"error":"avatar not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// Отдаем файл с правильным Content-Type
	w.Header().Set("Content-Type", avatar.MimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", avatar.FileName))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
