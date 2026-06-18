package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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
		       upload_status, processing_status, thumbnail_s3_keys, created_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL`

	avatar := &model.Avatar{}
	var thumbnailKeys []byte // JSONB приходит как []byte

	err := r.db.Pool().QueryRow(ctx, query, id).Scan(
		&avatar.ID, &avatar.UserID, &avatar.FileName, &avatar.MimeType,
		&avatar.SizeBytes, &avatar.S3Key, &avatar.UploadStatus,
		&avatar.ProcessingStatus, &thumbnailKeys, &avatar.CreatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("avatar not found")
		}
		return nil, fmt.Errorf("failed to get avatar: %w", err)
	}

	// Парсим JSONB в map
	if len(thumbnailKeys) > 0 {
		if err := json.Unmarshal(thumbnailKeys, &avatar.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("failed to unmarshal thumbnails: %w", err)
		}
	}

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
func (r *AvatarRepository) UpdateThumbnails(ctx context.Context, avatarID string, thumbnails map[string]string) error {
	query := `UPDATE avatars SET thumbnail_s3_keys = $1, processing_status = 'completed', updated_at = NOW() WHERE id = $2`
	_, err := r.db.Pool().Exec(ctx, query, thumbnails, avatarID)
	if err != nil {
		return fmt.Errorf("failed to update thumbnails: %w", err)
	}
	return nil
}

func (r *AvatarRepository) GetMetadataByID(ctx context.Context, id string) (*model.Avatar, error) {
	query := `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       upload_status, processing_status, thumbnail_s3_keys, created_at, updated_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL`

	avatar := &model.Avatar{}
	var thumbnailKeys []byte
	var updatedAt sql.NullTime

	err := r.db.Pool().QueryRow(ctx, query, id).Scan(
		&avatar.ID, &avatar.UserID, &avatar.FileName, &avatar.MimeType,
		&avatar.SizeBytes, &avatar.S3Key, &avatar.UploadStatus,
		&avatar.ProcessingStatus, &thumbnailKeys, &avatar.CreatedAt, &updatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("avatar not found")
		}
		return nil, fmt.Errorf("failed to get avatar: %w", err)
	}

	if updatedAt.Valid {
		avatar.UpdatedAt = updatedAt.Time
	}

	if len(thumbnailKeys) > 0 {
		if err := json.Unmarshal(thumbnailKeys, &avatar.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("failed to unmarshal thumbnails: %w", err)
		}
	}

	return avatar, nil
}
func (r *AvatarRepository) GetByUserID(ctx context.Context, userID string) ([]*model.Avatar, error) {
	query := `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       upload_status, processing_status, thumbnail_s3_keys, created_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`

	rows, err := r.db.Pool().Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query avatars: %w", err)
	}
	defer rows.Close()

	var avatars []*model.Avatar
	for rows.Next() {
		avatar := &model.Avatar{}
		var thumbnailKeys []byte
		err := rows.Scan(
			&avatar.ID, &avatar.UserID, &avatar.FileName, &avatar.MimeType,
			&avatar.SizeBytes, &avatar.S3Key, &avatar.UploadStatus,
			&avatar.ProcessingStatus, &thumbnailKeys, &avatar.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan avatar: %w", err)
		}
		if len(thumbnailKeys) > 0 {
			_ = json.Unmarshal(thumbnailKeys, &avatar.ThumbnailS3Keys)
		}
		avatar.URL = fmt.Sprintf("/api/v1/avatars/%s", avatar.ID)
		avatars = append(avatars, avatar)
	}

	return avatars, nil
}
