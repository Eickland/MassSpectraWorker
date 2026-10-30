package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"MassSpectraWorker/internal/config"
	"MassSpectraWorker/internal/model"
)

// MsConvertHandler - сведения о msconvert для веб-формы
type MsConvertHandler struct {
	cfg config.Config
}

func NewMsConvertHandler(cfg config.Config) *MsConvertHandler {
	return &MsConvertHandler{cfg: cfg}
}

type msconvertInfo struct {
	Configured bool                `json:"configured"` // задан MSCONVERT_PATH
	Found      bool                `json:"found"`      // файл по этому пути существует
	Path       string              `json:"path"`
	Defaults   model.ConvertParams `json:"defaults"`
}

// Info - GET /api/msconvert
func (h *MsConvertHandler) Info(w http.ResponseWriter, r *http.Request) {
	info := msconvertInfo{
		Configured: h.cfg.MsConvert != "",
		Path:       h.cfg.MsConvert,
		Defaults:   model.DefaultConvert(),
	}
	if info.Configured {
		_, err := os.Stat(h.cfg.MsConvert)
		info.Found = err == nil
	}
	writeJSON(w, http.StatusOK, info)
}

type msconvertPreview struct {
	Params  model.ConvertParams `json:"params"` // после подстановки умолчаний
	Filters []string            `json:"filters"`
	Command string              `json:"command"`
	Tag     string              `json:"tag"`
	Error   string              `json:"error,omitempty"`
}

// Preview - POST /api/msconvert/preview
// Тело — параметры конвертации (частичные допустимы); ответ — команда msconvert,
// которая будет запущена, или ошибка валидации (200, чтобы форма показала её у поля).
func (h *MsConvertHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var c model.ConvertParams
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&c); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	c.ApplyDefaults()
	resp := msconvertPreview{Params: c}
	if err := c.Validate(); err != nil {
		resp.Error = err.Error()
	} else {
		exe := h.cfg.MsConvert
		if exe == "" {
			exe = "msconvert"
		}
		resp.Filters = c.Filters()
		resp.Command = model.CommandLine(exe, "<спектр>.d", "<папка кэша>", c)
		resp.Tag = c.Tag()
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
