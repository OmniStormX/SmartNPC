package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_PostJSON_Success(t *testing.T) {
	type req struct {
		Text string `json:"text"`
	}
	type resp struct {
		OK bool `json:"ok"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var got req
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Text != "hi" {
			t.Errorf("text = %q, want hi", got.Text)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	var out resp
	if err := c.PostJSON(context.Background(), "/anything", req{Text: "hi"}, &out); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if !out.OK {
		t.Errorf("OK = false")
	}
}

func TestClient_PostJSON_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.PostJSON(context.Background(), "/x", struct{}{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewClient_DefaultURL(t *testing.T) {
	c := NewClient("")
	if c.baseURL != DefaultModURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultModURL)
	}
}
