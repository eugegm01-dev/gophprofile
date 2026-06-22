package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	S3       S3Config
	RabbitMQ RabbitMQConfig
}

type ServerConfig struct {
	Host string
	Port int
}

type DatabaseConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type RabbitMQConfig struct {
	URL         string
	Queue       string
	QueueDelete string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	// Fail early: проверяем обязательные секреты
	required := []string{
		"POSTGRES_PASSWORD",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"RABBITMQ_PASSWORD",
	}
	for _, env := range required {
		if os.Getenv(env) == "" {
			return nil, fmt.Errorf("required environment variable %s is not set", env)
		}
	}

	rabbitUser := getEnv("RABBITMQ_USER", "gopher")
	rabbitPass := os.Getenv("RABBITMQ_PASSWORD")
	rabbitHost := getEnv("RABBITMQ_HOST", "localhost")
	rabbitPort := getEnv("RABBITMQ_PORT", "5672")
	rabbitURL := fmt.Sprintf("amqp://%s:%s@%s:%s/", rabbitUser, rabbitPass, rabbitHost, rabbitPort)

	cfg := &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnvAsInt("SERVER_PORT", 8080),
		},
		Database: DatabaseConfig{
			Host:     getEnv("POSTGRES_HOST", "localhost"),
			Port:     getEnvAsInt("POSTGRES_PORT", 5432),
			User:     getEnv("POSTGRES_USER", "gopher"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			DBName:   getEnv("POSTGRES_DB", "gophprofile"),
			SSLMode:  getEnv("POSTGRES_SSLMODE", "disable"),
		},
		S3: S3Config{
			Endpoint:  getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
			Bucket:    getEnv("S3_BUCKET", "avatars"),
			UseSSL:    getEnvAsBool("S3_USE_SSL", false),
		},
		RabbitMQ: RabbitMQConfig{
			URL:         rabbitURL,
			Queue:       getEnv("RABBITMQ_QUEUE", "avatar_processing"),
			QueueDelete: getEnv("RABBITMQ_QUEUE_DELETE", "avatar_deletion"),
		},
	}

	return cfg, nil
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("✅ Config loaded: server=%s:%d, db=%s:%d",
		cfg.Server.Host, cfg.Server.Port,
		cfg.Database.Host, cfg.Database.Port)
	return cfg
}

func (c *DatabaseConfig) DSN() string {
	return "postgres://" + c.User + ":" + c.Password + "@" +
		c.Host + ":" + strconv.Itoa(c.Port) + "/" + c.DBName +
		"?sslmode=" + c.SSLMode
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true"
	}
	return defaultValue
}
