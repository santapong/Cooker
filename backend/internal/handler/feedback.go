package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/santapong/cooker/internal/auth"
	"github.com/santapong/cooker/internal/service"
)

// feedbackRequest is the JSON body of POST /feedback. Identity and
// User-Agent are derived server-side (auth context / request header),
// never from client JSON.
type feedbackRequest struct {
	Category string `json:"category"`
	Message  string `json:"message"`
	PageURL  string `json:"pageUrl"`
}

// SubmitFeedback files the caller's feedback as a GitHub issue (pure
// relay — nothing is persisted). 503 when the feature is off; upstream
// failures return a generic 502 (GitHub's response text is logged
// server-side, never relayed).
func (h *Handler) SubmitFeedback(c *gin.Context) {
	if h.Feedback == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "feedback not configured"})
		return
	}
	var req feedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	switch req.Category {
	case "bug", "idea", "other":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "category must be one of: bug, idea, other"})
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" || utf8.RuneCountInString(msg) > 4000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message must be 1-4000 characters"})
		return
	}
	if utf8.RuneCountInString(req.PageURL) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pageUrl must be at most 500 characters"})
		return
	}

	in := service.FeedbackInput{
		Category:  req.Category,
		Message:   msg,
		PageURL:   req.PageURL,
		UserAgent: c.Request.UserAgent(),
	}
	if u := auth.GetUser(c); u != nil {
		in.UserID = u.Subject
		in.UserEmail = u.Email
	}

	issueURL, issueNumber, err := h.Feedback.Submit(c.Request.Context(), in)
	if err != nil {
		// Upstream error text can carry GitHub's response body —
		// log it, never relay it.
		slog.Error("feedback: submit failed", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to file feedback"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"issueUrl": issueURL, "issueNumber": issueNumber})
}
