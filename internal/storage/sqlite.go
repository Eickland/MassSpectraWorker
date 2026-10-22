// Package storage открывает базу SQLite и применяет миграции схемы.
package storage

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // чистый Go, без cgo — собирается и на Windows, и в WSL

	"MassSpectraWorker/migrations"
)

// DriverName - имя драйвера database/sql (modernc.org/sqlite регистрирует "sqlite")
var DriverName = "sqlite"

// Open открывает (или создаёт) файл БД и применяет недостающие миграции.
func Open(path string) (*sqlx.DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	// Параметры соединения для modernc.org/sqlite
	dsn := path +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sqlx.Open(DriverName, dsn)
	if err != nil {
		return nil, err
	}

	// SQLite допускает только одного писателя. Одно соединение исключает
	// SQLITE_BUSY между воркерами и HTTP-хендлерами; нагрузки здесь
	// (десятки запросов в секунду) для этого более чем достаточно.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Дублируем ключевые PRAGMA явно — на случай другого драйвера
	for _, p := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}

	if err := Migrate(db, migrations.FS); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type migration struct {
	version int
	name    string
}

// Migrate применяет файлы NNNN_*.up.sql, номер которых больше PRAGMA user_version.
func Migrate(db *sqlx.DB, fsys fs.FS) error {
	var current int
	if err := db.Get(&current, "PRAGMA user_version"); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	entries, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return err
	}
	var list []migration
	for _, name := range entries {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			continue
		}
		list = append(list, migration{version: v, name: name})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })

	for _, m := range list {
		if m.version <= current {
			continue
		}
		body, err := fs.ReadFile(fsys, m.name)
		if err != nil {
			return err
		}
		tx, err := db.Beginx()
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w\n%s", m.name, err, stmt)
			}
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s commit: %w", m.name, err)
		}
		log.Printf("db: applied migration %s", m.name)
		current = m.version
	}
	return nil
}

// splitStatements - простой разборщик: убирает комментарии "--" и делит по ";".
// Подходит для наших миграций (в них нет строковых литералов с ";" или "--").
func splitStatements(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var out []string
	for _, s := range strings.Split(b.String(), ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
