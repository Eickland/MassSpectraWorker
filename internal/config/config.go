package config

import "os"

type Config struct {
	ResultsDir string
}

func LoadConfig() Config {
	dir := os.Getenv("RESULTS_DIR")
	if dir == "" {
		dir = "./results"
	}
	return Config{ResultsDir: dir}
}
