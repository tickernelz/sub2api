package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayModelsAccountRepoStub struct {
	service.AccountRepository

	byGroup map[int64][]service.Account
}

type gatewayModelsSubscriptionRepoStub struct {
	service.UserSubscriptionRepository
	byUserGroup map[[2]int64]*service.UserSubscription
}

func (s *gatewayModelsSubscriptionRepoStub) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	if s == nil {
		return nil, service.ErrSubscriptionNotFound
	}
	if sub := s.byUserGroup[[2]int64{userID, groupID}]; sub != nil {
		clone := *sub
		return &clone, nil
	}
	return nil, service.ErrSubscriptionNotFound
}

func (s *gatewayModelsSubscriptionRepoStub) UpdateStatus(ctx context.Context, subscriptionID int64, status string) error {
	return nil
}

func (s *gatewayModelsSubscriptionRepoStub) ActivateWindows(ctx context.Context, id int64, start time.Time) error {
	return nil
}

func (s *gatewayModelsSubscriptionRepoStub) ResetDailyUsage(ctx context.Context, id int64, start time.Time) error {
	return nil
}

func (s *gatewayModelsSubscriptionRepoStub) ResetWeeklyUsage(ctx context.Context, id int64, start time.Time) error {
	return nil
}

func (s *gatewayModelsSubscriptionRepoStub) ResetMonthlyUsage(ctx context.Context, id int64, start time.Time) error {
	return nil
}

type gatewayModelsResponseForTest struct {
	Object string                    `json:"object"`
	Data   []gatewayModelItemForTest `json:"data"`
}

type gatewayModelItemForTest struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Created   int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	CreatedAt string `json:"created_at"`
}

func (s *gatewayModelsAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	accounts, ok := s.byGroup[groupID]
	if !ok {
		return nil, nil
	}
	out := make([]service.Account, len(accounts))
	copy(out, accounts)
	return out, nil
}

func newGatewayModelsHandlerForTest(repo service.AccountRepository) *GatewayHandler {
	return &GatewayHandler{
		gatewayService: service.NewGatewayService(
			repo,
			nil, // groupRepo
			nil, // usageLogRepo
			nil, // usageBillingRepo
			nil, // userRepo
			nil, // userSubRepo
			nil, // userGroupRateRepo
			nil, // cache
			nil, // cfg
			nil, // schedulerSnapshot
			nil, // concurrencyService
			nil, // billingService
			nil, // rateLimitService
			nil, // billingCacheService
			nil, // identityService
			nil, // httpUpstream
			nil, // deferredService
			nil, // claudeTokenProvider
			nil, // kiroTokenProvider
			nil, // kiroCooldownStore
			nil, // sessionLimitCache
			nil, // rpmCache
			nil, // digestStore
			nil, // settingService
			nil, // tlsFPProfileService
			nil, // channelService
			nil, // resolver
			nil, // balanceNotifyService
			nil, // userPlatformQuotaRepo
		),
	}
}

func TestGatewayAPIKeyAllowsFallbackGroupOnlyWhenAssigned(t *testing.T) {
	fallbackGroupID := int64(99)
	apiKey := &service.APIKey{
		GroupIDs: []int64{10, fallbackGroupID},
		Groups: []service.Group{
			{ID: 10, Status: service.StatusActive},
			{ID: fallbackGroupID, Status: service.StatusActive},
		},
	}

	require.True(t, apiKeyAllowsFallbackGroup(apiKey, fallbackGroupID))
	require.False(t, apiKeyAllowsFallbackGroup(apiKey, 100))
}

func TestGatewayAPIKeyAllowsFallbackGroupSupportsLegacyScalarOnlyKey(t *testing.T) {
	legacyGroupID := int64(10)
	apiKey := &service.APIKey{GroupID: &legacyGroupID}

	require.True(t, apiKeyAllowsFallbackGroup(apiKey, legacyGroupID))
	require.False(t, apiKeyAllowsFallbackGroup(apiKey, 11))
}

func TestGatewayModelListGroupsDoesNotUseStaleDefaultOutsideAssignments(t *testing.T) {
	staleDefaultID := int64(99)
	apiKey := &service.APIKey{
		GroupID:  &staleDefaultID,
		Group:    &service.Group{ID: staleDefaultID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
		GroupIDs: []int64{10},
		Groups: []service.Group{
			{ID: 10, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		},
	}

	groups := apiKeyModelListGroups(apiKey, "")

	require.Len(t, groups, 1)
	require.Equal(t, int64(10), groups[0].ID)
}

func TestGatewayModels_GeminiGroupFallsBackToGeminiModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(20)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformGemini},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "list", got.Object)
	require.Contains(t, modelIDsForTest(got.Data), "gemini-2.5-flash")
	require.NotContains(t, modelIDsForTest(got.Data), "claude-sonnet-4-6")
}

