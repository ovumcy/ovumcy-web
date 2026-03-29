CREATE TABLE IF NOT EXISTS oidc_logout_states (
    session_id TEXT PRIMARY KEY,
    end_session_endpoint TEXT NOT NULL,
    id_token_hint TEXT NOT NULL,
    post_logout_redirect_url TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_oidc_logout_states_expires_at
    ON oidc_logout_states (expires_at);
