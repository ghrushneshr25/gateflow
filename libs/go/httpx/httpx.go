package httpx

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const HeaderCorrelationID = "X-Correlation-ID"

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	CorrelationID string         `json:"correlation_id"`
	Retryable     bool           `json:"retryable"`
	Status        string         `json:"status"`
	Details       map[string]any `json:"details,omitempty"`
}

func CorrelationID(r *http.Request) string {
	if id := r.Header.Get(HeaderCorrelationID); id != "" {
		return id
	}
	return uuid.NewString()
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, httpStatus int, code, message, correlationID, status string, retryable bool) {
	WriteJSON(w, httpStatus, ErrorBody{Error: ErrorDetail{
		Code:          code,
		Message:       message,
		CorrelationID: correlationID,
		Retryable:     retryable,
		Status:        status,
	}})
}

func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := CorrelationID(r)
		w.Header().Set(HeaderCorrelationID, id)
		r.Header.Set(HeaderCorrelationID, id)
		next.ServeHTTP(w, r)
	})
}

type Health struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Time    string `json:"time"`
}

func MountHealth(mux *http.ServeMux, service string, ready func() error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, Health{
			Service: service,
			Status:  "ok",
			Time:    time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(); err != nil {
				WriteError(w, http.StatusServiceUnavailable, "NOT_READY", err.Error(), CorrelationID(r), "failed", true)
				return
			}
		}
		WriteJSON(w, http.StatusOK, Health{
			Service: service,
			Status:  "ready",
			Time:    time.Now().UTC().Format(time.RFC3339),
		})
	})
}

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
