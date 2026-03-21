package main

import (
	"log"
	"net/http"

	"mobilevc/internal/config"
	"mobilevc/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", ws.NewHandler(cfg.AuthToken))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	addr := ":" + cfg.Port
	log.Printf("server listening on %s (runtime default=%s/%s debug=%v enhanced=%v)", addr, cfg.Runtime.DefaultCommand, cfg.Runtime.DefaultMode, cfg.Runtime.Debug, cfg.Runtime.EnhancedProjection)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
