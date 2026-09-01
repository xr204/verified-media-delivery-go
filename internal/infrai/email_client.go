package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const emailSendPath = "/v1/email/send"

type EmailClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Sleep      func(context.Context, time.Duration) error
}

type SendRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type SendResult struct {
	MessageID string `json:"message_id"`
}

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorBody      `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (c *EmailClient) Send(ctx context.Context, request SendRequest, idempotencyKey string) (SendResult, error) {
	if c.APIKey == "" {
		return SendResult{}, errors.New("INFRAI_API_KEY is required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return SendResult{}, fmt.Errorf("encode email: %w", err)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+emailSendPath, bytes.NewReader(payload))
		if err != nil {
			return SendResult{}, fmt.Errorf("create email request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		response, err := client.Do(req)
		if err != nil {
			return SendResult{}, fmt.Errorf("send email: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return SendResult{}, fmt.Errorf("read email response: %w", readErr)
		}

		var reply envelope
		if err := json.Unmarshal(body, &reply); err != nil {
			return SendResult{}, fmt.Errorf("decode email response (status %d): %w", response.StatusCode, err)
		}
		if !reply.OK {
			if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := sleep(ctx, retryDelay(response.Header.Get("Retry-After"), attempt)); err != nil {
					return SendResult{}, err
				}
				continue
			}
			apiErr := &APIError{HTTPStatus: response.StatusCode, Message: "email request rejected"}
			if reply.Error != nil {
				apiErr.Code = reply.Error.Code
				apiErr.Message = reply.Error.Message
				if apiErr.Message == "" {
					apiErr.Message = reply.Error.Hint
				}
			}
			return SendResult{}, apiErr
		}
		if response.StatusCode >= http.StatusInternalServerError {
			return SendResult{}, fmt.Errorf("email transport status %d", response.StatusCode)
		}

		var result SendResult
		if err := json.Unmarshal(reply.Data, &result); err != nil {
			return SendResult{}, fmt.Errorf("decode email data: %w", err)
		}
		return result, nil
	}
	return SendResult{}, errors.New("email retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
