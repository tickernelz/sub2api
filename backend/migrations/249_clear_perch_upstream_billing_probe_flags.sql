UPDATE accounts
SET extra = extra
	- 'upstream_billing_probe_enabled'
	- 'upstream_billing_rate_sync_enabled'
	- 'upstream_billing_probe'
WHERE platform = 'perch'
	AND (
		extra ? 'upstream_billing_probe_enabled'
		OR extra ? 'upstream_billing_rate_sync_enabled'
		OR extra ? 'upstream_billing_probe'
	);
