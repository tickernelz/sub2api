package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// testMetaImageGeneration sends one Muse image generation through the gateway's own Meta images request.
func (s *AccountTestService) testMetaImageGeneration(c *gin.Context, account *Account, modelID, prompt string) error {
	if s.openaiGatewayService == nil {
		return s.sendErrorAndEnd(c, "Meta image test is unavailable")
	}
	ctx := c.Request.Context()
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = defaultOpenAIImageTestPrompt
	}
	token, _, err := s.openaiGatewayService.GetAccessToken(ctx, account)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("No API key available: %s", err.Error()))
	}
	payload, err := json.Marshal(map[string]any{"model": modelID, "prompt": prompt, "n": 1})
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to marshal Meta image request")
	}
	req, err := s.openaiGatewayService.buildOpenAIImagesRequest(ctx, c, account, payload, "application/json", token, openAIImagesGenerationsEndpoint)
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to create request: %s", err.Error()))
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "Calling Muse /images/generations..."})

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to read response: %s", err.Error()))
	}
	if resp.StatusCode != http.StatusOK {
		return s.sendErrorAndEnd(c, fmt.Sprintf("API returned %d: %s", resp.StatusCode, sanitizeUpstreamErrorMessage(string(body))))
	}

	var result struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Failed to parse response: %s", err.Error()))
	}
	images := 0
	for _, item := range result.Data {
		if item.RevisedPrompt != "" {
			s.sendEvent(c, TestEvent{Type: "content", Text: item.RevisedPrompt})
		}
		switch {
		case item.B64JSON != "":
			mimeType := base64ImageMimeType(item.B64JSON)
			s.sendEvent(c, TestEvent{Type: "image", ImageURL: "data:" + mimeType + ";base64," + item.B64JSON, MimeType: mimeType})
			images++
		case item.URL != "":
			s.sendEvent(c, TestEvent{Type: "image", ImageURL: item.URL})
			images++
		}
	}
	if images == 0 {
		return s.sendErrorAndEnd(c, "No images returned from API")
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func base64ImageMimeType(encoded string) string {
	head := encoded
	if len(head) > 24 {
		head = head[:24]
	}
	raw, _ := base64.StdEncoding.DecodeString(head)
	if mimeType := http.DetectContentType(raw); strings.HasPrefix(mimeType, "image/") {
		return mimeType
	}
	return "image/png"
}
