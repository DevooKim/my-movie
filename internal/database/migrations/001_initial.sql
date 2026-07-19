CREATE TABLE IF NOT EXISTS subscriptions (
  id TEXT PRIMARY KEY,
  discord_user_id TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('cgv', 'megabox')),
  theater_id TEXT NOT NULL,
  theater_name TEXT NOT NULL,
  theater_area_code TEXT NOT NULL DEFAULT '',
  movie_id TEXT NOT NULL,
  movie_name TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('initializing', 'active', 'disabled')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(discord_user_id, provider, theater_id, movie_id)
);

CREATE TABLE IF NOT EXISTS showtimes (
  key TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  play_date TEXT NOT NULL,
  starts_at TEXT NOT NULL,
  auditorium TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
  subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  showtime_key TEXT NOT NULL REFERENCES showtimes(key) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('baseline', 'pending', 'sent', 'failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT,
  last_error TEXT,
  PRIMARY KEY(subscription_id, showtime_key)
);

CREATE TABLE IF NOT EXISTS poll_runs (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  succeeded INTEGER,
  showtime_count INTEGER,
  error_summary TEXT
);
