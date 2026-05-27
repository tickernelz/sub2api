-- Normalize Antigravity Pro High aliases to the upstream model that actually works.
-- Antigravity rejects raw gemini-3.1-pro-high / gemini-3-pro-high IDs; use gemini-pro-agent.

UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{model_mapping}',
    (
        credentials->'model_mapping'
        || jsonb_build_object(
            'gemini-pro-agent', 'gemini-pro-agent',
            'gemini-3-pro-high', 'gemini-pro-agent',
            'gemini-3-pro-preview', 'gemini-pro-agent',
            'gemini-3.1-pro-high', 'gemini-pro-agent',
            'gemini-3.1-pro-preview', 'gemini-pro-agent',
            'gemini-3-pro-low', 'gemini-3.1-pro-low'
        )
    )
)
WHERE platform = 'antigravity'
  AND deleted_at IS NULL
  AND credentials->'model_mapping' IS NOT NULL;
