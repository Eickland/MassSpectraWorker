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
	PeakPicking    string
	CwtPeakSpace   float64       // для cwt: мин. расстояние между пиками, m/z (по умолчанию 0.001)
	CwtSNR         float64       // для cwt: мин. сигнал/шум (по умолчанию 1)
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
		// Умолчания для формы «Конвертация (msconvert)»; в каждой задаче их можно изменить
		PeakPicking:  os.Getenv("PEAK_PICKING"), // vendor | cwt | none
		CwtSNR:       getFloat("CWT_SNR", 0),
		CwtPeakSpace: getFloat("CWT_PEAK_SPACE", 0),
		GRPCTimeout:  getDuration("GRPC_TIMEOUT", 200*time.Second),
		Workers:      getInt("WORKERS", 5),
		HTTPAddr:     getEnv("HTTP_ADDR", ":8080"),
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

func getFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}
