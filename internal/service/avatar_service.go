package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/gubaevem/gophprofile/internal/model"
	kafkapkg "github.com/gubaevem/gophprofile/pkg/kafka"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var ErrAccessDenied = errors.New("access denied")

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

// События для RabbitMQ (для воркера)
type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}
type AvatarDeleteEvent struct {
	AvatarID string `json:"avatar_id"`
	S3Key    string `json:"s3_key"`
}

// === БИЗНЕС-МЕТРИКИ ===
var (
	meter = otel.Meter("gophprofile.avatar-service")

	uploadsTotal, _ = meter.Int64Counter(
		"avatars_uploads_total",
		metric.WithDescription("Total number of avatar uploads"),
	)

	uploadDuration, _ = meter.Float64Histogram(
		"avatars_upload_duration_seconds",
		metric.WithDescription("Avatar upload duration in seconds"),
		metric.WithUnit("s"),
	)

	storageUsage, _ = meter.Int64Counter(
		"avatars_storage_bytes_total",
		metric.WithDescription("Total bytes uploaded to storage"),
	)
)

// Сервис с гибридной архитектурой: RabbitMQ + Kafka
type AvatarService struct {
	repo            AvatarRepository
	s3Client        S3Client
	publisher       Publisher          // RabbitMQ для воркера
	deletePublisher Publisher          // RabbitMQ для воркера (удаление)
	kafkaProducer   *kafkapkg.Producer // Kafka для аналитики/аудита
}

func NewAvatarService(
	repo AvatarRepository,
	s3 S3Client,
	pub Publisher,
	deletePub Publisher,
	kafkaProd *kafkapkg.Producer,
) *AvatarService {
	return &AvatarService{
		repo:            repo,
		s3Client:        s3,
		publisher:       pub,
		deletePublisher: deletePub,
		kafkaProducer:   kafkaProd,
	}
}

func (s *AvatarService) Upload(ctx context.Context, userID, fileName, mimeType string, data []byte) (retAvatar *model.Avatar, retErr error) {
	startTime := time.Now()

	// Создаём корневой спан для операции Upload
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.Upload")
	defer func() {
		duration := time.Since(startTime).Seconds()
		status := "success"
		if retErr != nil {
			status = "error"
			span.RecordError(retErr)
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()

		// Записываем бизнес-метрики
		uploadsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", status),
			attribute.String("mime_type", mimeType),
		))
		uploadDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("status", status),
		))
		if retErr == nil {
			storageUsage.Add(ctx, int64(len(data)))
		}
	}()

	avatarID := uuid.New().String()
	s3Key := fmt.Sprintf("avatars/%s/%s", userID, avatarID)

	// 1. Загружаем файл в S3
	ctx, s3Span := otel.Tracer("avatar-service").Start(ctx, "S3.Upload")
	s3Span.SetAttributes(attribute.String("s3.key", s3Key), attribute.Int("file.size", len(data)))
	err := s.s3Client.Upload(ctx, s3Key, data, mimeType)
	s3Span.End()
	if err != nil {
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
	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.Create")
	dbSpan.SetAttributes(attribute.String("avatar.id", avatarID), attribute.String("user.id", userID))
	if err := s.repo.Create(ctx, avatar); err != nil {
		dbSpan.RecordError(err)
		dbSpan.End()
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}
	dbSpan.End()

	// 4. Отправляем событие в RabbitMQ для воркера (ресайз)
	event := AvatarUploadEvent{
		AvatarID: avatarID,
		UserID:   userID,
		S3Key:    s3Key,
	}
	ctx, mqSpan := otel.Tracer("avatar-service").Start(ctx, "RabbitMQ.Publish")
	if err := s.publisher.PublishEvent(ctx, event); err != nil {
		mqSpan.RecordError(err)
		log.Printf("⚠️ Failed to publish event to RabbitMQ: %v", err)
	}
	mqSpan.End()

	// 5. Публикуем событие в Kafka для аналитики/аудита
	if s.kafkaProducer != nil {
		kafkaEvent := map[string]any{
			"event_type": "avatar_uploaded",
			"avatar_id":  avatarID,
			"user_id":    userID,
			"file_name":  fileName,
			"mime_type":  mimeType,
			"size_bytes": int64(len(data)),
			"timestamp":  time.Now().UTC(),
		}
		ctx, kafkaSpan := otel.Tracer("avatar-service").Start(ctx, "Kafka.Publish")
		if err := s.kafkaProducer.PublishEvent(ctx, kafkaEvent); err != nil {
			kafkaSpan.RecordError(err)
			log.Printf("Warning: failed to publish to Kafka: %v", err)
		}
		kafkaSpan.End()
	}

	return avatar, nil
}

