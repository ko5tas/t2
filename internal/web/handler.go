package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ko5tas/t2/internal/portfolio"
	webfs "github.com/ko5tas/t2/web"
)

var funcMap = template.FuncMap{
	"formatGBP": func(v float64) string {
		if v < 0 {
			return fmt.Sprintf("-£%s", formatNumber(-v))
		}
		return fmt.Sprintf("£%s", formatNumber(v))
	},
	"formatQty": func(v float64) string {
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%.4f", v)
	},
	"formatPct": func(v float64) string {
		return fmt.Sprintf("%.2f%%", v)
	},
	"returnClass": func(v float64) string {
		if v > 0 {
			return "positive"
		}
		if v < 0 {
			return "negative"
		}
		return ""
	},
	"nextDir": func(currentSort, currentDir, col string) string {
		return nextDir(currentSort, currentDir, col)
	},
	"eq2": func(a, b string) bool { return a == b },
	"formatPrice": func(price float64, currency string, priceGBP float64) string {
		symbols := map[string]string{
			"USD": "$", "EUR": "€", "CHF": "CHF ", "SEK": "SEK ",
			"NOK": "NOK ", "DKK": "DKK ", "PLN": "PLN ", "CZK": "CZK ",
			"HUF": "HUF ", "RON": "RON ",
		}
		switch currency {
		case "GBP":
			return fmt.Sprintf("£%.2f", price)
		case "GBX":
			return fmt.Sprintf("£%.2f", price/100)
		default:
			sym, ok := symbols[currency]
			if !ok {
				sym = currency + " "
			}
			return fmt.Sprintf("%s%.2f (£%.2f)", sym, price, priceGBP)
		}
	},
	"formatBigNum": func(v *float64, fetched bool, isETF bool) string {
		if v == nil {
			if isETF {
				return "N/A"
			}
			return "—"
		}
		n := *v
		neg := ""
		if n < 0 {
			neg = "-"
			n = -n
		}
		switch {
		case n >= 1e12:
			return fmt.Sprintf("%s$%.1fT", neg, n/1e12)
		case n >= 1e9:
			return fmt.Sprintf("%s$%.1fB", neg, n/1e9)
		case n >= 1e6:
			return fmt.Sprintf("%s$%.1fM", neg, n/1e6)
		case n >= 1e3:
			return fmt.Sprintf("%s$%.1fK", neg, n/1e3)
		default:
			return fmt.Sprintf("%s$%.0f", neg, n)
		}
	},
	"formatFund": func(v *float64, fetched bool, isETF bool) string {
		if v == nil {
			if isETF {
				return "N/A"
			}
			return "—"
		}
		return fmt.Sprintf("%.2f", *v)
	},
	"formatFundPct": func(v *float64, fetched bool, isETF bool) string {
		if v == nil {
			if isETF {
				return "N/A"
			}
			return "—"
		}
		return fmt.Sprintf("%.2f%%", *v)
	},
	"performanceClass": func(v float64) string {
		if v < 0 {
			return "perf-negative"
		}
		if v >= 100 {
			return "perf-legendary"
		}
		if v >= 50 {
			return "perf-positive"
		}
		if v >= 25 {
			return "perf-good"
		}
		if v >= 0 && v <= 4 {
			return "perf-warning perf-blink"
		}
		if v < 10 {
			return "perf-warning"
		}
		return ""
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
			result = append(result, byte(c)) //nolint:gosec // c is always a digit (0-9)
		}
		s = string(result)
	}

	return fmt.Sprintf("%s.%02d", s, int64(frac*100+0.5))
}

// Handler holds the HTTP handlers for the web dashboard.
type Handler struct {
	service          *portfolio.Service
	indexTmpl        *template.Template
	positionsTmpl    *template.Template
	positionRowTmpl  *template.Template
	historyTmpl      *template.Template
	refreshSeconds   int
	version          string
}

// NewHandler creates a new web handler.
func NewHandler(service *portfolio.Service, refreshInterval time.Duration, version string) *Handler {
	indexTmpl := template.Must(
		template.New("index.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/index.html"),
	)
	positionsTmpl := template.Must(
		template.New("positions.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/positions.html"),
	)
	positionRowTmpl := template.Must(
		template.New("position_row.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/position_row.html"),
	)
	historyTmpl := template.Must(
		template.New("history.html").Funcs(funcMap).ParseFS(webfs.TemplateFS, "templates/history.html"),
	)

	return &Handler{
		service:         service,
		indexTmpl:       indexTmpl,
		positionsTmpl:   positionsTmpl,
		positionRowTmpl: positionRowTmpl,
		historyTmpl:     historyTmpl,
		refreshSeconds:  int(refreshInterval.Seconds()),
		version:         version,
	}
}

