package service

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/gubaevem/gophprofile/internal/model"
)

// Интерфейсы для зависимостей (Dependency Inversion Principle)
type AvatarRepository interface {
	Create(ctx context.Context, avatar *model.Avatar) error
	GetByID(ctx context.Context, id string) (*model.Avatar, error)
	SoftDelete(ctx context.Context, id, userID string) error
	UpdateProcessingStatus(ctx context.Context, avatarID, status string) error
	UpdateThumbnails(ctx context.Context, avatarID string, thumbnails map[string]string) error
	GetMetadataByID(ctx context.Context, id string) (*model.Avatar, error)
	GetByUserID(ctx context.Context, userID string) ([]*model.Avatar, error)
}

type S3Client interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	Download(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type Publisher interface {
	PublishEvent(ctx context.Context, event any) error
}

// События
type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}

type AvatarDeleteEvent struct {
	AvatarID string `json:"avatar_id"`
	S3Key    string `json:"s3_key"`
}

// Сервис теперь зависит от интерфейсов
type AvatarService struct {
	repo            AvatarRepository
	s3Client        S3Client
	publisher       Publisher
	deletePublisher Publisher
}

func NewAvatarService(repo AvatarRepository, s3 S3Client, pub Publisher, deletePub Publisher) *AvatarService {
	return &AvatarService{
		repo:            repo,
		s3Client:        s3,
		publisher:       pub,
		deletePublisher: deletePub,
	}
}

// ... остальной код (Upload, Get, Delete, GetWithSize) остаётся без изменений
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
func (s *AvatarService) Delete(ctx context.Context, id, userID string) error {
	// 1. Получаем метаданные, чтобы узнать S3Key и проверить владельца
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if avatar.UserID != userID {
		return fmt.Errorf("access denied")
	}

	// 2. Мягкое удаление в БД
	if err := s.repo.SoftDelete(ctx, id, userID); err != nil {
		return err
	}

	// 3. Отправляем событие на физическое удаление из S3
	event := AvatarDeleteEvent{AvatarID: id, S3Key: avatar.S3Key}
	if err := s.deletePublisher.PublishEvent(ctx, event); err != nil {
		log.Printf("⚠️ Failed to publish delete event: %v", err)
	}

	return nil
}
func (s *AvatarService) GetWithSize(ctx context.Context, id, size string) (*model.Avatar, []byte, error) {
	// 1. Получаем метаданные из БД
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

	// 2. Определяем, какой файл скачивать
	s3Key := avatar.S3Key // по умолчанию оригинал

	if size != "" && size != "original" {
		// Проверяем, есть ли миниатюра нужного размера
		if thumbKey, ok := avatar.ThumbnailS3Keys[size]; ok {
			s3Key = thumbKey
		} else {
			return nil, nil, fmt.Errorf("thumbnail size %s not available", size)
		}
	}

	// 3. Скачиваем файл из S3
	data, err := s.s3Client.Download(ctx, s3Key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download from s3: %w", err)
	}

	return avatar, data, nil
}

func (s *AvatarService) GetMetadata(ctx context.Context, id string) (*model.MetadataResponse, error) {
	avatar, err := s.repo.GetMetadataByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

	// Формируем список миниатюр с URL
	thumbnails := make([]model.ThumbnailInfo, 0, len(avatar.ThumbnailS3Keys))
	for size := range avatar.ThumbnailS3Keys {
		thumbnails = append(thumbnails, model.ThumbnailInfo{
			Size: size,
			URL:  fmt.Sprintf("/api/v1/avatars/%s?size=%s", avatar.ID, size),
		})
	}

	return &model.MetadataResponse{
		ID:         avatar.ID,
		UserID:     avatar.UserID,
		FileName:   avatar.FileName,
		MimeType:   avatar.MimeType,
		Size:       avatar.SizeBytes,
		Thumbnails: thumbnails,
		CreatedAt:  avatar.CreatedAt,
		UpdatedAt:  avatar.UpdatedAt,
	}, nil
}
func (s *AvatarService) GetUserAvatars(ctx context.Context, userID string) ([]*model.Avatar, error) {
	return s.repo.GetByUserID(ctx, userID)
}
