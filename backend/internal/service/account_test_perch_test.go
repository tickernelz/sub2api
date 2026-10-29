package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
)

const perchUnrestrictedEntitlements = `[{"key":"limits.allowed_models","value_json":{"models":["*"]}}]`

type perchModelFakeServer struct {
	*httptest.Server
	mu          sync.Mutex
	accountHits int
	modelCalls  int
	lastModel   string
	lastBody    string
}

func newPerchModelFakeServer(t *testing.T, entitlements string, answer string) *perchModelFakeServer {
	t.Helper()
	fake := &perchModelFakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perchai/account":
			fake.mu.Lock()
			fake.accountHits++
			fake.mu.Unlock()
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"profile":{"id":"user-1","email":"re@example.test","name":"RE"},"session":{"userId":"user-1","orgId":"org-1","workspaceId":"ws-1","planCode":"pro","planName":"Pro","entitlements":%s}}`, entitlements)
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			body, _ := io.ReadAll(r.Body)
			fake.mu.Lock()
			fake.modelCalls++
			fake.lastBody = string(body)
			fake.lastModel = modelIdentityFromCallBody(body)
			fake.mu.Unlock()
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: {\"type\":\"answer_delta\",\"text\":%s}\n\n", quoteJSON(answer))
			fmt.Fprint(w, "data: {\"type\":\"done\",\"ok\":true,\"text\":"+quoteJSON(answer)+",\"model\":\"wandb-kimi-k2-6\",\"finishReason\":\"stop\",\"usage\":{\"inputTokens\":9,\"outputTokens\":2,\"totalTokens\":11}}\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func modelIdentityFromCallBody(body []byte) string {
	var call struct {
		ManualModelOptionID string `json:"manualModelOptionId"`
		RoostModelChoice    string `json:"roostModelChoice"`
	}
	if json.Unmarshal(body, &call) != nil {
		return ""
	}
	if call.ManualModelOptionID != "" {
		return call.ManualModelOptionID
	}
	return call.RoostModelChoice
}

func (f *perchModelFakeServer) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accountHits, f.modelCalls
}

func (f *perchModelFakeServer) last() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastModel, f.lastBody
}

func newPerchModelsService(t *testing.T, fake *perchModelFakeServer) *AccountTestService {
	t.Helper()
	previous := perchClientFactory
	perchClientFactory = func(creds perch.Credentials, store perch.CredentialStore) *perch.Client {
		return perch.NewClient(perch.NewSession(creds, perch.WithHTTPClient(fake.Client())))
	}
	t.Cleanup(func() { perchClientFactory = previous })
	return &AccountTestService{openaiGatewayService: &OpenAIGatewayService{}}
}

func newPerchModelsAccount(fake *perchModelFakeServer) *Account {
	return &Account{
		ID:       5150,
		Platform: PlatformPerch,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			perchAppURLKey:       fake.URL,
			perchAccessTokenKey:  "access-token",
			perchRefreshTokenKey: "refresh-token",
			perchExpiresAtKey:    time.Now().Add(time.Hour).Unix(),
		},
	}
}

func perchModelIDs(models []claude.Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}

func TestPerchAvailableModelsUsesEntitlementAllowlist(t *testing.T) {
	fake := newPerchModelFakeServer(t, `[{"key":"limits.allowed_models","value_json":{"models":["wandb-kimi-k2-6","  ","wandb-glm-5"]}}]`, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)

	models, err := service.PerchAvailableModels(t.Context(), account)
	if err != nil {
		t.Fatalf("PerchAvailableModels: %v", err)
	}
	for _, model := range models {
		if model.DisplayName != model.ID || model.Type != "model" {
			t.Fatalf("model entry %+v is not a plain model descriptor", model)
		}
	}
	want := "wandb-kimi-k2-6,wandb-glm-5"
	if got := strings.Join(perchModelIDs(models), ","); got != want {
		t.Fatalf("models = %v, want %v", got, want)
	}
}

func TestPerchAvailableModelsFallsBackToCatalogWhenUnrestricted(t *testing.T) {
	fake := newPerchModelFakeServer(t, perchUnrestrictedEntitlements, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)

	models, err := service.PerchAvailableModels(t.Context(), account)
	if err != nil {
		t.Fatalf("PerchAvailableModels: %v", err)
	}
	want := strings.Join(perch.DefaultModelIDs(), ",")
	if got := strings.Join(perchModelIDs(models), ","); got != want {
		t.Fatalf("models = %v, want the Perch catalog %v", got, want)
	}
}

func TestPerchAvailableModelsUsesExplicitMappingWithoutUpstreamCall(t *testing.T) {
	fake := newPerchModelFakeServer(t, perchUnrestrictedEntitlements, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)
	account.Credentials["model_mapping"] = map[string]any{
		"perch-zulu":  "wandb-kimi-k2-6",
		"perch-alpha": "wandb-qwen3-6-35b-a3b",
	}

	models, err := service.PerchAvailableModels(t.Context(), account)
	if err != nil {
		t.Fatalf("PerchAvailableModels: %v", err)
	}
	if got := strings.Join(perchModelIDs(models), ","); got != "perch-alpha,perch-zulu" {
		t.Fatalf("models = %v, want the configured public names", got)
	}
	if accountHits, _ := fake.counts(); accountHits != 0 {
		t.Fatalf("account endpoint hits = %d, want 0 for an explicit mapping", accountHits)
	}
}

func TestPerchAvailableModelsReusesOneAccountFetch(t *testing.T) {
	fake := newPerchModelFakeServer(t, `[{"key":"limits.allowed_models","value_json":{"models":["wandb-kimi-k2-6"]}}]`, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)

	for i := 0; i < 3; i++ {
		if _, err := service.PerchAvailableModels(t.Context(), account); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if accountHits, _ := fake.counts(); accountHits != 1 {
		t.Fatalf("account endpoint hits = %d across three dialog opens, want 1", accountHits)
	}
}

func TestPerchAccountTestSendsPerchModelCall(t *testing.T) {
	fake := newPerchModelFakeServer(t, `[{"key":"limits.allowed_models","value_json":{"models":["wandb-kimi-k2-6"]}}]`, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)
	c, recorder := newPerchGinContext(t, "")

	if err := service.testPerchAccountConnection(c, account, "wandb-kimi-k2-6", "hello"); err != nil {
		t.Fatalf("testPerchAccountConnection: %v (%s)", err, recorder.Body.String())
	}

	model, body := fake.last()
	if model != "wandb-kimi-k2-6" {
		t.Fatalf("upstream model = %q, want the Perch model id (body=%s)", model, body)
	}
	if strings.Contains(strings.ToLower(body), "claude") {
		t.Fatalf("Perch test request carries an Anthropic model: %s", body)
	}
	stream := recorder.Body.String()
	for _, want := range []string{"\"type\":\"test_start\"", "\"model\":\"wandb-kimi-k2-6\"", "\"type\":\"content\"", "pong", "\"type\":\"test_complete\"", "\"success\":true"} {
		if !strings.Contains(stream, want) {
			t.Fatalf("stream misses %s: %s", want, stream)
		}
	}
	if _, modelCalls := fake.counts(); modelCalls != 1 {
		t.Fatalf("model calls = %d, want 1", modelCalls)
	}
}

func TestPerchAccountTestDefaultsToCatalogModel(t *testing.T) {
	fake := newPerchModelFakeServer(t, perchUnrestrictedEntitlements, "pong")
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)
	c, recorder := newPerchGinContext(t, "")

	if err := service.testPerchAccountConnection(c, account, "", ""); err != nil {
		t.Fatalf("testPerchAccountConnection: %v (%s)", err, recorder.Body.String())
	}
	model, body := fake.last()
	want := perch.DefaultModelIDs()[0]
	if model != want {
		t.Fatalf("upstream model = %q, want the Perch default %q (body=%s)", model, want, body)
	}
}

func TestPerchAccountTestReportsUpstreamRefusal(t *testing.T) {
	fake := &perchModelFakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		default:
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			fmt.Fprint(w, `{"ok":false,"error":"plan does not include this model","errorCode":"perch_plan_required"}`)
		}
	}))
	t.Cleanup(fake.Close)
	service := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)
	c, recorder := newPerchGinContext(t, "")

	if err := service.testPerchAccountConnection(c, account, "wandb-kimi-k2-6", "hello"); err == nil {
		t.Fatalf("expected an error for a refused model call")
	}
	if !strings.Contains(recorder.Body.String(), "\"type\":\"error\"") {
		t.Fatalf("stream has no error event: %s", recorder.Body.String())
	}
}

func TestPerchGroupModelCandidatesUsePerchCatalog(t *testing.T) {
	got := strings.Join(defaultModelsListCandidateIDs(PlatformPerch), ",")
	if want := strings.Join(perch.DefaultModelIDs(), ","); got != want {
		t.Fatalf("group model candidates = %s, want the Perch catalog %s", got, want)
	}
}
