package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/ko5tas/t2/internal/portfolio"
	webfs "github.com/ko5tas/t2/web"
)

var funcMap = template.FuncMap{
	"inc": func(i int) int { return i + 1 },
	"formatGBP": func(v float64) string {
		if v < 0 {
			return fmt.Sprintf("-£%s", formatNumber(-v))
		}
		return fmt.Sprintf("£%s", formatNumber(v))
	},
}

func formatNumber(v float64) string {
	whole := int64(v)
	frac := v - float64(whole)

	// Format with comma separators.
	s := fmt.Sprintf("%d", whole)
	if len(s) > 3 {
		var result []byte
		for i, c := range s {
			if i > 0 && (len(s)-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		s = string(result)
	}

	return fmt.Sprintf("%s.%02d", s, int64(frac*100+0.5))
}

// Handler holds the HTTP handlers for the web dashboard.
type Handler struct {
	service        *portfolio.Service
	indexTmpl      *template.Template
	positionsTmpl  *template.Template
	refreshSeconds int
}

// NewHandler creates a new web handler.
func NewHandler(service *portfolio.Service, refreshInterval time.Duration) *Handler {
	indexTmpl := template.Must(
		template.New("index.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/index.html"),
	)
	positionsTmpl := template.Must(
		template.New("positions.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/positions.html"),
	)

	return &Handler{
		service:        service,
		indexTmpl:      indexTmpl,
		positionsTmpl:  positionsTmpl,
		refreshSeconds: int(refreshInterval.Seconds()),
	}
}

// RegisterRoutes sets up the HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/positions", h.handlePositions)

	staticSub, err := fs.Sub(webfs.StaticFS, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := struct {
		RefreshSeconds int
	}{
		RefreshSeconds: h.refreshSeconds,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.indexTmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handlePositions(w http.ResponseWriter, r *http.Request) {
	summary := h.service.GetSummary()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.positionsTmpl.Execute(w, summary); err != nil {
		log.Printf("template error: %v", err)
	}
}
