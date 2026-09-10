package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"time"
)

type EmailSender interface {
	SendVerification(context.Context, string, string, string) error
	SendPasswordReset(context.Context, string, string, string) error
	SendActivation(context.Context, string, string, string) error
}

type resendSender struct {
	apiKey string
	from   string
	client *http.Client
}

func newResendSender(cfg Config) EmailSender {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &resendSender{
		apiKey: cfg.ResendAPIKey,
		from:   cfg.ResendFromEmail,
		client: &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}
}

func (sender *resendSender) SendVerification(ctx context.Context, targetEmail, userName, verificationURL string) error {
	return sender.send(
		ctx,
		targetEmail,
		"Verify your Habit Loop account",
		fmt.Sprintf(
			"<p>Hello %s,</p><p><a href=\"%s\">Continue verifying your account</a>.</p><p>This link expires in 24 hours.</p>",
			html.EscapeString(userName),
			html.EscapeString(verificationURL),
		),
	)
}

func (sender *resendSender) SendPasswordReset(ctx context.Context, targetEmail, userName, resetURL string) error {
	return sender.send(
		ctx,
		targetEmail,
		"Reset your Habit Loop password",
		fmt.Sprintf(
			"<p>Hello %s,</p><p><a href=\"%s\">Reset your password</a>.</p><p>This link expires in one hour.</p>",
			html.EscapeString(userName),
			html.EscapeString(resetURL),
		),
	)
}

func (sender *resendSender) SendActivation(ctx context.Context, targetEmail, userName, verificationURL string) error {
	return sender.send(
		ctx,
		targetEmail,
		"Activate your Habit Loop account",
		fmt.Sprintf(
			"<p>Hello %s,</p><p>An administrator created a Habit Loop account for you.</p><p><a href=\"%s\">Activate your account</a>.</p>",
			html.EscapeString(userName),
			html.EscapeString(verificationURL),
		),
	)
}

func (sender *resendSender) send(ctx context.Context, targetEmail, subject, body string) error {
	if sender.apiKey == "" || sender.from == "" {
		return fmt.Errorf("email service is not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"from":    sender.from,
		"to":      []string{targetEmail},
		"subject": subject,
		"html":    body,
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+sender.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := sender.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("email provider returned status %d", response.StatusCode)
	}
	return nil
}
