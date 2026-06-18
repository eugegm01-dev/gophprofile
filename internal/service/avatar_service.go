package service

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/gubaevem/gophprofile/internal/model"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq" // Добавили
	pkgs3 "github.com/gubaevem/gophprofile/pkg/s3"
)

// AvatarUploadEvent - структура события для воркера
type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}

type AvatarService struct {
	repo      *repository.AvatarRepository
	s3Client  *pkgs3.Client
	publisher *rabbitmq.Publisher // Добавили
}

func NewAvatarService(repo *repository.AvatarRepository, s3 *pkgs3.Client, pub *rabbitmq.Publisher) *AvatarService {
	return &AvatarService{repo: repo, s3Client: s3, publisher: pub}
}

func (s *AvatarService) Upload(ctx context.Context, userID, fileName, mimeType string, data []byte) (*model.Avatar, error) {
	avatarID := uuid.New().String()
	s3Key := fmt.Sprintf("avatars/%s/%s", userID, avatarID)

	// 1. Загружаем файл в S3
	if err := s.s3Client.Upload(ctx, s3Key, data, mimeType); err != nil {
		return nil, fmt.Errorf("failed to upload to s3: %w", err)
	}

	// 2. Готовим объект для БД
	avatar := &model.Avatar{
		ID:               avatarID,
		UserID:           userID,
		FileName:         fileName,
		MimeType:         mimeType,
		SizeBytes:        int64(len(data)),
		S3Key:            s3Key,
		URL:              fmt.Sprintf("/api/v1/avatars/%s", avatarID),
		UploadStatus:     "uploaded",
		ProcessingStatus: "pending",
	}

	// 3. Сохраняем метаданные в БД
	if err := s.repo.Create(ctx, avatar); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	// 4. Отправляем событие в RabbitMQ для воркера
	event := AvatarUploadEvent{
		AvatarID: avatarID,
		UserID:   userID,
		S3Key:    s3Key,
	}

	if err := s.publisher.PublishEvent(ctx, event); err != nil {
		// В идеале здесь нужен паттерн Transactional Outbox,
		// но для MVP мы просто логируем ошибку, чтобы не фейлить весь запрос
		log.Printf("⚠️ Failed to publish event to RabbitMQ: %v", err)
	}

	return avatar, nil
}
func (s *AvatarService) Get(ctx context.Context, id string) (*model.Avatar, []byte, error) {
	// 1. Получаем метаданные из БД
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

	// 2. Скачиваем файл из S3
	data, err := s.s3Client.Download(ctx, avatar.S3Key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download from s3: %w", err)
	}

	return avatar, data, nil
}
