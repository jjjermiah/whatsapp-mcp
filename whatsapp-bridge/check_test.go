package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestDefinitelyNotOnWhatsApp(t *testing.T) {
	lid := types.JID{User: "123456789012345", Server: types.HiddenUserServer}
	cases := []struct {
		name    string
		matches []types.IsOnWhatsAppResponse
		want    bool
	}{
		{"no answer proves nothing", nil, false},
		{"registered", []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid}}, false},
		{"answered, not registered", []types.IsOnWhatsAppResponse{{IsIn: false, Query: "+16476698464"}}, true},
		{"any registered wins", []types.IsOnWhatsAppResponse{{IsIn: false}, {IsIn: true, JID: lid}}, false},
	}
	for _, tc := range cases {
		if got := definitelyNotOnWhatsApp(tc.matches); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNotOnWhatsAppError(t *testing.T) {
	err := notOnWhatsApp("16476698464")
	if err.Status != http.StatusUnprocessableEntity || err.Code != "not_on_whatsapp" {
		t.Fatalf("unexpected error: %+v", err)
	}
	if !strings.Contains(err.Error(), "16476698464") {
		t.Fatalf("message should name the number: %q", err.Error())
	}
}

func TestApplyCheckMatches(t *testing.T) {
	lid := types.JID{User: "123456789012345", Server: types.HiddenUserServer}
	results := []checkResult{{Input: "+1 647-669-8464"}, {Input: "16476698464"}, {Input: "19052702348"}, {Input: "bad", Error: "x"}}
	queryIdx := map[string][]int{
		"+16476698464": {0, 1}, // duplicate inputs share one query
		"+19052702348": {2},
	}
	applyCheckMatches(results, queryIdx, []types.IsOnWhatsAppResponse{
		{Query: "+16476698464", IsIn: true, JID: lid},
		{Query: "+19052702348", IsIn: false},
		{Query: "+10000000000", IsIn: true, JID: lid}, // unrequested: ignored
	})

	for _, i := range []int{0, 1} {
		if !results[i].Registered || results[i].JID != lid.String() {
			t.Errorf("results[%d] = %+v, want registered with jid %s", i, results[i], lid)
		}
	}
	if results[2].Registered || results[2].JID != "" {
		t.Errorf("results[2] = %+v, want unregistered", results[2])
	}
	if results[3].Registered || results[3].Error != "x" {
		t.Errorf("results[3] = %+v, want untouched", results[3])
	}
}

func checkRequestTo(t *testing.T, handler http.Handler, token, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:8080/api/check", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestCheckHandler(t *testing.T) {
	const token = "supersecrettoken1234567890abcdef"
	handler := newRESTMux(newTestClient(&mockLIDStore{}), newTestMessageStore(t), 8080, token, nil)

	t.Run("method not allowed", func(t *testing.T) {
		if resp := checkRequestTo(t, handler, token, http.MethodGet, ""); resp.Code != http.StatusMethodNotAllowed {
			t.Errorf("got %d", resp.Code)
		}
	})
	t.Run("empty list", func(t *testing.T) {
		if resp := checkRequestTo(t, handler, token, http.MethodPost, `{"phone_numbers":[]}`); resp.Code != http.StatusBadRequest {
			t.Errorf("got %d", resp.Code)
		}
	})
	t.Run("too many numbers is 400", func(t *testing.T) {
		numbers := make([]string, maxCheckNumbers+1)
		for i := range numbers {
			numbers[i] = "\"16476698464\""
		}
		body := `{"phone_numbers":[` + strings.Join(numbers, ",") + `]}`
		if resp := checkRequestTo(t, handler, token, http.MethodPost, body); resp.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %d numbers, got %d", len(numbers), resp.Code)
		}
		at := strings.Join(numbers[:maxCheckNumbers], ",")
		if resp := checkRequestTo(t, handler, token, http.MethodPost, `{"phone_numbers":[`+at+`]}`); resp.Code != http.StatusServiceUnavailable {
			t.Errorf("expected the cap to allow %d numbers (503 while disconnected), got %d", maxCheckNumbers, resp.Code)
		}
	})
	t.Run("invalid inputs are reported per entry without a lookup", func(t *testing.T) {
		resp := checkRequestTo(t, handler, token, http.MethodPost,
			`{"phone_numbers":["6474571760","9052702348/ 6474571760","1647@s.whatsapp.net"]}`)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
		}
		var out struct {
			Results []checkResult `json:"results"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Results) != 3 {
			t.Fatalf("got %d results", len(out.Results))
		}
		for _, r := range out.Results {
			if r.Registered || r.Error == "" {
				t.Errorf("result %+v should be an error entry", r)
			}
		}
	})
	t.Run("valid number while disconnected is 503", func(t *testing.T) {
		resp := checkRequestTo(t, handler, token, http.MethodPost, `{"phone_numbers":["+1 647-669-8464"]}`)
		if resp.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d body=%s", resp.Code, resp.Body.String())
		}
	})
}
