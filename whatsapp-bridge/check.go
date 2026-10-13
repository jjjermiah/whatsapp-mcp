package main

// "Is this number on WhatsApp?" support: the not_on_whatsapp send error and
// POST /api/check.
//
//   POST /api/check {"phone_numbers": ["+1 647-669-8464", ...]}
//   -> {"results": [{"input": "...", "jid": "...@lid", "registered": true}, ...]}
//
// Inputs are normalized like send recipients. An input that fails
// normalization gets an "error" message and registered=false instead of
// failing the whole request.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// maxCheckNumbers caps one /api/check request so a single call cannot fan
// out into an unbounded usync query.
const maxCheckNumbers = 50

type checkRequest struct {
	PhoneNumbers []string `json:"phone_numbers"`
}

type checkResult struct {
	Input      string `json:"input"`
	JID        string `json:"jid"`
	Registered bool   `json:"registered"`
	Error      string `json:"error,omitempty"`
}

// notOnWhatsApp is the error for a well-formed number WhatsApp says has no
// account. 422, not 5xx: retrying will not help.
func notOnWhatsApp(number string) *recipientError {
	return &recipientError{
		Status:  http.StatusUnprocessableEntity,
		Code:    errCodeNotOnWhatsApp,
		Message: fmt.Sprintf("%s is not registered on WhatsApp", number),
	}
}

// definitelyNotOnWhatsApp reports whether a single-number IsOnWhatsApp answer
// positively says "not registered": WhatsApp answered for the number and
// none of the answers is registered. An empty answer proves nothing.
func definitelyNotOnWhatsApp(matches []types.IsOnWhatsAppResponse) bool {
	if len(matches) == 0 {
		return false
	}
	for _, m := range matches {
		if m.IsIn {
			return false
		}
	}
	return true
}

// applyCheckMatches fills results from an IsOnWhatsApp answer. queryIdx maps
// each query string ("+<digits>") to the positions in results that asked it.
func applyCheckMatches(results []checkResult, queryIdx map[string][]int, matches []types.IsOnWhatsAppResponse) {
	for _, m := range matches {
		for _, i := range queryIdx[m.Query] {
			results[i].Registered = m.IsIn
			if m.IsIn {
				results[i].JID = m.JID.String()
			}
		}
	}
}

func registerCheckEndpoint(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc, client *whatsmeow.Client) {
	mux.HandleFunc("/api/check", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req checkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.PhoneNumbers) == 0 {
			http.Error(w, "phone_numbers is required", http.StatusBadRequest)
			return
		}
		if len(req.PhoneNumbers) > maxCheckNumbers {
			http.Error(w, fmt.Sprintf("phone_numbers has %d entries; at most %d per request", len(req.PhoneNumbers), maxCheckNumbers), http.StatusBadRequest)
			return
		}

		results := make([]checkResult, len(req.PhoneNumbers))
		queryIdx := make(map[string][]int, len(req.PhoneNumbers))
		var queries []string
		for i, input := range req.PhoneNumbers {
			results[i].Input = input
			digits, err := normalizeRecipient(input)
			if err != nil {
				results[i].Error = err.Error()
				continue
			}
			if strings.Contains(digits, "@") {
				results[i].Error = "expected a phone number, not a JID"
				continue
			}
			query := "+" + digits
			if _, seen := queryIdx[query]; !seen {
				queries = append(queries, query)
			}
			queryIdx[query] = append(queryIdx[query], i)
		}

		w.Header().Set("Content-Type", "application/json")
		if len(queries) > 0 {
			if !client.IsConnected() {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "WhatsApp client is not connected. Please wait for reconnection.",
				})
				return
			}
			matches, err := client.IsOnWhatsApp(context.Background(), queries)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": fmt.Sprintf("WhatsApp lookup failed: %v", err),
				})
				return
			}
			applyCheckMatches(results, queryIdx, matches)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
	}))
}
