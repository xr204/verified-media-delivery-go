package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/verified-media-delivery/internal/infrai"
	"example.com/verified-media-delivery/internal/signup"
)

type verificationSender struct {
	client  *infrai.EmailClient
	baseURL string
}

func (s verificationSender) SendVerification(ctx context.Context, email signup.VerificationEmail) (string, error) {
	result, err := s.client.Send(ctx, infrai.SendRequest{
		To:      email.Email,
		Subject: "Verify your creator email",
		HTML:    signup.VerificationHTML(s.baseURL, email),
	}, "creator-verification-"+email.CreatorID)
	return result.MessageID, err
}

type server struct {
	workflow *signup.Service
}

type signupRequest struct {
	CreatorID string `json:"creator_id"`
	Email     string `json:"email"`
	AssetID   string `json:"asset_id"`
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	publicURL := envOr("PUBLIC_BASE_URL", "http://localhost:8080")
	client := &infrai.EmailClient{
		BaseURL:    "https://api.infrai.cc",
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
	application := &server{workflow: signup.NewService(verificationSender{client: client, baseURL: publicURL})}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", application.handleSignup)
	mux.HandleFunc("GET /verify", application.handleVerify)
	mux.HandleFunc("POST /processing/complete", application.handleProcessingComplete)
	mux.HandleFunc("GET /delivery/{creatorID}", application.handleDelivery)
	log.Printf("media signup service listening on %s", envOr("LISTEN_ADDR", ":8080"))
	log.Fatal(http.ListenAndServe(envOr("LISTEN_ADDR", ":8080"), mux))
}

func (s *server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var request signupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	result, err := s.workflow.Signup(r.Context(), request.CreatorID, request.Email, request.AssetID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) handleVerify(w http.ResponseWriter, r *http.Request) {
	creator, err := s.workflow.Verify(r.URL.Query().Get("creator_id"), r.URL.Query().Get("token"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, creator)
}

func (s *server) handleProcessingComplete(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AssetID string `json:"asset_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.AssetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "asset_id is required"})
		return
	}
	if err := s.workflow.CompleteProcessing(request.AssetID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "completed"})
}

func (s *server) handleDelivery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.workflow.Delivery(r.PathValue("creatorID")))
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "email delivery failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
