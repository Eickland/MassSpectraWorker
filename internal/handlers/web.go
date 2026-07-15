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

	tmpl := template.Must(template.ParseGlob("templates/*.html"))

	return &WebHandler{
		templates: tmpl,
		data: map[string]interface{}{
			"Version": "1.0.0",
			"Year":    time.Now().Year(),
		},
	}
}

// render - рендерит HTML страницу
func (h *WebHandler) render(w http.ResponseWriter, name string, data interface{}) {
	// Добавляем общие данные
	if pageData, ok := data.(*PageData); ok {
		if pageData.Data == nil {
			pageData.Data = make(map[string]interface{})
		}
		pageData.Data["Version"] = h.data["Version"]
		pageData.Data["Year"] = h.data["Year"]
	}

	// Проверяем существование шаблона
	if h.templates.Lookup(name) == nil {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}

	// Рендерим шаблон
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// ============================================================
// HANDLERS
// ============================================================

// HealthPage - страница здоровья
func (h *WebHandler) HealthPage(w http.ResponseWriter, r *http.Request) {
	data := &PageData{
		Title: "Health Check",
	}
	h.render(w, "base.html", data)
}

func (h *WebHandler) MassListPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "Mass List Processing",
	}

	if err := h.templates.ExecuteTemplate(w, "mass_list.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
