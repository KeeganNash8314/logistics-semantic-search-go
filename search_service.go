package logisticssearch

import (
	"encoding/json"
	"errors"
	"net/http"
)

type SearchService struct {
	Client     *Client
	Collection string
}

func (s SearchService) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /shipments/index", s.indexShipment)
	mux.HandleFunc("POST /search", s.search)
	return mux
}

func (s SearchService) indexShipment(w http.ResponseWriter, r *http.Request) {
	var record ShipmentRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil || record.ShipmentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "shipment_id and a valid JSON body are required"})
		return
	}
	if err := s.Client.IndexShipment(r.Context(), s.Collection, record); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"shipment_id": record.ShipmentID, "status": "indexed"})
}

func (s SearchService) search(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query and a valid JSON body are required"})
		return
	}
	if input.TopK == 0 {
		input.TopK = 5
	}
	hits, err := s.Client.Search(r.Context(), s.Collection, input.Query, input.TopK)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": input.Query, "hits": hits})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *InfraiError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
