ALTER TABLE oidc_logout_states ADD COLUMN IF NOT EXISTS user_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_oidc_logout_states_user_id
    ON oidc_logout_states (user_id);
