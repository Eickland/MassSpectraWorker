// internal/handlers/web.go
package handlers

import (
	"html/template"
	"net/http"
	"time"
)

// WebHandler - обработчик для HTML страниц
type WebHandler struct {
	templates *template.Template
	data      map[string]interface{}
}

// PageData - данные для рендера страницы
type PageData struct {
	Title   string
	Content template.HTML
	Data    map[string]interface{}
	Year    int
	Version string
}

// NewWebHandler - создает новый WebHandler
func NewWebHandler() *WebHandler {
	// Загружаем все шаблоны
	tmpl := template.Must(template.ParseGlob("templates/*.html"))

	return &WebHandler{
		templates: tmpl,
		data: map[string]interface{}{
			"Version": "1.0.0",
			"Year":    time.Now().Year(),
		},
	}
}

// render - универсальный метод рендеринга
func (h *WebHandler) render(w http.ResponseWriter, templateName string, title string, extraData map[string]interface{}) {
	// Базовые данные
	data := map[string]interface{}{
		"Title":   title,
		"Version": h.data["Version"],
		"Year":    h.data["Year"],
	}

	// Добавляем дополнительные данные
	for key, value := range extraData {
		data[key] = value
	}

	// Проверяем существование шаблона
	if h.templates.Lookup(templateName) == nil {
		http.Error(w, "Template '"+templateName+"' not found", http.StatusNotFound)
		return
	}

	// Рендерим шаблон
	if err := h.templates.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
	}
}

// ============================================================
// HANDLERS - все используют универсальный render()
// ============================================================

// IndexPage - главная страница
func (h *WebHandler) IndexPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "index.html", "Главная", map[string]interface{}{
		"Message": "Добро пожаловать на главную страницу!",
	})
}

// MassListPage - страница обработки масс-листа
func (h *WebHandler) MassListPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "mass_list.html", "Обработка масс-листа", map[string]interface{}{
		"Description": "Загрузите файл с массами для обработки",
	})
}

// BatchMassListPage - страница пакетной обработки
func (h *WebHandler) BatchMassListPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "batch_mass_list.html", "Пакетная обработка", map[string]interface{}{
		"Description": "Загрузите несколько файлов для пакетной обработки",
	})
}

// HealthPage - страница здоровья
func (h *WebHandler) HealthPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "health.html", "Health Check", map[string]interface{}{
		"Status": "OK",
		"Uptime": time.Now().Format("2006-01-02 15:04:05"),
	})
}
