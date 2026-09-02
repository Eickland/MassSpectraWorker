package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// BrowseEntry - одна папка в списке содержимого текущего каталога
type BrowseEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// BrowseRoot - точка быстрого доступа в боковой панели (домашняя папка,
// корень ФС, примонтированные диски и т.п.)
type BrowseRoot struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BrowseResponse - ответ GET /api/browse
type BrowseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent,omitempty"`
	Roots   []BrowseRoot  `json:"roots,omitempty"`
	Entries []BrowseEntry `json:"entries"`
}

// BrowseHandler - обработчик серверного обозревателя папок.
// Показывает файловую систему той машины, где запущен Go-сервер
// (в WSL это включает примонтированные Windows-диски в /mnt/*).
type BrowseHandler struct{}

func NewBrowseHandler() *BrowseHandler {
	return &BrowseHandler{}
}

// Browse - GET /api/browse?path=/some/dir
// Без параметра path возвращает содержимое домашней папки процесса.
// Возвращает только подпапки (файлы не показываются - выбираем каталог),
// скрывает dot-файлы/dot-папки.
func (h *BrowseHandler) Browse(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")

	var target string
	if reqPath == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = string(os.PathSeparator)
		}
		target = home
	} else {
		target = reqPath
	}

	target = filepath.Clean(target)

	info, err := os.Stat(target)
	if err != nil {
		http.Error(w, "Path not found: "+err.Error(), http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		http.Error(w, "Path is not a directory", http.StatusBadRequest)
		return
	}

	dirEntries, err := os.ReadDir(target)
	if err != nil {
		http.Error(w, "Failed to read directory: "+err.Error(), http.StatusForbidden)
		return
	}

	entries := make([]BrowseEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // скрываем dotfiles/dot-папки
		}

		isDir := de.IsDir()
		// Символьная ссылка на папку тоже считается папкой
		if !isDir && de.Type()&os.ModeSymlink != 0 {
			if fi, statErr := os.Stat(filepath.Join(target, name)); statErr == nil && fi.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue // файлы не показываем - выбираем только каталог
		}

		entries = append(entries, BrowseEntry{
			Name:  name,
			Path:  filepath.Join(target, name),
			IsDir: true,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := filepath.Dir(target)
	if parent == target {
		parent = "" // уже в корне - подниматься некуда
	}

	resp := BrowseResponse{
		Path:    target,
		Parent:  parent,
		Roots:   listRoots(),
		Entries: entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// listRoots - быстрые точки входа для боковой панели обозревателя:
// домашняя папка, корень файловой системы, и (на Linux/WSL) примонтированные
// диски из /mnt/*, либо (на Windows) буквы доступных дисков.
func listRoots() []BrowseRoot {
	var roots []BrowseRoot

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, BrowseRoot{Name: "Домашняя папка", Path: home})
	}

	switch runtime.GOOS {
	case "windows":
		for c := 'A'; c <= 'Z'; c++ {
			drive := string(c) + ":\\"
			if _, err := os.Stat(drive); err == nil {
				roots = append(roots, BrowseRoot{Name: "Диск " + string(c) + ":", Path: drive})
			}
		}
	default:
		roots = append(roots, BrowseRoot{Name: "/ (корень)", Path: "/"})

		// WSL: Windows-диски обычно примонтированы как /mnt/c, /mnt/d, ...
		if mounts, err := os.ReadDir("/mnt"); err == nil {
			for _, m := range mounts {
				if !m.IsDir() {
					continue
				}
				name := m.Name()
				if len(name) == 1 { // однобуквенные точки монтирования - "c", "d" и т.п.
					roots = append(roots, BrowseRoot{
						Name: "Диск " + strings.ToUpper(name) + ": (/mnt/" + name + ")",
						Path: "/mnt/" + name,
					})
				}
			}
		}
	}

	return roots
}
