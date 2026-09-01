package signup

import (
	"context"
	"testing"
)

type recordingSender struct {
	email VerificationEmail
}

func (s *recordingSender) SendVerification(_ context.Context, email VerificationEmail) (string, error) {
	s.email = email
	return "msg-42", nil
}

func TestCreatorDeliveryDecision(t *testing.T) {
	tests := []struct {
		name               string
		verify             bool
		completeProcessing bool
		wantAllowed        bool
		wantReason         string
	}{
		{name: "verification pending", completeProcessing: true, wantReason: "email_verification_required"},
		{name: "processing pending", verify: true, wantReason: "processing_incomplete"},
		{name: "verified processed asset", verify: true, completeProcessing: true, wantAllowed: true, wantReason: "ready_for_creator_delivery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &recordingSender{}
			service := NewService(sender)
			result, err := service.Signup(context.Background(), "creator-7", "maker@example.com", "asset-9")
			if err != nil {
				t.Fatal(err)
			}
			if result.Asset.State != "ingested" || result.Job.State != "queued" || result.MessageID != "msg-42" {
				t.Fatalf("unexpected signup result: %+v", result)
			}
			if tt.verify {
				if _, err := service.Verify("creator-7", sender.email.PlaintextToken); err != nil {
					t.Fatal(err)
				}
			}
			if tt.completeProcessing {
				if err := service.CompleteProcessing("asset-9"); err != nil {
					t.Fatal(err)
				}
			}
			decision := service.Delivery("creator-7")
			if decision.Allowed != tt.wantAllowed || decision.Reason != tt.wantReason {
				t.Fatalf("got %+v, want allowed=%v reason=%q", decision, tt.wantAllowed, tt.wantReason)
			}
		})
	}
}

func TestVerificationTokenIsSingleWorkflowSecret(t *testing.T) {
	sender := &recordingSender{}
	service := NewService(sender)
	if _, err := service.Signup(context.Background(), "creator-7", "maker@example.com", "asset-9"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify("creator-7", "wrong-token"); err == nil {
		t.Fatal("expected a mismatched token to be rejected")
	}
	if service.Delivery("creator-7").Allowed {
		t.Fatal("delivery must remain blocked")
	}
}
