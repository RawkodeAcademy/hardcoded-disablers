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
	Error    string      `json:"error,omitempty"`
}

type ValidationResult struct {
	Valid bool
	Error string
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
	var validationResult ValidationResult

	// Parse and validate JSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		validationResult = ValidationResult{
			Valid: false,
			Error: fmt.Sprintf("Invalid JSON: %s", err.Error()),
		}
	} else {
		validationResult = validateInput(req)
	}

	// Process based on validation result
	var normalizedValue interface{}
	var errorMsg string

	if !validationResult.Valid {
		normalizedValue = nil
		errorMsg = validationResult.Error
	} else if req.Text != nil && *req.Text != "" {
		// Additional runtime validation
		if len(*req.Text) > 10000 { // Limit text length
			normalizedValue = nil
			errorMsg = "Text too long (max 10000 characters)"
		} else {
			normalized := normalizeText(*req.Text)
			normalizedValue = normalized
		}
	} else {
		normalizedValue = nil
		errorMsg = "No text provided or empty text"
	}

	response := OpResponse{
		Key:      "normalized",
		Value:    normalizedValue,
		CacheHit: false,
	}

	// Include error message if present
	if errorMsg != "" {
		response.Error = errorMsg
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// validateInput performs comprehensive input validation
func validateInput(req OpRequest) ValidationResult {
	// Check if request is completely empty
	if req.Text == nil && req.Deps == nil {
		return ValidationResult{
			Valid: false,
			Error: "Request body must contain either 'text' or 'deps' field",
		}
	}

	// Validate text field if present
	if req.Text != nil {
		if len(*req.Text) == 0 {
			return ValidationResult{
				Valid: false,
				Error: "Text field cannot be empty string",
			}
		}

		// Check for invalid characters or encoding issues
		if !isValidUTF8(*req.Text) {
			return ValidationResult{
				Valid: false,
				Error: "Text contains invalid UTF-8 characters",
			}
		}
	}

	// Validate deps structure if present
	if req.Deps != nil {
		if err := validateDeps(*req.Deps); err != "" {
			return ValidationResult{
				Valid: false,
				Error: err,
			}
		}
	}

	return ValidationResult{Valid: true, Error: ""}
}

// validateDeps validates the deps structure
func validateDeps(deps struct {
	Normalized     *string  `json:"normalized,omitempty"`
	Transliterated *string  `json:"transliterated,omitempty"`
	Tokens         []string `json:"tokens,omitempty"`
}) string {
	// Validate normalized field
	if deps.Normalized != nil && len(*deps.Normalized) > 10000 {
		return "deps.normalized too long (max 10000 characters)"
	}

	// Validate transliterated field
	if deps.Transliterated != nil && len(*deps.Transliterated) > 10000 {
		return "deps.transliterated too long (max 10000 characters)"
	}

	// Validate tokens array
	if len(deps.Tokens) > 1000 {
		return "deps.tokens array too large (max 1000 items)"
	}

	for i, token := range deps.Tokens {
		if len(token) > 100 {
			return fmt.Sprintf("deps.tokens[%d] too long (max 100 characters)", i)
		}
	}

	return ""
}

// isValidUTF8 checks if string contains valid UTF-8
func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == unicode.ReplacementChar {
			return false
		}
	}
	return true
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
