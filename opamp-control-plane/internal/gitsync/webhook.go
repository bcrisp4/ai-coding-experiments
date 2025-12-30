package gitsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// WebhookHandler handles Git webhook events.
type WebhookHandler struct {
	syncer *Syncer
	secret string
	logger *slog.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(syncer *Syncer, secret string, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		syncer: syncer,
		secret: secret,
		logger: logger,
	}
}

// GitHubPushPayload represents a GitHub push event payload.
type GitHubPushPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
}

// ServeHTTP handles incoming webhook requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read webhook body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature if secret is configured
	if h.secret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if !h.verifySignature(body, signature) {
			h.logger.Warn("invalid webhook signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		// Try GitLab format
		eventType = r.Header.Get("X-Gitlab-Event")
	}

	h.logger.Info("received webhook", "event", eventType)

	switch eventType {
	case "push", "Push Hook":
		if err := h.handlePush(r.Context(), body); err != nil {
			h.logger.Error("failed to handle push event", "error", err)
			http.Error(w, "failed to process push", http.StatusInternalServerError)
			return
		}
	case "ping":
		h.logger.Info("received ping event")
	default:
		h.logger.Debug("ignoring event", "type", eventType)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

// handlePush processes a push event.
func (h *WebhookHandler) handlePush(ctx context.Context, body []byte) error {
	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("failed to parse push payload: %w", err)
	}

	h.logger.Info("processing push event",
		"repo", payload.Repository.FullName,
		"ref", payload.Ref,
		"commit", payload.After,
		"pusher", payload.Pusher.Name,
	)

	// Trigger sync
	commit, err := h.syncer.Sync(ctx)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	h.logger.Info("sync completed", "commit", commit)
	return nil
}

// verifySignature verifies the GitHub webhook signature.
func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expected := hmac.New(sha256.New, []byte(h.secret))
	expected.Write(body)
	expectedSig := "sha256=" + hex.EncodeToString(expected.Sum(nil))

	return hmac.Equal([]byte(expectedSig), []byte(signature))
}
