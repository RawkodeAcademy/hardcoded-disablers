package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
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

type AnalyseRequest struct {
	Text string `json:"text"`
}

type AnalyseResponse struct {
	Normalized     string  `json:"normalized"`
	Transliterated string  `json:"transliterated"`
	Slug           string  `json:"slug"`
	Tokens         int     `json:"tokens"`
	UniqueWords    int     `json:"unique_words"`
	BigramCount    int     `json:"bigram_count"`
	CharCount      int     `json:"char_count"`
	UniqueChars    int     `json:"unique_chars"`
	Hash64         string  `json:"hash64"`
	Entropy        float64 `json:"entropy"`
	Palindrome     bool    `json:"palindrome"`
	Degraded       bool    `json:"degraded"`
}

var (
	normalizerURL     = getEnv("NORMALIZER_URL", "http://normalizer.disablers.svc.cluster.local:8080")
	transliteratorURL = getEnv("TRANSLITERATOR_URL", "http://transliterator.disablers.svc.cluster.local:8082")
	sluggerURL        = getEnv("SLUGGER_URL", "http://slugger.disablers.svc.cluster.local:8083")
	tokenizerURL      = getEnv("TOKENIZER_URL", "http://tokenizer.disablers.svc.cluster.local:8084")
	counterURL        = getEnv("COUNTER_URL", "http://counter.disablers.svc.cluster.local:8085")
	hasherURL         = getEnv("HASHER_URL", "http://hasher.disablers.svc.cluster.local:8086")
	entropyURL        = getEnv("ENTROPY_URL", "http://entropy.disablers.svc.cluster.local:8087")
	palindromeURL     = getEnv("PALINDROME_URL", "http://palindrome.disablers.svc.cluster.local:8088")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	http.HandleFunc("/analyze", handleAnalyze)
	http.HandleFunc("/healthz", handleHealth)

	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AnalyseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "Text field is required", http.StatusBadRequest)
		return
	}

	response, err := aggregateAnalysis(req.Text)
	if err != nil {
		log.Printf("Error aggregating analysis: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func callMicroservice(url string, text string) (*OpResponse, error) {
	payload := OpRequest{Text: &text}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url+"/op", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var opResp OpResponse
	if err := json.Unmarshal(body, &opResp); err != nil {
		return nil, err
	}

	return &opResp, nil
}

func aggregateAnalysis(text string) (*AnalyseResponse, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	response := &AnalyseResponse{
		CharCount: len(text),
		Degraded:  false,
	}

	errors := make([]error, 0)

	// Call normalizer service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(normalizerURL, text); err == nil {
			if normalized, ok := resp.Value.(string); ok {
				mu.Lock()
				response.Normalized = normalized
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("normalizer: %w", err))
			mu.Unlock()
		}
	}()

	// Call transliterator service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(transliteratorURL, text); err == nil {
			if transliterated, ok := resp.Value.(string); ok {
				mu.Lock()
				response.Transliterated = transliterated
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("transliterator: %w", err))
			mu.Unlock()
		}
	}()

	// Call slugger service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(sluggerURL, text); err == nil {
			if slug, ok := resp.Value.(string); ok {
				mu.Lock()
				response.Slug = slug
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("slugger: %w", err))
			mu.Unlock()
		}
	}()

	// Call tokenizer service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(tokenizerURL, text); err == nil {
			if tokens, ok := resp.Value.(float64); ok {
				mu.Lock()
				response.Tokens = int(tokens)
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("tokenizer: %w", err))
			mu.Unlock()
		}
	}()

	// Call counter service for unique words, bigrams, and unique chars
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(counterURL, text); err == nil {
			if data, ok := resp.Value.(map[string]interface{}); ok {
				mu.Lock()
				if uniqueWords, ok := data["unique_words"].(float64); ok {
					response.UniqueWords = int(uniqueWords)
				}
				if bigramCount, ok := data["bigram_count"].(float64); ok {
					response.BigramCount = int(bigramCount)
				}
				if uniqueChars, ok := data["unique_chars"].(float64); ok {
					response.UniqueChars = int(uniqueChars)
				}
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("counter: %w", err))
			mu.Unlock()
		}
	}()

	// Call hasher service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(hasherURL, text); err == nil {
			if hash, ok := resp.Value.(string); ok {
				mu.Lock()
				response.Hash64 = hash
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("hasher: %w", err))
			mu.Unlock()
		}
	}()

	// Call entropy service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(entropyURL, text); err == nil {
			if entropy, ok := resp.Value.(float64); ok {
				mu.Lock()
				response.Entropy = entropy
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("entropy: %w", err))
			mu.Unlock()
		}
	}()

	// Call palindrome service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if resp, err := callMicroservice(palindromeURL, text); err == nil {
			if palindrome, ok := resp.Value.(bool); ok {
				mu.Lock()
				response.Palindrome = palindrome
				mu.Unlock()
			}
		} else {
			mu.Lock()
			errors = append(errors, fmt.Errorf("palindrome: %w", err))
			mu.Unlock()
		}
	}()

	wg.Wait()

	if len(errors) > 0 {
		response.Degraded = true
		log.Printf("Some services failed: %v", errors)
	}

	return response, nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
