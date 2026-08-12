package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	JWTSecret         string
	WorkerConcurrency int
}

func Load() Config {
	return Config{
		HTTPAddr:          getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:       getenv("DATABASE_URL", "postgres://agenthub:agenthub@localhost:5432/agenthub?sslmode=disable"),
		JWTSecret:         getenv("JWT_SECRET", "dev-secret-change-me"),
		WorkerConcurrency: getenvInt("WORKER_CONCURRENCY", 2),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
