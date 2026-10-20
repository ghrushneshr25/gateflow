package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Service holds common process configuration loaded from environment.
type Service struct {
	Name        string
	HTTPAddr    string
	Environment string
	LogLevel    string
	DatabaseURL string
	RedisAddr   string
	KafkaBrokers []string
}

func LoadService(name, defaultAddr string) Service {
	return Service{
		Name:         name,
		HTTPAddr:     getenv("HTTP_ADDR", defaultAddr),
		Environment:  getenv("ENVIRONMENT", "local"),
		LogLevel:     getenv("LOG_LEVEL", "info"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		RedisAddr:    getenv("REDIS_ADDR", "localhost:6379"),
		KafkaBrokers: splitCSV(getenv("KAFKA_BROKERS", "localhost:9092")),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func EnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func EnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func Must(err error) {
	if err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
}
