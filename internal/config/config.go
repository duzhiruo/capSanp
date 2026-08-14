package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr          string
	DatabaseDSN   string
	UploadDir     string
	AutoMigrate   bool
	LLMBaseURL    string
	LLMAPIKey     string
	LLMModel      string
	LLMProvider   string
	LLMTimeout    time.Duration
	OCRBinaryPath string
	OCRScriptPath string
	OCRTimeout    time.Duration
	LLMInputCost  float64
	LLMOutputCost float64

	WorkerPollInterval time.Duration
	WorkerConcurrency  int
	WorkerLockTimeout  time.Duration
	WorkerMaxAttempts  int

	EmbeddingModel     string
	EmbeddingDimension int
	QdrantHost         string
	QdrantPort         int
	QdrantCollection   string
	QdrantEnabled      bool
}

func Load() Config {
	return Config{
		Addr:          getEnv("ADDR", ":8080"),
		DatabaseDSN:   getEnv("DATABASE_DSN", "root:password@tcp(127.0.0.1:3306)/capsnap?parseTime=true&multiStatements=true"),
		UploadDir:     getEnv("UPLOAD_DIR", "data/uploads"),
		AutoMigrate:   getBool("AUTO_MIGRATE", true),
		LLMBaseURL:    getEnv("LLM_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		LLMAPIKey:     os.Getenv("LLM_API_KEY"),
		LLMModel:      getEnv("LLM_MODEL", "qwen-turbo"),
		LLMProvider:   getEnv("LLM_PROVIDER", "openai_compatible"),
		LLMTimeout:    time.Duration(getInt("LLM_TIMEOUT_SECONDS", 30)) * time.Second,
		OCRBinaryPath: getEnv("OCR_BINARY_PATH", ".cache/bin/ocr_vision"),
		OCRScriptPath: getEnv("OCR_SCRIPT_PATH", "tools/ocr_vision.swift"),
		OCRTimeout:    time.Duration(getInt("OCR_TIMEOUT_SECONDS", 30)) * time.Second,
		LLMInputCost:  getFloat("LLM_INPUT_COST_PER_1M", 0),
		LLMOutputCost: getFloat("LLM_OUTPUT_COST_PER_1M", 0),

		WorkerPollInterval: time.Duration(getInt("WORKER_POLL_INTERVAL", 3)) * time.Second,
		WorkerConcurrency:  getInt("WORKER_CONCURRENCY", 2),
		WorkerLockTimeout:  time.Duration(getInt("WORKER_LOCK_TIMEOUT", 300)) * time.Second,
		WorkerMaxAttempts:  getInt("WORKER_MAX_ATTEMPTS", 3),

		EmbeddingModel:     getEnv("EMBEDDING_MODEL", "text-embedding-v3"),
		EmbeddingDimension: getInt("EMBEDDING_DIMENSION", 1024),
		QdrantHost:         getEnv("QDRANT_HOST", "localhost"),
		QdrantPort:         getInt("QDRANT_PORT", 6334),
		QdrantCollection:   getEnv("QDRANT_COLLECTION", "screenshots"),
		QdrantEnabled:      getBool("QDRANT_ENABLED", true),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
