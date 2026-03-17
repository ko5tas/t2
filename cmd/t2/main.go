package main

import (
	"log"
	"net/http"
	"time"

	"github.com/ko5tas/t2/internal/config"
	"github.com/ko5tas/t2/internal/fundamentals"
	"github.com/ko5tas/t2/internal/portfolio"
	"github.com/ko5tas/t2/internal/trading212"
	"github.com/ko5tas/t2/internal/web"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	client := trading212.NewClient(cfg.BaseURL, cfg.APIKey, cfg.APISecret)

	fundsSvc := fundamentals.NewService(cfg.FinnhubAPIKey)
	if cfg.FinnhubAPIKey != "" {
		log.Println("fundamentals enabled (Finnhub for US + Yahoo Finance for EU)")
	} else {
		log.Println("fundamentals enabled (Yahoo Finance only — set finnhub_api_key for US stocks)")
	}

	log.Println("loading instrument and exchange metadata...")
	svc, err := portfolio.NewService(client, fundsSvc)
	if err != nil {
		log.Fatalf("portfolio service: %v", err)
	}
	svc.StartMetadataRefresh()
	svc.StartSummaryRefresh(cfg.RefreshInterval)
	svc.StartReturnsRefresh(cfg.RefreshInterval)
	svc.StartFundamentalsRefresh()

	// Page polls every 30s (cheap — reads from cache). API fetches happen on RefreshInterval.
	handler := web.NewHandler(svc, 30*time.Second, Version)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("starting t2 dashboard on %s (refresh every %s)", cfg.Listen, cfg.RefreshInterval)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
