package main

import (
	"log"
	"net/http"

	"github.com/ko5tas/t2/internal/config"
	"github.com/ko5tas/t2/internal/portfolio"
	"github.com/ko5tas/t2/internal/trading212"
	"github.com/ko5tas/t2/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	client := trading212.NewClient(cfg.BaseURL, cfg.APIKey, cfg.APISecret)

	log.Println("loading instrument and exchange metadata...")
	svc, err := portfolio.NewService(client)
	if err != nil {
		log.Fatalf("portfolio service: %v", err)
	}
	svc.StartMetadataRefresh()
	svc.StartReturnsRefresh(cfg.RefreshInterval)

	handler := web.NewHandler(svc, cfg.RefreshInterval)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("starting t2 dashboard on %s (refresh every %s)", cfg.Listen, cfg.RefreshInterval)
	if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
