package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Env string

	HTTPAddr string

	MySQLDSN string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	MachineID     int64
	JWTSecret     string

	// MinIO 配置
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
	MinioBucket    string

	// RabbitMQ 配置
	RabbitMQURL string

	// AI Service 配置
	AIServiceURL string

	// SMTP 配置
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFromName string
}

func Load() *Config {
	// 本地开发时从 .env 加载，生产环境中 .env 不存在则静默跳过
	_ = godotenv.Load()

	machineID, err := strconv.ParseInt(getEnv("MACHINE_ID", "0"), 10, 64)
	if err != nil {
		panic(err)
	}


	cfg := &Config{
		Env:      getEnv("APP_ENV", "dev"),
		HTTPAddr: getEnv("HTTP_ADDR", ":8080"),

		MySQLDSN: getEnv("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/context_graph?charset=utf8mb4&parseTime=True&loc=UTC"),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		JWTSecret:     getEnv("JWT_SECRET", "secret_1103783949"),
		MachineID:     machineID,

		// MinIO 配置
		MinioEndpoint:  getEnv("MINIO_ENDPOINT", "127.0.0.1:9000"),
		MinioAccessKey: getEnv("MINIO_ACCESS_KEY", "admin"),
		MinioSecretKey: getEnv("MINIO_SECRET_KEY", "password123"),
		MinioUseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		MinioBucket:    getEnv("MINIO_BUCKET", "context-graph"),

		// RabbitMQ 配置
		RabbitMQURL: getEnv("RABBITMQ_URL", "amqp://guest:guest@127.0.0.1:5672/"),

		AIServiceURL: getEnv("AI_SERVICE_URL", "http://localhost:8001"),

		// SMTP 配置
		SMTPHost:     getEnv("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:     getEnv("SMTP_PORT", "587"),
		SMTPUser:     getEnv("SMTP_USER", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFromName: getEnv("SMTP_FROM_NAME", "ContextGraph"),
	}

	cfg.RedisDB = 0

	return cfg
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
