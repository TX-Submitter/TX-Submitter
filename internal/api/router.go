package api

import "net/http"

// Router sets up all HTTP routes.
func Router(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/ready", h.Ready)
	mux.HandleFunc("/submit", h.Submit)
	mux.HandleFunc("/transactions/", h.Transaction)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "metrics endpoint available on separate port", http.StatusBadGateway)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	return mux
}
