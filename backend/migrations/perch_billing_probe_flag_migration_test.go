package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClearPerchUpstreamBillingProbeFlagsMigration(t *testing.T) {
	content, err := FS.ReadFile("249_clear_perch_upstream_billing_probe_flags.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "UPDATE accounts SET extra = extra - 'upstream_billing_probe_enabled' - 'upstream_billing_rate_sync_enabled' - 'upstream_billing_probe'")
	require.Contains(t, sql, "WHERE platform = 'perch'")
	require.Contains(t, sql, "extra ? 'upstream_billing_probe_enabled'")
	require.Contains(t, sql, "extra ? 'upstream_billing_rate_sync_enabled'")
	require.Contains(t, sql, "extra ? 'upstream_billing_probe'")
}
