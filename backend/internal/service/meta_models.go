package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/meta"
)

func DefaultMetaModels() []meta.Model {
	return meta.DefaultModels()
}

func (s *AccountTestService) FetchMetaAccountModels(ctx context.Context, account *Account) ([]meta.Model, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}
	req, err := s.buildMetaUpstreamModelsRequest(ctx, account)
	if err != nil {
		return nil, err
	}
	body, err := s.readUpstreamModelsResponse(req, account)
	if err != nil {
		return nil, err
	}
	models, err := meta.ParseModelList(body)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Upstream model list response has no usable muse models", err)
	}
	return MetaAccountTestModels(account, models), nil
}

func extractMetaUpstreamModelIDs(body []byte) ([]string, error) {
	models, err := meta.ParseModelList(body)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

// MetaAccountTestModels projects a Muse catalog onto the account's configured models.
func MetaAccountTestModels(account *Account, models []meta.Model) []meta.Model {
	return AccountTestModels(account, models, func(model meta.Model) string { return model.ID }, func(id string, target *meta.Model) meta.Model {
		if target == nil {
			return meta.NewModel(id)
		}
		entry := *target
		entry.ID = id
		return entry
	})
}

func (s *AccountTestService) buildMetaUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}
	if !account.IsMeta() {
		return nil, newUpstreamModelSyncUnsupportedError("Account is not a Meta account", nil)
	}

	apiKey := strings.TrimSpace(account.GetMetaAPIKey())
	if apiKey == "" && account.ID > 0 && s.openaiGatewayService != nil {
		minted, err := s.openaiGatewayService.MetaAPIKey(ctx, account)
		if err != nil {
			return nil, newUpstreamModelSyncConfigError("No Meta API key is available", err)
		}
		apiKey = strings.TrimSpace(minted)
	}
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No Meta API key is available", nil)
	}

	baseURL, err := s.validateUpstreamBaseURL(account.GetMetaBaseURL())
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Meta base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildV1ModelsURL(baseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Meta model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	meta.ApplyClientIdentity(req.Header, "")
	return req, nil
}