func (s *AvatarService) Get(ctx context.Context, id string) (*model.Avatar, []byte, error) {
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.Get")
	defer span.End()

	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.GetByID")
	avatar, err := s.repo.GetByID(ctx, id)
	dbSpan.End()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

	ctx, s3Span := otel.Tracer("avatar-service").Start(ctx, "S3.Download")
	data, err := s.s3Client.Download(ctx, avatar.S3Key)
	s3Span.End()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download from s3: %w", err)
	}
	return avatar, data, nil
}

func (s *AvatarService) Delete(ctx context.Context, id, userID string) (retErr error) {
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.Delete")
	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	// 1. Получаем метаданные, чтобы узнать S3Key и проверить владельца
	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.GetByID")
	avatar, err := s.repo.GetByID(ctx, id)
	dbSpan.End()
	if err != nil {
		return err
	}

	if avatar.UserID != userID {
		return ErrAccessDenied
	}

	// 2. Мягкое удаление в БД
	ctx, dbDelSpan := otel.Tracer("avatar-service").Start(ctx, "DB.SoftDelete")
	if err := s.repo.SoftDelete(ctx, id, userID); err != nil {
		dbDelSpan.RecordError(err)
		dbDelSpan.End()
		return err
	}
	dbDelSpan.End()

	// 3. Отправляем событие на физическое удаление из S3 (RabbitMQ)
	event := AvatarDeleteEvent{AvatarID: id, S3Key: avatar.S3Key}
	ctx, mqSpan := otel.Tracer("avatar-service").Start(ctx, "RabbitMQ.PublishDelete")
	if err := s.deletePublisher.PublishEvent(ctx, event); err != nil {
		mqSpan.RecordError(err)
		log.Printf("⚠️ Failed to publish delete event: %v", err)
	}
	mqSpan.End()

	// 4. Публикуем событие удаления в Kafka для аналитики/аудита
	if s.kafkaProducer != nil {
		kafkaEvent := map[string]any{
			"event_type": "avatar_deleted",
			"avatar_id":  id,
			"user_id":    userID,
			"timestamp":  time.Now().UTC(),
		}
		ctx, kafkaSpan := otel.Tracer("avatar-service").Start(ctx, "Kafka.PublishDelete")
		if err := s.kafkaProducer.PublishEvent(ctx, kafkaEvent); err != nil {
			kafkaSpan.RecordError(err)
			log.Printf("Warning: failed to publish delete event to Kafka: %v", err)
		}
		kafkaSpan.End()
	}

	return nil
}

func (s *AvatarService) GetWithSize(ctx context.Context, id, size string) (*model.Avatar, []byte, error) {
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.GetWithSize")
	defer span.End()

	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.GetByID")
	avatar, err := s.repo.GetByID(ctx, id)
	dbSpan.End()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

	s3Key := avatar.S3Key // по умолчанию оригинал
	if size != "" && size != "original" {
		if thumbKey, ok := avatar.ThumbnailS3Keys[size]; ok {
			s3Key = thumbKey
		} else {
			return nil, nil, fmt.Errorf("thumbnail size %s not available", size)
		}
	}

	ctx, s3Span := otel.Tracer("avatar-service").Start(ctx, "S3.Download")
	data, err := s.s3Client.Download(ctx, s3Key)
	s3Span.End()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download from s3: %w", err)
	}
	return avatar, data, nil
}

func (s *AvatarService) GetMetadata(ctx context.Context, id string) (*model.MetadataResponse, error) {
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.GetMetadata")
	defer span.End()

	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.GetMetadataByID")
	avatar, err := s.repo.GetMetadataByID(ctx, id)
	dbSpan.End()
	if err != nil {
		return nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}

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
	ctx, span := otel.Tracer("avatar-service").Start(ctx, "AvatarService.GetUserAvatars")
	defer span.End()

	ctx, dbSpan := otel.Tracer("avatar-service").Start(ctx, "DB.GetByUserID")
	avatars, err := s.repo.GetByUserID(ctx, userID)
	dbSpan.End()
	return avatars, err
}