// RegisterRoutes sets up the HTTP routes.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/positions", h.handlePositions)
	mux.HandleFunc("/position/", h.handlePosition)
	mux.HandleFunc("/history", h.handleHistory)

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
		Version        string
	}{
		RefreshSeconds: h.refreshSeconds,
		Version:        h.version,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.indexTmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handlePosition(w http.ResponseWriter, r *http.Request) {
	rawTicker := strings.TrimPrefix(r.URL.Path, "/position/")
	if rawTicker == "" {
		http.NotFound(w, r)
		return
	}

	pos := h.service.GetPosition(rawTicker)
	if pos == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.positionRowTmpl.Execute(w, pos); err != nil {
		log.Printf("template error: %v", err)
	}
}

// sortField maps query param values to position field accessors.
var sortFields = map[string]func(p portfolio.Position) float64{
	"market_value":   func(p portfolio.Position) float64 { return p.MarketValue },
	"recovered":      func(p portfolio.Position) float64 { return p.Return },
	"invested":       func(p portfolio.Position) float64 { return p.Invested },
	"recovered_pct":  func(p portfolio.Position) float64 { return p.ReturnPct },
	"performance":    func(p portfolio.Position) float64 { return p.PerformancePct },
	"qty":            func(p portfolio.Position) float64 { return p.Quantity },
	"price":          func(p portfolio.Position) float64 { return p.CurrentPriceGBP },
	"div_yield":      func(p portfolio.Position) float64 { return p.DividendYieldPct },
	"pe":             func(p portfolio.Position) float64 { return derefF(p.PERatio) },
	"market_cap":     func(p portfolio.Position) float64 { return derefF(p.MarketCapM) },
	"eps":            func(p portfolio.Position) float64 { return derefF(p.EPS) },
	"eps_growth":     func(p portfolio.Position) float64 { return derefF(p.EPSGrowthPct) },
	"revenue":        func(p portfolio.Position) float64 { return derefF(p.RevenueM) },
	"profit_margin":  func(p portfolio.Position) float64 { return derefF(p.ProfitMarginPct) },
}

func derefF(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

var sortStringFields = map[string]func(p portfolio.Position) string{
	"name":          func(p portfolio.Position) string { return p.Ticker },
	"exchange":      func(p portfolio.Position) string { return p.Exchange },
	"first_bought":  func(p portfolio.Position) string { return p.FirstBought },
}

type positionsData struct {
	*portfolio.Summary
	Sort           string
	Dir            string
	RefreshSeconds int
}

// nextDir returns the toggled direction for a column header.
func nextDir(currentSort, currentDir, col string) string {
	if currentSort == col {
		if currentDir == "desc" {
			return "asc"
		}
		return "desc"
	}
	return "desc" // default to descending when clicking a new column
}

func (h *Handler) handlePositions(w http.ResponseWriter, r *http.Request) {
	summary := h.service.GetSummary()

	sortBy := r.URL.Query().Get("sort")
	dir := r.URL.Query().Get("dir")
	if sortBy == "" {
		sortBy = "market_value"
	}
	if dir == "" {
		dir = "desc"
	}
	asc := dir == "asc"

	// Sort positions
	if fn, ok := sortFields[sortBy]; ok {
		sort.SliceStable(summary.Positions, func(i, j int) bool {
			if asc {
				return fn(summary.Positions[i]) < fn(summary.Positions[j])
			}
			return fn(summary.Positions[i]) > fn(summary.Positions[j])
		})
	} else if fn, ok := sortStringFields[sortBy]; ok {
		sort.SliceStable(summary.Positions, func(i, j int) bool {
			if asc {
				return strings.ToLower(fn(summary.Positions[i])) < strings.ToLower(fn(summary.Positions[j]))
			}
			return strings.ToLower(fn(summary.Positions[i])) > strings.ToLower(fn(summary.Positions[j]))
		})
	}

	data := positionsData{
		Summary:        summary,
		Sort:           sortBy,
		Dir:            dir,
		RefreshSeconds: h.refreshSeconds,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.positionsTmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	summary := h.service.GetSummary()

	sortBy := r.URL.Query().Get("sort")
	dir := r.URL.Query().Get("dir")
	if sortBy == "" {
		sortBy = "performance"
	}
	if dir == "" {
		dir = "desc"
	}
	asc := dir == "asc"

	positions := summary.ClosedPositions
	if fn, ok := sortFields[sortBy]; ok {
		sort.SliceStable(positions, func(i, j int) bool {
			if asc {
				return fn(positions[i]) < fn(positions[j])
			}
			return fn(positions[i]) > fn(positions[j])
		})
	} else if fn, ok := sortStringFields[sortBy]; ok {
		sort.SliceStable(positions, func(i, j int) bool {
			if asc {
				return strings.ToLower(fn(positions[i])) < strings.ToLower(fn(positions[j]))
			}
			return strings.ToLower(fn(positions[i])) > strings.ToLower(fn(positions[j]))
		})
	}

	var totalInvested, totalReturn float64
	for _, p := range positions {
		totalInvested += p.Invested
		totalReturn += p.Return
	}
	var totalPerfPct float64
	if totalInvested > 0 {
		totalPerfPct = (totalReturn - totalInvested) / totalInvested * 100
	}

	data := struct {
		Positions           []portfolio.Position
		Sort                string
		Dir                 string
		LastUpdated         time.Time
		TotalInvested       float64
		TotalReturn         float64
		TotalProfit         float64
		TotalPerformancePct float64
	}{
		Positions:           positions,
		Sort:                sortBy,
		Dir:                 dir,
		LastUpdated:         summary.LastUpdated,
		TotalInvested:       totalInvested,
		TotalReturn:         totalReturn,
		TotalProfit:         totalReturn - totalInvested,
		TotalPerformancePct: totalPerfPct,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.historyTmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}
