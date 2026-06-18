package config

import (
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
	URL   string
	Queue string
}

func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnvAsInt("SERVER_PORT", 8080),
		},
		Database: DatabaseConfig{
			Host:     getEnv("POSTGRES_HOST", "localhost"),
			Port:     getEnvAsInt("POSTGRES_PORT", 5433), // ← 5433!
			User:     getEnv("POSTGRES_USER", "gopher"),
			Password: getEnv("POSTGRES_PASSWORD", "gopher123"),
			DBName:   getEnv("POSTGRES_DB", "gophprofile"),
			SSLMode:  getEnv("POSTGRES_SSLMODE", "disable"),
		},
		S3: S3Config{
			Endpoint:  getEnv("S3_ENDPOINT", "localhost:9000"),
			AccessKey: getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("S3_SECRET_KEY", "minioadmin"),
			Bucket:    getEnv("S3_BUCKET", "avatars"),
			UseSSL:    getEnvAsBool("S3_USE_SSL", false),
		},
		RabbitMQ: RabbitMQConfig{
			URL:   getEnv("RABBITMQ_URL", "amqp://gopher:gopher123@localhost:5672/"),
			Queue: getEnv("RABBITMQ_QUEUE", "avatar_processing"),
		},
	}
}

func (c *DatabaseConfig) DSN() string {
	return "postgres://" + c.User + ":" + c.Password + "@" +
		c.Host + ":" + strconv.Itoa(c.Port) + "/" + c.DBName +
		"?sslmode=" + c.SSLMode
}

func MustLoad() *Config {
	cfg := Load()
	log.Printf("✅ Config loaded: server=%s:%d, db=%s:%d",
		cfg.Server.Host, cfg.Server.Port,
		cfg.Database.Host, cfg.Database.Port)
	return cfg
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
