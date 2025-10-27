package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode"
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

	log.Println("Starting slugger server on :8080...")
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
	var slugValue interface{}
	var errorMsg string

	if !validationResult.Valid {
		slugValue = nil
		errorMsg = validationResult.Error
	} else {
		// Use deps.transliterated if available, otherwise use text
		var inputText string
		var hasInput bool

		if req.Deps != nil && req.Deps.Transliterated != nil && *req.Deps.Transliterated != "" {
			inputText = *req.Deps.Transliterated
			hasInput = true
		} else if req.Text != nil && *req.Text != "" {
			inputText = *req.Text
			hasInput = true
		}

		if hasInput {
			// Additional runtime validation
			if len(inputText) > 10000 {
				slugValue = nil
				errorMsg = "Input text too long (max 10000 characters)"
			} else {
				slug := generateSlug(inputText)
				if slug == "" {
					slugValue = nil
					errorMsg = "No valid characters found for slug generation"
				} else {
					slugValue = slug
				}
			}
		} else {
			slugValue = nil
			errorMsg = "No transliterated text in deps or text provided"
		}
	}

	response := OpResponse{
		Key:      "slug",
		Value:    slugValue,
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

// generateSlug creates a URL-friendly slug from input text
func generateSlug(s string) string {
	// Convert to lowercase
	text := strings.ToLower(s)

	// Replace any non-alphanumeric characters with spaces
	reg := regexp.MustCompile(`[^a-z0-9\s]+`)
	text = reg.ReplaceAllString(text, " ")

	// Split into words and filter out empty strings
	words := strings.Fields(text)
	var validWords []string

	for _, word := range words {
		// Only keep words that contain alphanumeric characters
		if regexp.MustCompile(`[a-z0-9]`).MatchString(word) {
			validWords = append(validWords, word)
		}
	}

	if len(validWords) == 0 {
		return ""
	}

	// Join words with hyphens
	slug := strings.Join(validWords, "-")

	// Ensure max 64 characters
	if len(slug) > 64 {
		// Try to truncate at word boundaries
		slug = truncateSlugAtWordBoundary(slug, 64)

		// If still too long, hard truncate
		if len(slug) > 64 {
			slug = slug[:64]
			// Remove trailing hyphen if present
			slug = strings.TrimSuffix(slug, "-")
		}
	}

	// Clean up any edge cases (double hyphens, leading/trailing hyphens)
	slug = cleanupSlug(slug)

	return slug
}

// truncateSlugAtWordBoundary tries to truncate at the last complete word within the limit
func truncateSlugAtWordBoundary(slug string, maxLen int) string {
	if len(slug) <= maxLen {
		return slug
	}

	// Find the last hyphen before the limit
	truncated := slug[:maxLen]
	lastHyphen := strings.LastIndex(truncated, "-")

	if lastHyphen > 0 {
		return slug[:lastHyphen]
	}

	return truncated
}

// cleanupSlug removes duplicate hyphens and trims leading/trailing hyphens
func cleanupSlug(slug string) string {
	// Remove duplicate hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	// Trim leading and trailing hyphens
	slug = strings.Trim(slug, "-")

	return slug
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
