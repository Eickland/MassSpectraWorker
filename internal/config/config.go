// config.go — только чтение окружения
package config

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	ResultsDir     string
	WorkDir        string
	MsConvert      string
	ConvertTimeout time.Duration
	GRPCTimeout    time.Duration
}

func LoadConfig() Config {
	work := getEnv("WORK_DIR", "./work")
	if abs, err := filepath.Abs(work); err == nil {
		work = abs
	}
	results := getEnv("RESULTS_DIR", "./results")
	if abs, err := filepath.Abs(results); err == nil {
		results = abs
	}
	return Config{
		ResultsDir:     results,
		WorkDir:        work,
		MsConvert:      os.Getenv("MSCONVERT_PATH"),
		ConvertTimeout: getDuration("CONVERT_TIMEOUT", 30*time.Minute),
		GRPCTimeout:    getDuration("GRPC_TIMEOUT", 200*time.Second),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
