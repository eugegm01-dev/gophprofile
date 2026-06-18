package validator

import (
	"fmt"
	"net/http"
)

// Magic bytes для разных форматов
var magicBytes = map[string][]byte{
	"image/jpeg": {0xFF, 0xD8, 0xFF},
	"image/png":  {0x89, 0x50, 0x4E, 0x47},
	"image/webp": {0x52, 0x49, 0x46, 0x46}, // RIFF
	"image/gif":  {0x47, 0x49, 0x46, 0x38}, // GIF8
}

// ValidateImageByMagicBytes проверяет, что файл действительно является изображением
func ValidateImageByMagicBytes(data []byte, claimedType string) error {
	if len(data) < 12 {
		return fmt.Errorf("file too small to be an image")
	}

	for mimeType, magic := range magicBytes {
		if len(data) >= len(magic) && matchBytes(data[:len(magic)], magic) {
			// Если нашли совпадение magic bytes
			if claimedType != "" && claimedType != mimeType {
				// Но MIME тип не совпадает — это подозрительно
				return fmt.Errorf("file content is %s but claimed as %s", mimeType, claimedType)
			}
			return nil
		}
	}

	return fmt.Errorf("unsupported or invalid image format")
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
	for mimeType, magic := range magicBytes {
		if len(data) >= len(magic) && matchBytes(data[:len(magic)], magic) {
			return mimeType
		}
	}
	return http.DetectContentType(data)
}
