package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type OpRequest struct {
	Text *string `json:"text,omitempty"`
	Deps *struct {
		Normalized     *string  `json:"normalized,omitempty"`
		Transliterated *string  `json:"transliterated,omitempty"`
		Tokens         []string `json:"tokens,omitempty"`
	} `json:"deps,omitempty"`
}

type OpResponse struct {
	Key      string      `json:"key"`
	Value    interface{} `json:"value"`
	CacheHit bool        `json:"cache_hit"`
}

// Global request counter
var requestCounter int64

func main() {
	http.HandleFunc("/op", handleOp)
	http.HandleFunc("/healthz", handleHealth)
	http.HandleFunc("/metrics", handleMetrics)

	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleOp(w http.ResponseWriter, r *http.Request) {
	// Increment request counter
	atomic.AddInt64(&requestCounter, 1)

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req OpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var normalized *string
	if req.Text != nil {
		norm := normalizeText(*req.Text)
		normalized = &norm
	}

	response := OpResponse{
		Key:      "normalized",
		Value:    normalized,
		CacheHit: false,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// normalizeText applies NFKC normalization, lowercasing, whitespace collapsing, and diacritic stripping
func normalizeText(s string) string {
	// NFKC normalization
	imported := norm.NFKC.String(s)
	// Lowercase
	imported = strings.ToLower(imported)
	// Collapse whitespace
	imported = strings.Join(strings.Fields(imported), " ")
	// Strip diacritics
	imported = stripDiacritics(imported)
	return imported
}

func stripDiacritics(s string) string {
	t := norm.NFD.String(s)
	out := make([]rune, 0, len(t))
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	count := atomic.LoadInt64(&requestCounter)
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "# HELP op_requests_total Total number of requests to /op endpoint\n")
	fmt.Fprintf(w, "# TYPE op_requests_total counter\n")
	fmt.Fprintf(w, "op_requests_total %d\n", count)
}
