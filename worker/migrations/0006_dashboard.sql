ALTER TABLE exchanges ADD COLUMN provider TEXT;
ALTER TABLE exchanges ADD COLUMN finish_reason TEXT;
ALTER TABLE exchanges ADD COLUMN access_token_label TEXT;
ALTER TABLE exchanges ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE exchanges ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS exchanges_ts_id ON exchanges(ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS exchanges_provider_ts ON exchanges(provider, ts DESC);
CREATE INDEX IF NOT EXISTS exchanges_model_ts ON exchanges(model, ts DESC);
CREATE INDEX IF NOT EXISTS exchanges_harness_ts ON exchanges(harness, ts DESC);