func TestGatewayModels_GeminiGroupFiltersMappedModelsByPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(21)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
							},
						},
					},
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gemini-2.5-flash": "gemini-2.5-flash",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformGemini},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gemini-2.5-flash"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_MultiGroupReturnsUnion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroupID := int64(31)
	geminiGroupID := int64(32)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				anthropicGroupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-sonnet-4-6": "claude-sonnet-4-6",
								"shared-public":     "claude-upstream",
							},
						},
					},
				},
				geminiGroupID: {
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gemini-2.5-flash": "gemini-2.5-flash",
								"shared-public":    "gemini-upstream",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &anthropicGroupID,
		Group:   &service.Group{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: geminiGroupID, Platform: service.PlatformGemini, Status: service.StatusActive},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-sonnet-4-6", "gemini-2.5-flash", "shared-public"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_MultiOpenAIGroupsUseOpenAIModelShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	defaultGroupID := int64(33)
	secondGroupID := int64(34)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				defaultGroupID: {{ID: 1, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
				secondGroupID:  {{ID: 2, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}}}},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &defaultGroupID,
		Group:   &service.Group{ID: defaultGroupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: defaultGroupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
			{ID: secondGroupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.4", "gpt-5.5"}, modelIDsForTest(got.Data))
	require.Equal(t, "model", got.Data[0].Object)
	require.NotZero(t, got.Data[0].Created)
	require.Empty(t, got.Data[0].CreatedAt)
}

func TestGatewaySelectAPIKeyGroupForRequest_SelectsAssignedGroupByModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroupID := int64(41)
	geminiGroupID := int64(42)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				anthropicGroupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"},
						},
					},
				},
				geminiGroupID: {
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"},
						},
					},
				},
			},
		},
	)

	body := `{"model":"gemini-2.5-flash","messages":[]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &anthropicGroupID,
		Group:   &service.Group{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: geminiGroupID, Platform: service.PlatformGemini, Status: service.StatusActive},
		},
	})

	require.True(t, h.SelectAPIKeyGroupForRequest(c, GatewayEndpointMessages))

	selected, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.NotNil(t, selected.GroupID)
	require.Equal(t, geminiGroupID, *selected.GroupID)
	require.NotNil(t, selected.Group)
	require.Equal(t, service.PlatformGemini, selected.Group.Platform)
	restored, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(restored))
}

func TestGatewaySelectAPIKeyGroupForRequest_SelectsGeminiGroupByPathModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroupID := int64(43)
	geminiGroupID := int64(44)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				anthropicGroupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"},
						},
					},
				},
				geminiGroupID: {
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader(`{"contents":[]}`))
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-flash:generateContent"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &anthropicGroupID,
		Group:   &service.Group{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: geminiGroupID, Platform: service.PlatformGemini, Status: service.StatusActive},
		},
	})

	require.True(t, h.SelectAPIKeyGroupForRequest(c, GatewayEndpointGeminiV1Beta))

	selected, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.NotNil(t, selected.GroupID)
	require.Equal(t, geminiGroupID, *selected.GroupID)
	require.NotNil(t, selected.Group)
	require.Equal(t, service.PlatformGemini, selected.Group.Platform)
}

func TestGatewaySelectAPIKeyGroupForRequest_RejectsUnavailableModelInsteadOfFallingBack(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroupID := int64(45)
	geminiGroupID := int64(46)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				anthropicGroupID: {{ID: 1, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"}}}},
				geminiGroupID:    {{ID: 2, Platform: service.PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"}}}},
			},
		},
	)

	body := `{"model":"not-assigned-model","messages":[]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &anthropicGroupID,
		Group:   &service.Group{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: geminiGroupID, Platform: service.PlatformGemini, Status: service.StatusActive},
		},
	})

	require.False(t, h.SelectAPIKeyGroupForRequest(c, GatewayEndpointMessages))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGatewaySelectAPIKeyGroupForRequest_SkipsDefaultSubscriptionGroupWithoutActiveSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)

	defaultGroupID := int64(47)
	standardGroupID := int64(48)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				defaultGroupID:  {{ID: 1, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"}}}},
				standardGroupID: {{ID: 2, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"}}}},
			},
		},
	)
	h.subscriptionService = service.NewSubscriptionService(nil, &gatewayModelsSubscriptionRepoStub{}, nil, nil, &config.Config{})

	body := `{"model":"claude-sonnet-4-6","messages":[]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &defaultGroupID,
		Group: &service.Group{
			ID:               defaultGroupID,
			Platform:         service.PlatformAnthropic,
			Status:           service.StatusActive,
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
		User: &service.User{ID: 77, Status: service.StatusActive, Balance: 0},
		Groups: []service.Group{
			{ID: defaultGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription},
			{ID: standardGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		},
	})

	require.True(t, h.SelectAPIKeyGroupForRequest(c, GatewayEndpointMessages))
	selected, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.NotNil(t, selected.GroupID)
	require.Equal(t, standardGroupID, *selected.GroupID)
}

func TestGatewayModels_MultiGroupOmitsUnusableSubscriptionGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	subscriptionGroupID := int64(49)
	standardGroupID := int64(50)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				subscriptionGroupID: {{ID: 1, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"subscription-only": "subscription-upstream"}}}},
				standardGroupID:     {{ID: 2, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"standard-model": "standard-upstream"}}}},
			},
		},
	)
	h.subscriptionService = service.NewSubscriptionService(nil, &gatewayModelsSubscriptionRepoStub{}, nil, nil, &config.Config{})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &subscriptionGroupID,
		Group: &service.Group{
			ID:               subscriptionGroupID,
			Platform:         service.PlatformAnthropic,
			Status:           service.StatusActive,
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
		User: &service.User{ID: 78, Status: service.StatusActive, Balance: 0},
		Groups: []service.Group{
			{ID: subscriptionGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription},
			{ID: standardGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"standard-model"}, modelIDsForTest(got.Data))
}

func TestGatewaySelectAPIKeyGroupForRequest_PrefersDefaultGroupForSharedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	anthropicGroupID := int64(51)
	geminiGroupID := int64(52)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				anthropicGroupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"shared-public": "claude-upstream"},
						},
					},
				},
				geminiGroupID: {
					{
						ID:       2,
						Platform: service.PlatformGemini,
						Credentials: map[string]any{
							"model_mapping": map[string]any{"shared-public": "gemini-upstream"},
						},
					},
				},
			},
		},
	)

	body := `{"model":"shared-public","messages":[]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &anthropicGroupID,
		Group:   &service.Group{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
		Groups: []service.Group{
			{ID: anthropicGroupID, Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: geminiGroupID, Platform: service.PlatformGemini, Status: service.StatusActive},
		},
	})

	require.True(t, h.SelectAPIKeyGroupForRequest(c, GatewayEndpointMessages))

	selected, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.NotNil(t, selected.GroupID)
	require.Equal(t, anthropicGroupID, *selected.GroupID)
	require.NotNil(t, selected.Group)
	require.Equal(t, service.PlatformAnthropic, selected.Group.Platform)
}

