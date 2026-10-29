package perch

import (
	"context"
	"encoding/json"
	"strings"
)

const allowedModelsEntitlement = "limits.allowed_models"

// AllowedModels reads the account model allowlist; restricted is false when the plan allows every model.
func (a AccountInfo) AllowedModels() (models []string, restricted bool) {
	for _, ent := range a.Entitlements {
		if ent.Key != allowedModelsEntitlement {
			continue
		}
		var value struct {
			Models []string `json:"models"`
		}
		if json.Unmarshal(ent.Value, &value) != nil {
			return nil, false
		}
		out := make([]string, 0, len(value.Models))
		for _, model := range value.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			out = append(out, model)
		}
		if len(out) == 1 && out[0] == "*" {
			return nil, false
		}
		return dedupe(out), true
	}
	return nil, false
}

// Health reports whether the session can reach Perch and what the plan allows.
func (c *Client) Health(ctx context.Context) (Health, error) {
	account, err := c.Account(ctx)
	if err != nil {
		return Health{}, err
	}
	models, restricted := account.AllowedModels()
	return Health{
		OK:         true,
		Plan:       firstNonEmpty(account.PlanName, account.PlanCode),
		Email:      account.Email,
		Workspace:  account.WorkspaceID,
		Models:     models,
		Restricted: restricted,
	}, nil
}

// ModelIDs returns the model identifiers the account may request.
func (c *Client) ModelIDs(ctx context.Context) ([]string, error) {
	account, err := c.AccountCached(ctx)
	if err != nil {
		return nil, err
	}
	models, restricted := account.AllowedModels()
	if restricted && len(models) > 0 {
		return models, nil
	}
	return DefaultModelIDs(), nil
}

// DefaultModelIDs is the model set advertised when the plan allows every model.
func DefaultModelIDs() []string {
	return []string{
		"wandb-kimi-k2-6",
		"wandb-qwen3-6-35b-a3b",
	}
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
