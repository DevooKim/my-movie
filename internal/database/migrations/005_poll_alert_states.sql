CREATE TABLE poll_alert_states (
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  failed INTEGER NOT NULL,
  error_summary TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(provider, theater_id)
);
