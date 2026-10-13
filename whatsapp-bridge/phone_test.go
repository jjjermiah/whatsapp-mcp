package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeRecipient(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string // substring of the error message; empty means success
	}{
		{"plain digits", "16476698464", "16476698464", ""},
		{"plus prefix", "+16476698464", "16476698464", ""},
		{"csv apostrophe", "'+1 647-669-8464", "16476698464", ""},
		{"curly apostrophe", "\u2019+1 647-669-8464", "16476698464", ""},
		{"parens and dots", "+1 (647) 669.8464", "16476698464", ""},
		{"nbsp", "+1\u00a0647\u00a0669\u00a08464", "16476698464", ""},
		{"surrounding whitespace", "  16476698464\n", "16476698464", ""},
		{"11 digits", "12345678901", "12345678901", ""},
		{"15 digits max", "123456789012345", "123456789012345", ""},

		{"phone jid untouched", "16476698464@s.whatsapp.net", "16476698464@s.whatsapp.net", ""},
		{"group jid untouched", "120363012345678901@g.us", "120363012345678901@g.us", ""},
		{"lid jid untouched", "184125298348272@lid", "184125298348272@lid", ""},
		{"jid with spaces around", " 16476698464@s.whatsapp.net ", "16476698464@s.whatsapp.net", ""},

		{"slash separated", "9052702348/ 6474571760", "", "more than one number"},
		{"comma separated", "16476698464, 19052702348", "", "more than one number"},
		{"semicolon separated", "16476698464;19052702348", "", "more than one number"},
		{"newline separated", "16476698464\n19052702348", "", "more than one number"},
		{"two numbers joined by spaces", "16476698464 19052702348", "", "more than the 15"},

		{"national number no country code", "6474571760", "", "country code"},
		{"formatted national number", "(647) 457-1760", "", "country code"},
		{"short", "12345", "", "country code"},
		{"too long", "1234567890123456", "", "more than the 15"},

		{"empty", "", "", "empty"},
		{"whitespace only", "   ", "", "empty"},
		{"no digits", "+", "", "no digits"},
		{"letters", "1647abc8464", "", "invalid character"},
		{"extension", "16476698464 ext 5", "", "invalid character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeRecipient(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeRecipient(%q) = %q, want error containing %q", tc.in, got, tc.wantErr)
				}
				re, ok := err.(*recipientError)
				if !ok || re.Status != http.StatusBadRequest || re.Code != errCodeInvalidRecipient {
					t.Fatalf("normalizeRecipient(%q) error = %#v, want 400 %s", tc.in, err, errCodeInvalidRecipient)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("normalizeRecipient(%q) error = %q, want it to contain %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeRecipient(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeRecipient(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveRecipientJID_RejectsBadNumber(t *testing.T) {
	client := newTestClient(&mockLIDStore{})
	for _, in := range []string{"6474571760", "9052702348/ 6474571760", "not a number"} {
		if _, err := resolveRecipientJID(client, in); err == nil {
			t.Errorf("resolveRecipientJID(%q) succeeded, want error", in)
		} else if _, ok := err.(*recipientError); !ok {
			t.Errorf("resolveRecipientJID(%q) error type = %T, want *recipientError", in, err)
		}
	}
}

// TestRecipientHandlers_RejectBadNumbers_Return400 checks that every endpoint
// taking a recipient answers a malformed number with a JSON 400 carrying the
// invalid_recipient code, before touching the WhatsApp client.
func TestRecipientHandlers_RejectBadNumbers_Return400(t *testing.T) {
	const token = "supersecrettoken1234567890abcdef"
	handler := newRESTMux(newTestClient(&mockLIDStore{}), newTestMessageStore(t), 8080, token, nil)

	cases := []struct {
		name string
		path string
		body string
	}{
		{"send multiple", "/api/send", `{"recipient":"9052702348/ 6474571760","message":"hi"}`},
		{"send no country code", "/api/send", `{"recipient":"6474571760","message":"hi"}`},
		{"react", "/api/react", `{"recipient":"6474571760","message_id":"3AABCDEF01234567","emoji":"👍"}`},
		{"mark-read chat", "/api/mark-read", `{"message_ids":["3AABCDEF01234567"],"chat_jid":"6474571760"}`},
		{"mark-read sender", "/api/mark-read", `{"message_ids":["3AABCDEF01234567"],"chat_jid":"120363012345678901@g.us","sender_jid":"6474571760"}`},
		{"typing", "/api/typing", `{"recipient":"6474571760","is_typing":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", resp.Code, resp.Body.String())
			}
			var out struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
				t.Fatalf("response is not JSON: %v body=%s", err, resp.Body.String())
			}
			if out.Success || out.Error != errCodeInvalidRecipient || out.Message == "" {
				t.Fatalf("unexpected body: %+v", out)
			}
		})
	}
}
