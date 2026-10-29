package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PerchOAuthHandler struct {
	perchOAuthService *service.PerchOAuthService
}

func NewPerchOAuthHandler(perchOAuthService *service.PerchOAuthService) *PerchOAuthHandler {
	return &PerchOAuthHandler{perchOAuthService: perchOAuthService}
}

type PerchGenerateAuthURLRequest struct {
	Provider    string `json:"provider"`
	AppURL      string `json:"app_url"`
	RedirectURI string `json:"redirect_uri"`
}

func (h *PerchOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req PerchGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = PerchGenerateAuthURLRequest{}
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "authentication required")
		return
	}
	result, err := h.perchOAuthService.GenerateAuthURL(c.Request.Context(), service.PerchAuthURLInput{
		AdminID:     subject.UserID,
		Provider:    req.Provider,
		AppURL:      req.AppURL,
		RedirectURI: req.RedirectURI,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type PerchExchangeCodeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	CallbackURL string `json:"callback_url"`
	Code        string `json:"code"`
}

func (h *PerchOAuthHandler) ExchangeCode(c *gin.Context) {
	var req PerchExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "authentication required")
		return
	}
	result, err := h.perchOAuthService.ExchangeCode(c.Request.Context(), service.PerchExchangeInput{
		AdminID:     subject.UserID,
		SessionID:   req.SessionID,
		CallbackURL: req.CallbackURL,
		Code:        req.Code,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("cache-control", "no-store")
	response.Success(c, result)
}
