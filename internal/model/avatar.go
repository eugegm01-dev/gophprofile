package model

import "time"

type Avatar struct {
	ID               string            `json:"id"`
	UserID           string            `json:"user_id"`
	FileName         string            `json:"file_name"`
	MimeType         string            `json:"mime_type"`
	SizeBytes        int64             `json:"size_bytes"`
	S3Key            string            `json:"-"`
	URL              string            `json:"url"`
	UploadStatus     string            `json:"upload_status"`
	ProcessingStatus string            `json:"processing_status"`
	ThumbnailS3Keys  map[string]string `json:"thumbnails,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}
