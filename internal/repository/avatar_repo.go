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

func (r *AvatarRepository) UpdateProcessingStatus(ctx context.Context, avatarID, status string) error {
	query := `UPDATE avatars SET processing_status = $1, updated_at = NOW() WHERE id = $2`
	_, err := r.db.Pool().Exec(ctx, query, status, avatarID)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	return nil
}

func (r *AvatarRepository) GetByID(ctx context.Context, id string) (*model.Avatar, error) {
	query := `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key, 
		       upload_status, processing_status, created_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL`

	avatar := &model.Avatar{}
	err := r.db.Pool().QueryRow(ctx, query, id).Scan(
		&avatar.ID, &avatar.UserID, &avatar.FileName, &avatar.MimeType,
		&avatar.SizeBytes, &avatar.S3Key, &avatar.UploadStatus,
		&avatar.ProcessingStatus, &avatar.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("avatar not found")
		}
		return nil, fmt.Errorf("failed to get avatar: %w", err)
	}

	// Формируем URL для API
	avatar.URL = fmt.Sprintf("/api/v1/avatars/%s", avatar.ID)

	return avatar, nil
}
func (r *AvatarRepository) SoftDelete(ctx context.Context, id, userID string) error {
	query := `UPDATE avatars SET deleted_at = NOW() WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	tag, err := r.db.Pool().Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("failed to soft delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("avatar not found or access denied")
	}
	return nil
}
