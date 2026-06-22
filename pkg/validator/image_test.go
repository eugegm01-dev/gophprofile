package validator

import (
	"testing"
)

func TestValidateImageByMagicBytes_JPEG(t *testing.T) {
	// JPEG magic bytes: FF D8 FF
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}

	err := ValidateImageByMagicBytes(data, "image/jpeg")
	if err != nil {
		t.Errorf("Expected no error for valid JPEG, got: %v", err)
	}
}

func TestValidateImageByMagicBytes_PNG(t *testing.T) {
	// PNG magic bytes: 89 50 4E 47
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

	err := ValidateImageByMagicBytes(data, "image/png")
	if err != nil {
		t.Errorf("Expected no error for valid PNG, got: %v", err)
	}
}

func TestValidateImageByMagicBytes_WebP(t *testing.T) {
	// WebP: RIFF....WEBP
	data := []byte{
		0x52, 0x49, 0x46, 0x46, // RIFF
		0x00, 0x00, 0x00, 0x00, // size
		0x57, 0x45, 0x42, 0x50, // WEBP
	}

	err := ValidateImageByMagicBytes(data, "image/webp")
	if err != nil {
		t.Errorf("Expected no error for valid WebP, got: %v", err)
	}
}

func TestValidateImageByMagicBytes_TooSmall(t *testing.T) {
	data := []byte{0xFF, 0xD8}

	err := ValidateImageByMagicBytes(data, "image/jpeg")
	if err == nil {
		t.Error("Expected error for too small file, got nil")
	}
}

func TestValidateImageByMagicBytes_InvalidFormat(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	err := ValidateImageByMagicBytes(data, "image/jpeg")
	if err == nil {
		t.Error("Expected error for invalid format, got nil")
	}
}

func TestValidateImageByMagicBytes_WrongMimeType(t *testing.T) {
	// JPEG data but claiming PNG
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}

	err := ValidateImageByMagicBytes(data, "image/png")
	if err == nil {
		t.Error("Expected error for wrong MIME type, got nil")
	}
}

func TestDetectMimeType_JPEG(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}

	mimeType := DetectMimeType(data)
	if mimeType != "image/jpeg" {
		t.Errorf("Expected image/jpeg, got: %s", mimeType)
	}
}

func TestDetectMimeType_PNG(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

	mimeType := DetectMimeType(data)
	if mimeType != "image/png" {
		t.Errorf("Expected image/png, got: %s", mimeType)
	}
}

func TestDetectMimeType_WebP(t *testing.T) {
	data := []byte{
		0x52, 0x49, 0x46, 0x46,
		0x00, 0x00, 0x00, 0x00,
		0x57, 0x45, 0x42, 0x50,
	}

	mimeType := DetectMimeType(data)
	if mimeType != "image/webp" {
		t.Errorf("Expected image/webp, got: %s", mimeType)
	}
}
