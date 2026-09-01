package signup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"sync"
)

type EmailSender interface {
	SendVerification(context.Context, VerificationEmail) (string, error)
}

type VerificationEmail struct {
	CreatorID     string
	Email         string
	PlaintextToken string
}

type Creator struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

type Asset struct {
	ID        string `json:"id"`
	CreatorID string `json:"creator_id"`
	State     string `json:"state"`
}

type ProcessingJob struct {
	ID      string `json:"id"`
	AssetID string `json:"asset_id"`
	State   string `json:"state"`
}

type SignupResult struct {
	Creator  Creator       `json:"creator"`
	Asset    Asset         `json:"asset"`
	Job      ProcessingJob `json:"job"`
	MessageID string       `json:"message_id"`
}

type DeliveryDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type record struct {
	result    SignupResult
	tokenHash [32]byte
}

type Service struct {
	mu      sync.Mutex
	sender  EmailSender
	records map[string]*record
}

func NewService(sender EmailSender) *Service {
	return &Service{sender: sender, records: make(map[string]*record)}
}

func (s *Service) Signup(ctx context.Context, creatorID, email, assetID string) (SignupResult, error) {
	if creatorID == "" || email == "" || assetID == "" {
		return SignupResult{}, errors.New("creator_id, email, and asset_id are required")
	}

	token, err := randomToken()
	if err != nil {
		return SignupResult{}, fmt.Errorf("create verification token: %w", err)
	}
	result := SignupResult{
		Creator: Creator{ID: creatorID, Email: email},
		Asset: Asset{ID: assetID, CreatorID: creatorID, State: "ingested"},
		Job: ProcessingJob{ID: "job-" + assetID, AssetID: assetID, State: "queued"},
	}
	messageID, err := s.sender.SendVerification(ctx, VerificationEmail{CreatorID: creatorID, Email: email, PlaintextToken: token})
	if err != nil {
		return SignupResult{}, err
	}
	result.MessageID = messageID

	s.mu.Lock()
	s.records[creatorID] = &record{result: result, tokenHash: sha256.Sum256([]byte(token))}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) Verify(creatorID, token string) (Creator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[creatorID]
	if !ok || sha256.Sum256([]byte(token)) != rec.tokenHash {
		return Creator{}, errors.New("verification token is invalid")
	}
	rec.result.Creator.EmailVerified = true
	return rec.result.Creator, nil
}

func (s *Service) CompleteProcessing(assetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.result.Asset.ID == assetID {
			rec.result.Job.State = "completed"
			return nil
		}
	}
	return errors.New("asset not found")
}

func (s *Service) Delivery(creatorID string) DeliveryDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[creatorID]
	if !ok {
		return DeliveryDecision{Reason: "creator_not_found"}
	}
	if !rec.result.Creator.EmailVerified {
		return DeliveryDecision{Reason: "email_verification_required"}
	}
	if rec.result.Job.State != "completed" {
		return DeliveryDecision{Reason: "processing_incomplete"}
	}
	return DeliveryDecision{Allowed: true, Reason: "ready_for_creator_delivery"}
}

func VerificationHTML(baseURL string, email VerificationEmail) string {
	link := fmt.Sprintf("%s/verify?creator_id=%s&token=%s", baseURL, email.CreatorID, email.PlaintextToken)
	return "<p>Confirm your creator email to release processed media.</p><p><a href=\"" + html.EscapeString(link) + "\">Verify email</a></p>"
}

func randomToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
