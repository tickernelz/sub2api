package service

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// AccountTestModels lists the defaults the account serves, then its other configured custom and aliased models.
// named builds a configured model's entry from the default its mapping targets, or from nil.
func AccountTestModels[M any](account *Account, defaults []M, idOf func(M) string, named func(id string, target *M) M) []M {
	sources, configured := configuredTestModelSources(account)
	byID := make(map[string]int, len(defaults))
	models := make([]M, 0, len(defaults)+len(sources))
	seen := make(map[string]struct{}, len(defaults)+len(sources))
	for i, model := range defaults {
		id := idOf(model)
		byID[id] = i
		if _, dup := seen[id]; dup {
			continue
		}
		if configured && !account.IsModelSupported(id) {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, model)
	}
	for _, id := range sources {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		var target *M
		if i, ok := byID[account.GetMappedModel(id)]; ok {
			target = &defaults[i]
		}
		models = append(models, named(id, target))
	}
	return models
}

// configuredTestModelSources returns the stored mapping's exact sources and whether the mapping applies.
func configuredTestModelSources(account *Account) ([]string, bool) {
	if account == nil || (account.IsOpenAIPassthroughEnabled() && !account.IsOpenAIPassthroughModelFilterEnabled()) {
		return nil, false
	}
	var sources []string
	configured := false
	switch mapping := account.Credentials["model_mapping"].(type) {
	case map[string]any:
		for source, target := range mapping {
			if _, ok := target.(string); ok {
				configured = true
				sources = appendTestModelSource(sources, source)
			}
		}
	case map[string]string:
		for source := range mapping {
			configured = true
			sources = appendTestModelSource(sources, source)
		}
	}
	sort.Strings(sources)
	return sources, configured
}

func OpenAITestModelID(model openai.Model) string { return model.ID }

func NewOpenAITestModel(id string, _ *openai.Model) openai.Model {
	return openai.Model{ID: id, Object: "model", Type: "model", DisplayName: openaiCodexDisplayName(id)}
}

func appendTestModelSource(sources []string, source string) []string {
	if strings.TrimSpace(source) == "" || strings.Contains(source, "*") {
		return sources
	}
	return append(sources, source)
}
