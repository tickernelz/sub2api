//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountTestService_PerchAccountRoutesToThePerchModelCall(t *testing.T) {
	fake := newPerchModelFakeServer(t, perchUnrestrictedEntitlements, "pong")
	svc := newPerchModelsService(t, fake)
	account := newPerchModelsAccount(fake)
	svc.accountRepo = &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	c, recorder := newTestContext()

	err := svc.TestAccountConnection(c, account.ID, "wandb-kimi-k2-6", "hello", AccountTestModeDefault)

	require.NoError(t, err)
	_, modelCalls := fake.counts()
	require.Equal(t, 1, modelCalls)
	stream := recorder.Body.String()
	require.Contains(t, stream, "\"type\":\"test_complete\"")
	require.NotContains(t, strings.ToLower(stream), "claude")
}
