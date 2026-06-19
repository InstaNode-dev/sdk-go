package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/InstaNode-dev/sdk-go/instant"
)

func TestRun_HappyPath(t *testing.T) {
	const wantID = "550e8400-e29b-41d4-a716-446655440099"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email   string `json:"email"`
			Company string `json:"company"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Email != "cto@acme.com" {
			http.Error(w, "bad email", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true,"id":"`+wantID+`"}`)
	}))
	t.Cleanup(srv.Close)

	c := instant.New(instant.WithBaseURL(srv.URL))
	var out strings.Builder
	err := run(context.Background(), c, &instant.LeadParams{
		Email:   "cto@acme.com",
		Company: "Acme Corp",
		UseCase: "Need SOC 2 + 10 TB Postgres.",
	}, &out)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, wantID) {
		t.Errorf("output missing lead ID %q:\n%s", wantID, s)
	}
	if !strings.Contains(s, "cto@acme.com") {
		t.Errorf("output missing email:\n%s", s)
	}
}

func TestRun_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal","message":"db down"}`)
	}))
	t.Cleanup(srv.Close)

	c := instant.New(instant.WithBaseURL(srv.URL))
	var out strings.Builder
	err := run(context.Background(), c, &instant.LeadParams{Email: "a@b.com"}, &out)
	if err == nil {
		t.Fatal("expected error from server failure")
	}
}
