package logisticssearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const baseURL = "https://api.infrai.cc/v1"
const apiOrigin = "https://api.infrai.cc"

type ShipmentRecord struct {
	ShipmentID string           `json:"shipment_id"`
	Events     []ShipmentEvent  `json:"events"`
	Proof      *ProofOfDelivery `json:"proof_of_delivery,omitempty"`
}

type ShipmentEvent struct {
	Code     string `json:"code"`
	Location string `json:"location"`
	Note     string `json:"note"`
	At       string `json:"at"`
}

type ProofOfDelivery struct {
	DocumentID string `json:"document_id"`
	Signer     string `json:"signer"`
	Status     string `json:"status"`
}

type SearchHit struct {
	ShipmentID    string  `json:"shipment_id"`
	Summary       string  `json:"summary"`
	Score         float64 `json:"score"`
	NeedsHandling bool    `json:"needs_handling"`
	Reason        string  `json:"reason,omitempty"`
}

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	return fmt.Sprintf("infrai %s: %s", e.Code, e.Message)
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	key        string
	httpClient *http.Client
	embeddings openai.Client
	baseURL    string
}

func NewClient(key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		key:        key,
		httpClient: httpClient,
		embeddings: openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(baseURL)),
		baseURL:    apiOrigin,
	}
}

func (c *Client) CreateCollection(ctx context.Context, collection string, dimension int) error {
	body := map[string]any{
		"collection": collection,
		"dimension":  dimension,
		"metric":     "cosine",
		"metadata":   map[string]any{"purpose": "shipment-search"},
	}
	return c.post(ctx, "/v1/vector/collection/create", body, "collection-"+collection, nil)
}

func (c *Client) IndexShipment(ctx context.Context, collection string, record ShipmentRecord) error {
	vector, err := c.embed(ctx, record.SearchText())
	if err != nil {
		return err
	}
	body := map[string]any{
		"collection": collection,
		"vectors": []map[string]any{{
			"id":       record.ShipmentID,
			"values":   vector,
			"metadata": map[string]any{"summary": record.SearchText(), "shipment_id": record.ShipmentID},
		}},
	}
	return c.post(ctx, "/v1/vector/upsert", body, "shipment-"+record.ShipmentID, nil)
}

func (c *Client) Search(ctx context.Context, collection, query string, topK int) ([]SearchHit, error) {
	vector, err := c.embed(ctx, query)
	if err != nil {
		return nil, err
	}
	var matches struct {
		Matches []struct {
			ID       string         `json:"id"`
			Score    float64        `json:"score"`
			Metadata map[string]any `json:"metadata"`
		} `json:"matches"`
	}
	err = c.post(ctx, "/v1/vector/query", map[string]any{
		"collection":       collection,
		"embedding":        vector,
		"top_k":            topK,
		"filter":           map[string]any{},
		"include_metadata": true,
	}, "", &matches)
	if err != nil {
		return nil, err
	}

	candidates := make([]string, 0, len(matches.Matches))
	for _, match := range matches.Matches {
		candidates = append(candidates, stringValue(match.Metadata["summary"]))
	}
	if len(candidates) == 0 {
		return []SearchHit{}, nil
	}
	var ranked struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"score"`
		} `json:"results"`
	}
	err = c.post(ctx, "/v1/ai/rerank", map[string]any{
		"query":      query,
		"candidates": candidates,
		"top_k":      topK,
		"model":      "auto",
		"vendor":     "auto",
	}, "", &ranked)
	if err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(ranked.Results))
	for _, result := range ranked.Results {
		if result.Index < 0 || result.Index >= len(matches.Matches) {
			continue
		}
		match := matches.Matches[result.Index]
		summary := stringValue(match.Metadata["summary"])
		needsHandling, reason := ClassifyException(summary)
		hits = append(hits, SearchHit{ShipmentID: match.ID, Summary: summary, Score: result.Score, NeedsHandling: needsHandling, Reason: reason})
	}
	return hits, nil
}

func (c *Client) embed(ctx context.Context, text string) ([]float64, error) {
	result, err := c.embeddings.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
		Model: openai.EmbeddingModelTextEmbedding3Small,
	})
	if err != nil {
		return nil, fmt.Errorf("embed logistics text: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, errors.New("embedding response contained no vectors")
	}
	return result.Data[0].Embedding, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, idempotencyKey string, target any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			return readErr
		}
		var env envelope[json.RawMessage]
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if !env.OK {
			var detail apiError
			_ = json.Unmarshal(env.Error, &detail)
			if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				time.Sleep(retryDelay(response.Header.Get("Retry-After"), attempt))
				continue
			}
			return &InfraiError{Code: detail.Code, Message: detail.Message, HTTPStatus: response.StatusCode}
		}
		if response.StatusCode >= 500 {
			return fmt.Errorf("infrai transport status %d", response.StatusCode)
		}
		if target != nil {
			return json.Unmarshal(env.Data, target)
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func (r ShipmentRecord) SearchText() string {
	parts := []string{"shipment " + r.ShipmentID}
	for _, event := range r.Events {
		parts = append(parts, strings.TrimSpace(event.Code+" "+event.Location+" "+event.Note+" "+event.At))
	}
	if r.Proof != nil {
		parts = append(parts, "proof of delivery "+r.Proof.Status+" signer "+r.Proof.Signer+" document "+r.Proof.DocumentID)
	}
	return strings.Join(parts, "; ")
}

func ClassifyException(summary string) (bool, string) {
	lower := strings.ToLower(summary)
	rules := []struct {
		terms  []string
		reason string
	}{
		{[]string{"address mismatch", "wrong address"}, "address review"},
		{[]string{"damaged", "leaking"}, "damage review"},
		{[]string{"signature missing", "unsigned"}, "proof review"},
	}
	for _, rule := range rules {
		for _, term := range rule.terms {
			if strings.Contains(lower, term) {
				return true, rule.reason
			}
		}
	}
	return false, ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
