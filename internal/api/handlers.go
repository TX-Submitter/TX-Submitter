// Package api provides HTTP handlers for the Stellar TX Submitter.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gamp/stellar-tx-submitter/internal/metrics"
	"github.com/gamp/stellar-tx-submitter/internal/state"
	"github.com/shopspring/decimal"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	store state.Store
}

// NewHandlers creates new HTTP handlers.
func NewHandlers(store state.Store) *Handlers {
	return &Handlers{store: store}
}

// --- Request/Response types ---

// SubmitRequest is the body of a /submit POST.
type SubmitRequest struct {
	ExternalID  string `json:"external_id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Amount      string `json:"amount"`
	AssetCode   string `json:"asset_code"`
	AssetIssuer string `json:"asset_issuer"`
}

// SubmitResponse is the body of a /submit POST response.
type SubmitResponse struct {
	ID          string                   `json:"id"`
	Status      string                   `json:"status"`
	Transitions []*TransitionResponse    `json:"transitions,omitempty"`
}

// TransitionResponse is a single transition record for API output.
type TransitionResponse struct {
	FromStatus  *string `json:"from_status,omitempty"`
	ToStatus    string  `json:"to_status"`
	Detail      string  `json:"detail,omitempty"`
	AttemptedAt string  `json:"attempted_at"`
}

// --- Handlers ---

// Health returns 200 when the service is running.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready returns 200 when the service is ready to accept traffic.
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Submit handles POST /submit to enqueue a new payment.
func (h *Handlers) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Source == "" || req.Destination == "" || req.Amount == "" {
		http.Error(w, "source, destination, and amount are required", http.StatusBadRequest)
		return
	}

	tx, err := h.store.CreateTransaction(r.Context(), state.NewTransactionParams{
		ExternalID:      req.ExternalID,
		SourceAccount:   req.Source,
		Destination:     req.Destination,
		Amount:          decimalMustParse(req.Amount),
		AssetCode:       req.AssetCode,
		AssetIssuer:     req.AssetIssuer,
	})
	if err != nil {
		http.Error(w, "failed to create transaction", http.StatusConflict)
		return
	}

	metrics.IncrementSubmission("pending")

	writeJSON(w, http.StatusCreated, SubmitResponse{
		ID:     tx.ID,
		Status: string(tx.Status),
	})
}

// Transaction handles GET /transactions/{id} to get status and history.
func (h *Handlers) Transaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	txID := extractIDFromPath(r.URL.Path)
	if txID == "" {
		http.Error(w, "transaction ID required", http.StatusBadRequest)
		return
	}

	tx, err := h.store.GetTransaction(r.Context(), txID)
	if err == state.ErrNotFound {
		http.Error(w, "transaction not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to get transaction", http.StatusInternalServerError)
		return
	}

	transitions, _ := h.store.TransitionHistory(r.Context(), txID)
	resp := SubmitResponse{
		ID:     tx.ID,
		Status: string(tx.Status),
	}

	for _, t := range transitions {
		tr := TransitionResponse{
			ToStatus:    string(t.ToStatus),
			AttemptedAt: t.AttemptedAt.Format("2006-01-02T15:04:05Z"),
		}
		if t.FromStatus != nil {
			fs := string(*t.FromStatus)
			tr.FromStatus = &fs
		}
		if t.Detail != nil {
			tr.Detail = *t.Detail
		}
		resp.Transitions = append(resp.Transitions, &tr)
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func decimalMustParse(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func extractIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "transactions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
