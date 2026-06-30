package validator

import (
	"fmt"
	"net/http"
)

// Magic bytes для разных форматов
var magicBytes = map[string][]byte{
	"image/jpeg": {0xFF, 0xD8, 0xFF},
	"image/png":  {0x89, 0x50, 0x4E, 0x47},
	"image/gif":  {0x47, 0x49, 0x46, 0x38}, // GIF8
}

// ValidateImageByMagicBytes проверяет, что файл действительно является изображением
func ValidateImageByMagicBytes(data []byte, claimedType string) error {
	if len(data) < 12 {
		return fmt.Errorf("file too small")
	}

	// JPEG
	if matchBytes(data[:3], []byte{0xFF, 0xD8, 0xFF}) {
		return validateClaimedType("image/jpeg", claimedType)
	}

	// PNG
	if matchBytes(data[:4], []byte{0x89, 0x50, 0x4E, 0x47}) {
		return validateClaimedType("image/png", claimedType)
	}

	// WebP: RIFF + WEBP на позициях 8-11
	if matchBytes(data[:4], []byte{0x52, 0x49, 0x46, 0x46}) && // RIFF
		matchBytes(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}) { // WEBP
		return validateClaimedType("image/webp", claimedType)
	}

	return fmt.Errorf("unsupported image format")
}

func validateClaimedType(detected, claimed string) error {
	if claimed != "" && claimed != detected {
		return fmt.Errorf("file is %s but claimed as %s", detected, claimed)
	}
	return nil
}

func matchBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DetectMimeType определяет MIME-тип по magic bytes
func DetectMimeType(data []byte) string {
	// Проверяем WebP отдельно (RIFF + WEBP)
	if len(data) >= 12 &&
		matchBytes(data[:4], []byte{0x52, 0x49, 0x46, 0x46}) &&
		matchBytes(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}) {
		return "image/webp"
	}

	// Остальные форматы
	for mimeType, magic := range magicBytes {
		if len(data) >= len(magic) && matchBytes(data[:len(magic)], magic) {
			return mimeType
		}
	}
	return http.DetectContentType(data)
}
