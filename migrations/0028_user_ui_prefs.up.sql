-- Per-account UI preferences (library layouts, sorts, collapsed sections), so
-- they follow the user across browsers and devices.
CREATE TABLE IF NOT EXISTS user_ui_prefs (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
