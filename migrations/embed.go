// Package migrations хранит SQL-миграции схемы SQLite.
// Файлы *.up.sql встраиваются в бинарник и применяются автоматически
// при старте сервера (см. internal/storage).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
