package repository

import (
	"context"
	"fmt"

	"github.com/gubaevem/gophprofile/internal/model"
)

type AvatarRepository struct {
	db *Postgres
}

func NewAvatarRepository(db *Postgres) *AvatarRepository {
	return &AvatarRepository{db: db}
}

func (r *AvatarRepository) Create(ctx context.Context, avatar *model.Avatar) error {
	query := `
		INSERT INTO avatars (id, user_id, file_name, mime_type, size_bytes, s3_key, upload_status, processing_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at`

	err := r.db.Pool().QueryRow(ctx, query,
		avatar.ID, avatar.UserID, avatar.FileName, avatar.MimeType,
		avatar.SizeBytes, avatar.S3Key, avatar.UploadStatus, avatar.ProcessingStatus,
	).Scan(&avatar.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert avatar: %w", err)
	}
	return nil
}
