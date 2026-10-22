// config.go — только чтение окружения
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	DBPath         string // файл SQLite
	ResultsDir     string
	WorkDir        string
	MsConvert      string
	ConvertTimeout time.Duration
	GRPCTimeout    time.Duration // таймаут одного вызова Python-сервиса
	Workers        int
	HTTPAddr       string
}

func LoadConfig() Config {
	return Config{
		DBPath:         absPath(getEnv("DB_PATH", "./data/massspectra.db")),
		ResultsDir:     absPath(getEnv("RESULTS_DIR", "./results")),
		WorkDir:        absPath(getEnv("WORK_DIR", "./work")),
		MsConvert:      os.Getenv("MSCONVERT_PATH"),
		ConvertTimeout: getDuration("CONVERT_TIMEOUT", 30*time.Minute),
		GRPCTimeout:    getDuration("GRPC_TIMEOUT", 200*time.Second),
		Workers:        getInt("WORKERS", 5),
		HTTPAddr:       getEnv("HTTP_ADDR", ":8080"),
	}
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
