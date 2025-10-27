package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode"

	"golang.org/x/text/transform"
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

	log.Println("Starting transliterator server on :8080...")
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
	var transliteratedValue interface{}
	var errorMsg string

	if !validationResult.Valid {
		transliteratedValue = nil
		errorMsg = validationResult.Error
	} else {
		// Use deps.normalized if available, otherwise use text
		var inputText string
		var hasInput bool

		if req.Deps != nil && req.Deps.Normalized != nil && *req.Deps.Normalized != "" {
			inputText = *req.Deps.Normalized
			hasInput = true
		} else if req.Text != nil && *req.Text != "" {
			inputText = *req.Text
			hasInput = true
		}

		if hasInput {
			// Additional runtime validation
			if len(inputText) > 10000 {
				transliteratedValue = nil
				errorMsg = "Input text too long (max 10000 characters)"
			} else {
				transliterated := transliterateText(inputText)
				transliteratedValue = transliterated
			}
		} else {
			transliteratedValue = nil
			errorMsg = "No normalized text in deps or text provided"
		}
	}

	response := OpResponse{
		Key:      "transliterated",
		Value:    transliteratedValue,
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

// transliterateText performs ASCII-ish transliteration with ligature replacement
func transliterateText(s string) string {
	// First normalize using NFD to decompose characters
	normalized := norm.NFD.String(s)

	// Replace common ligatures first
	text := replaceLigatures(normalized)

	// Remove diacritics by filtering out combining marks
	text = removeDiacritics(text)

	// Apply additional ASCII transliterations
	text = applyASCIITransliterations(text)

	return text
}

// replaceLigatures replaces common ligatures with ASCII equivalents
func replaceLigatures(s string) string {
	ligatures := map[string]string{
		"æ": "ae", "Æ": "AE",
		"œ": "oe", "Œ": "OE",
		"ß": "ss",
		"ﬀ": "ff", "ﬁ": "fi", "ﬂ": "fl",
		"ﬃ": "ffi", "ﬄ": "ffl",
		"ﬅ": "st", "ﬆ": "st",
		"ij": "ij", "IJ": "IJ",
		"ł": "l", "Ł": "L",
		"ø": "o", "Ø": "O",
		"đ": "d", "Đ": "D",
		"þ": "th", "Þ": "TH",
		"ð": "dh", "Ð": "DH",
	}

	result := s
	for ligature, replacement := range ligatures {
		result = strings.ReplaceAll(result, ligature, replacement)
	}

	return result
}

// removeDiacritics removes combining diacritical marks
func removeDiacritics(s string) string {
	t := transform.Chain(norm.NFD, transform.RemoveFunc(func(r rune) bool {
		return unicode.Is(unicode.Mn, r) // Remove nonspacing marks
	}), norm.NFC)

	result, _, _ := transform.String(t, s)
	return result
}

// applyASCIITransliterations applies additional character-to-ASCII mappings
func applyASCIITransliterations(s string) string {
	transliterations := map[rune]string{
		'α': "a", 'β': "b", 'γ': "g", 'δ': "d", 'ε': "e",
		'ζ': "z", 'η': "h", 'θ': "th", 'ι': "i", 'κ': "k",
		'λ': "l", 'μ': "m", 'ν': "n", 'ξ': "x", 'ο': "o",
		'π': "p", 'ρ': "r", 'σ': "s", 'ς': "s", 'τ': "t",
		'υ': "u", 'φ': "ph", 'χ': "ch", 'ψ': "ps", 'ω': "w",
		'Α': "A", 'Β': "B", 'Γ': "G", 'Δ': "D", 'Ε': "E",
		'Ζ': "Z", 'Η': "H", 'Θ': "TH", 'Ι': "I", 'Κ': "K",
		'Λ': "L", 'Μ': "M", 'Ν': "N", 'Ξ': "X", 'Ο': "O",
		'Π': "P", 'Ρ': "R", 'Σ': "S", 'Τ': "T", 'Υ': "U",
		'Φ': "PH", 'Χ': "CH", 'Ψ': "PS", 'Ω': "W",
	}

	var result strings.Builder
	for _, r := range s {
		if replacement, exists := transliterations[r]; exists {
			result.WriteString(replacement)
		} else if r <= 127 { // Keep ASCII characters as-is
			result.WriteRune(r)
		} else {
			// For other non-ASCII characters, try to keep them or replace with ?
			result.WriteRune(r)
		}
	}

	return result.String()
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
