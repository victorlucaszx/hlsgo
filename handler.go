package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type Handler struct {
	queue *JobQueue
}

func NewHandler(queue *JobQueue) *Handler {
	return &Handler{queue: queue}
}

func (h *Handler) HandleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	var req ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido: " + err.Error()})
		return
	}

	if req.MediaFileID == 0 || req.S3Path == "" || len(req.Qualities) == 0 || req.CallbackURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Campos obrigatórios: media_file_id, s3_path, qualities, callback_url"})
		return
	}

	conversionID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	job := &ConversionJob{
		ID:      conversionID,
		Request: req,
		Cancel:  cancel,
		Ctx:     ctx,
	}

	h.queue.Enqueue(job)

	log.Printf("[HANDLER] Conversão %s criada para media_file_id=%d", conversionID, req.MediaFileID)

	writeJSON(w, http.StatusAccepted, ConvertResponse{
		ConversionID: conversionID,
		Message:      "Conversão iniciada",
	})
}

func (h *Handler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	// Extract conversion_id from path: /api/hls/{conversion_id}
	path := strings.TrimPrefix(r.URL.Path, "/api/hls/")
	conversionID := strings.TrimSuffix(path, "/")

	if conversionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "conversion_id é obrigatório"})
		return
	}

	if h.queue.Cancel(conversionID) {
		log.Printf("[HANDLER] Conversão %s cancelada", conversionID)
		writeJSON(w, http.StatusOK, CancelResponse{
			Success: true,
			Message: "Conversão cancelada",
		})
	} else {
		writeJSON(w, http.StatusNotFound, CancelResponse{
			Success: false,
			Message: "Conversão não encontrada",
		})
	}
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