func TestGatewayModels_CustomModelsListDisabledKeepsOriginalModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(22)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.5": "gpt-5.5",
								"gpt-5.4": "gpt-5.4",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: false,
				Models:  []string{"gpt-5.5"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.4", "gpt-5.5"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListFiltersAndOrdersMappedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(23)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.4":         "gpt-5.4",
								"gpt-5.5":         "gpt-5.5",
								"legacy-gpt-2024": "legacy-gpt-2024",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "missing-model", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListKeepsConcreteModelAllowedByWildcardMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(26)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformAnthropic,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"claude-*": "claude-sonnet-4-6",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformAnthropic,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"claude-sonnet-4-6"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"claude-sonnet-4-6"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListCanReturnEmptyWhenSelectionsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(24)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{
						ID:       1,
						Platform: service.PlatformOpenAI,
						Credentials: map[string]any{
							"model_mapping": map[string]any{
								"gpt-5.4": "gpt-5.4",
							},
						},
					},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, modelIDsForTest(got.Data))
}

func TestGatewayModels_CustomModelsListFiltersDefaultFallbackModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(25)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "legacy-gpt-2024", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
}

func TestGatewayModels_OpenAICustomModelsListKeepsOpenAIResponseShapeForDefaultFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(27)
	h := newGatewayModelsHandlerForTest(
		&gatewayModelsAccountRepoStub{
			byGroup: map[int64][]service.Account{
				groupID: {
					{ID: 1, Platform: service.PlatformOpenAI},
				},
			},
		},
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.5", "gpt-5.4"},
			},
		},
	})

	h.Models(c)

	require.Equal(t, http.StatusOK, rec.Code)

	var got gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, modelIDsForTest(got.Data))
	require.Equal(t, "model", got.Data[0].Object)
	require.NotZero(t, got.Data[0].Created)
	require.Equal(t, "openai", got.Data[0].OwnedBy)
	require.Empty(t, got.Data[0].CreatedAt)
}

func modelIDsForTest(models []gatewayModelItemForTest) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}
