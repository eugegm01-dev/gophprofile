package service

import (
	"context"
	"errors"
	"testing"

	"github.com/gubaevem/gophprofile/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Моки для зависимостей
type MockAvatarRepository struct {
	mock.Mock
}

func (m *MockAvatarRepository) Create(ctx context.Context, avatar *model.Avatar) error {
	args := m.Called(ctx, avatar)
	return args.Error(0)
}

func (m *MockAvatarRepository) GetByID(ctx context.Context, id string) (*model.Avatar, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Avatar), args.Error(1)
}

func (m *MockAvatarRepository) SoftDelete(ctx context.Context, id, userID string) error {
	args := m.Called(ctx, id, userID)
	return args.Error(0)
}

func (m *MockAvatarRepository) UpdateProcessingStatus(ctx context.Context, avatarID, status string) error {
	args := m.Called(ctx, avatarID, status)
	return args.Error(0)
}

func (m *MockAvatarRepository) UpdateThumbnails(ctx context.Context, avatarID string, thumbnails map[string]string) error {
	args := m.Called(ctx, avatarID, thumbnails)
	return args.Error(0)
}

func (m *MockAvatarRepository) GetMetadataByID(ctx context.Context, id string) (*model.Avatar, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Avatar), args.Error(1)
}

func (m *MockAvatarRepository) GetByUserID(ctx context.Context, userID string) ([]*model.Avatar, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*model.Avatar), args.Error(1)
}

type MockS3Client struct {
	mock.Mock
}

func (m *MockS3Client) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	args := m.Called(ctx, key, data, contentType)
	return args.Error(0)
}

func (m *MockS3Client) Download(ctx context.Context, key string) ([]byte, error) {
	args := m.Called(ctx, key)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockS3Client) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

type MockPublisher struct {
	mock.Mock
}

func (m *MockPublisher) PublishEvent(ctx context.Context, event any) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

// Тесты для Upload
func TestAvatarService_Upload_Success(t *testing.T) {
	mockRepo := new(MockAvatarRepository)
	mockS3 := new(MockS3Client)
	mockPublisher := new(MockPublisher)
	mockDeletePublisher := new(MockPublisher)
	// 5-й аргумент nil — Kafka producer не нужен в unit-тестах
	svc := NewAvatarService(mockRepo, mockS3, mockPublisher, mockDeletePublisher, nil)

	mockS3.On("Upload", mock.Anything, mock.MatchedBy(func(key string) bool {
		return len(key) > 0
	}), mock.Anything, "image/jpeg").Return(nil)
	mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*model.Avatar")).Return(nil)
	mockPublisher.On("PublishEvent", mock.Anything, mock.AnythingOfType("service.AvatarUploadEvent")).Return(nil)

	avatar, err := svc.Upload(context.Background(), "user-123", "test.jpg", "image/jpeg", []byte("fake image data"))

	assert.NoError(t, err)
	assert.NotNil(t, avatar)
	assert.Equal(t, "user-123", avatar.UserID)
	assert.Equal(t, "test.jpg", avatar.FileName)
	assert.Equal(t, "image/jpeg", avatar.MimeType)
	assert.Equal(t, int64(15), avatar.SizeBytes)
	assert.Equal(t, "uploaded", avatar.UploadStatus)
	assert.Equal(t, "pending", avatar.ProcessingStatus)

	mockS3.AssertExpectations(t)
	mockRepo.AssertExpectations(t)
	mockPublisher.AssertExpectations(t)
}

func TestAvatarService_Upload_S3Error(t *testing.T) {
	mockRepo := new(MockAvatarRepository)
	mockS3 := new(MockS3Client)
	mockPublisher := new(MockPublisher)
	mockDeletePublisher := new(MockPublisher)
	svc := NewAvatarService(mockRepo, mockS3, mockPublisher, mockDeletePublisher, nil)

	mockS3.On("Upload", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("S3 error"))

	avatar, err := svc.Upload(context.Background(), "user-123", "test.jpg", "image/jpeg", []byte("data"))

	assert.Error(t, err)
	assert.Nil(t, avatar)
	assert.Contains(t, err.Error(), "failed to upload to s3")
	mockS3.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "Create")
}

func TestAvatarService_Get_Success(t *testing.T) {
	mockRepo := new(MockAvatarRepository)
	mockS3 := new(MockS3Client)
	mockPublisher := new(MockPublisher)
	mockDeletePublisher := new(MockPublisher)
	svc := NewAvatarService(mockRepo, mockS3, mockPublisher, mockDeletePublisher, nil)

	expectedAvatar := &model.Avatar{
		ID:    "test-id",
		S3Key: "avatars/user-123/test-id",
	}
	mockRepo.On("GetByID", mock.Anything, "test-id").Return(expectedAvatar, nil)
	mockS3.On("Download", mock.Anything, "avatars/user-123/test-id").Return([]byte("image data"), nil)

	avatar, data, err := svc.Get(context.Background(), "test-id")

	assert.NoError(t, err)
	assert.NotNil(t, avatar)
	assert.Equal(t, []byte("image data"), data)
	mockRepo.AssertExpectations(t)
	mockS3.AssertExpectations(t)
}

func TestAvatarService_Delete_Success(t *testing.T) {
	mockRepo := new(MockAvatarRepository)
	mockS3 := new(MockS3Client)
	mockPublisher := new(MockPublisher)
	mockDeletePublisher := new(MockPublisher)
	svc := NewAvatarService(mockRepo, mockS3, mockPublisher, mockDeletePublisher, nil)

	expectedAvatar := &model.Avatar{
		ID:     "test-id",
		UserID: "user-123",
		S3Key:  "avatars/user-123/test-id",
	}
	mockRepo.On("GetByID", mock.Anything, "test-id").Return(expectedAvatar, nil)
	mockRepo.On("SoftDelete", mock.Anything, "test-id", "user-123").Return(nil)
	mockDeletePublisher.On("PublishEvent", mock.Anything, mock.AnythingOfType("service.AvatarDeleteEvent")).Return(nil)

	err := svc.Delete(context.Background(), "test-id", "user-123")

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
	mockDeletePublisher.AssertExpectations(t)
}

func TestAvatarService_Delete_AccessDenied(t *testing.T) {
	mockRepo := new(MockAvatarRepository)
	mockS3 := new(MockS3Client)
	mockPublisher := new(MockPublisher)
	mockDeletePublisher := new(MockPublisher)
	svc := NewAvatarService(mockRepo, mockS3, mockPublisher, mockDeletePublisher, nil)

	expectedAvatar := &model.Avatar{
		ID:     "test-id",
		UserID: "user-456",
		S3Key:  "avatars/user-456/test-id",
	}
	mockRepo.On("GetByID", mock.Anything, "test-id").Return(expectedAvatar, nil)

	err := svc.Delete(context.Background(), "test-id", "user-123")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
	mockRepo.AssertExpectations(t)
	mockRepo.AssertNotCalled(t, "SoftDelete")
}
