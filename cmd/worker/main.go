package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gubaevem/gophprofile/internal/config"
	"github.com/gubaevem/gophprofile/internal/repository"
	"github.com/gubaevem/gophprofile/internal/service"
	"github.com/gubaevem/gophprofile/pkg/rabbitmq"
	pkgs3 "github.com/gubaevem/gophprofile/pkg/s3"
)

func main() {
	cfg := config.MustLoad()

	db, err := repository.NewPostgres(&cfg.Database)
	if err != nil {
		log.Fatalf("DB error: %v", err)
	}
	defer db.Close()

	s3Client, err := pkgs3.NewClient(&cfg.S3)
	if err != nil {
		log.Fatalf("S3 error: %v", err)
	}

	uploadConsumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ.URL, cfg.RabbitMQ.Queue)
	if err != nil {
		log.Fatalf("MQ Upload error: %v", err)
	}
	defer uploadConsumer.Close()

	deleteConsumer, err := rabbitmq.NewConsumer(cfg.RabbitMQ.URL, cfg.RabbitMQ.QueueDelete)
	if err != nil {
		log.Fatalf("MQ Delete error: %v", err)
	}
	defer deleteConsumer.Close()

	avatarRepo := repository.NewAvatarRepository(db)

	// Создаём отменяемый контекст для всех операций
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обработчик загрузок — теперь использует переданный ctx
	uploadHandler := func(body []byte) error {
		var event service.AvatarUploadEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}

		// Проверяем статус (идемпотентность)
		avatar, err := avatarRepo.GetByID(ctx, event.AvatarID)
		if err != nil {
			return err
		}
		if avatar.ProcessingStatus == "completed" {
			log.Printf("⏭️ Avatar %s already processed, skipping", event.AvatarID)
			return nil
		}

		log.Printf("🖼️ Processing avatar %s for user %s", event.AvatarID, event.UserID)

		maxRetries := 3
		backoff := time.Second
		var lastErr error

		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Передаём ctx в обработку, чтобы можно было прервать
			err := processAvatarUpload(ctx, s3Client, avatarRepo, event)
			if err == nil {
				log.Printf("✅ Avatar %s processed successfully", event.AvatarID)
				return nil
			}
			lastErr = err
			log.Printf("⚠️ Attempt %d/%d failed: %v", attempt, maxRetries, err)

			if attempt == maxRetries {
				break
			}

			log.Printf("⏳ Retrying in %v...", backoff)
			select {
			case <-time.After(backoff):
				// продолжаем
			case <-ctx.Done():
				return ctx.Err()
			}
			backoff *= 2
		}
		return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
	}

	// Обработчик удалений
	deleteHandler := func(body []byte) error {
		var event service.AvatarDeleteEvent
		if err := json.Unmarshal(body, &event); err != nil {
			return err
		}
		log.Printf("🗑️ Deleting avatar %s from S3", event.AvatarID)
		return s3Client.Delete(ctx, event.S3Key) // используем ctx
	}

	// Запускаем консьюмеров с WaitGroup для graceful shutdown
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := uploadConsumer.Consume(ctx, uploadHandler); err != nil {
			log.Printf("Upload consumer error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := deleteConsumer.Consume(ctx, deleteHandler); err != nil {
			log.Printf("Delete consumer error: %v", err)
		}
	}()

	log.Println("🚀 Worker starting (listening on 2 queues)...")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("🛑 Received shutdown signal, stopping consumers...")
	cancel() // останавливаем все операции

	log.Println("Waiting for consumers to stop...")
	wg.Wait()

	log.Println("👋 Worker stopped gracefully")
}

// processAvatarUpload теперь принимает context.Context
func processAvatarUpload(ctx context.Context, s3Client *pkgs3.Client, avatarRepo *repository.AvatarRepository, event service.AvatarUploadEvent) error {
	// Скачиваем оригинал
	originalData, err := s3Client.Download(ctx, event.S3Key)
	if err != nil {
		return err
	}

	img, format, err := decodeImage(originalData)
	if err != nil {
		return err
	}

	thumbnails := map[string]string{}
	sizes := []struct {
		name string
		size int
	}{
		{"100x100", 100},
		{"300x300", 300},
	}

	for _, s := range sizes {
		thumb := imaging.Fill(img, s.size, s.size, imaging.Center, imaging.Lanczos)
		buf := new(bytes.Buffer)
		if err := encodeImage(buf, thumb, format); err != nil {
			return err
		}

		// Формируем ключ для миниатюры (безопасно)
		baseKey := strings.TrimSuffix(event.S3Key, ".jpg")
		if format != "jpeg" {
			baseKey = strings.TrimSuffix(event.S3Key, "."+format)
		}
		thumbKey := baseKey + "_" + s.name + "." + format

		if err := s3Client.Upload(ctx, thumbKey, buf.Bytes(), "image/"+format); err != nil {
			return err
		}
		thumbnails[s.name] = thumbKey
		log.Printf("✅ Created thumbnail %s: %s", s.name, thumbKey)
	}

	return avatarRepo.UpdateThumbnails(ctx, event.AvatarID, thumbnails)
}

// decodeImage и encodeImage остаются без изменений
func decodeImage(data []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	return img, format, nil
}

func encodeImage(buf *bytes.Buffer, img image.Image, format string) error {
	switch format {
	case "jpeg":
		return jpeg.Encode(buf, img, &jpeg.Options{Quality: 85})
	case "png":
		return png.Encode(buf, img)
	default:
		return jpeg.Encode(buf, img, &jpeg.Options{Quality: 85})
	}
}
