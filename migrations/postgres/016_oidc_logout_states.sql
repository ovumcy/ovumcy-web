CREATE TABLE IF NOT EXISTS oidc_logout_states (
    session_id VARCHAR(64) PRIMARY KEY,
    end_session_endpoint TEXT NOT NULL,
    id_token_hint TEXT NOT NULL,
    post_logout_redirect_url TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oidc_logout_states_expires_at
    ON oidc_logout_states (expires_at);
