-- Add normalized many-to-many API key group assignments.
CREATE TABLE IF NOT EXISTS api_key_groups (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (api_key_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_api_key_groups_group_id ON api_key_groups(group_id);
CREATE INDEX IF NOT EXISTS idx_api_key_groups_api_key_priority ON api_key_groups(api_key_id, priority, group_id);

INSERT INTO api_key_groups (api_key_id, group_id, priority)
SELECT id, group_id, 0
FROM api_keys
WHERE group_id IS NOT NULL
  AND deleted_at IS NULL
ON CONFLICT DO NOTHING;
