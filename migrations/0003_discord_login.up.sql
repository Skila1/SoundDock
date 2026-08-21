-- Login OAuth state (Discord sign-in). user_id is unknown until callback.

CREATE TABLE login_states (
    state TEXT PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT 'discord',
    code_verifier_enc BYTEA,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
