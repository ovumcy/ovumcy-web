ALTER TABLE oidc_logout_states ADD COLUMN user_id INTEGER;

CREATE INDEX IF NOT EXISTS idx_oidc_logout_states_user_id
    ON oidc_logout_states (user_id);
