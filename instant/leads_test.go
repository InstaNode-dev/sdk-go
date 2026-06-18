package instant

// leads_test.go — unit tests for CreateLead against an httptest server.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateLead_HappyPath(t *testing.T) {
	const wantID = "550e8400-e29b-41d4-a716-446655440099"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/leads" {
			t.Errorf("path = %q, want /api/v1/leads", r.URL.Path)
		}
		var body LeadParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Email != "cto@acme.com" {
			t.Errorf("email = %q, want cto@acme.com", body.Email)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true,"id":"`+wantID+`"}`)
	}))
	t.Cleanup(srv.Close)

	c := New(WithBaseURL(srv.URL))
	res, err := c.CreateLead(context.Background(), &LeadParams{
		Email:   "cto@acme.com",
		Company: "Acme Corp",
		UseCase: "Need SOC 2 + dedicated Postgres.",
	})
	if err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	if res.ID != wantID {
		t.Errorf("ID = %q, want %q", res.ID, wantID)
	}
}

func TestCreateLead_NilParams(t *testing.T) {
	c := New()
	_, err := c.CreateLead(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
	if !strings.Contains(err.Error(), "params must not be nil") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateLead_MissingEmail(t *testing.T) {
	c := New()
	_, err := c.CreateLead(context.Background(), &LeadParams{Company: "Acme"})
	if err == nil {
		t.Fatal("expected error for missing email")
	}
	if !strings.Contains(err.Error(), "Email is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateLead_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_email_format","message":"email must be a valid address"}`)
	}))
	t.Cleanup(srv.Close)

	c := New(WithBaseURL(srv.URL))
	_, err := c.CreateLead(context.Background(), &LeadParams{Email: "not-an-email"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "invalid_email_format" {
		t.Errorf("Code = %q, want invalid_email_format", apiErr.Code)
	}
}
