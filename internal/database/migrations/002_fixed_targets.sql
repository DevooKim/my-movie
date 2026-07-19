DROP TABLE notification_deliveries;

ALTER TABLE subscriptions RENAME TO subscriptions_legacy;

CREATE TABLE subscriptions (
  id TEXT PRIMARY KEY,
  discord_user_id TEXT NOT NULL,
  provider TEXT NOT NULL CHECK (provider IN ('cgv', 'megabox')),
  target_id TEXT NOT NULL,
  auditorium_name TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  theater_name TEXT NOT NULL,
  theater_area_code TEXT NOT NULL DEFAULT '',
  movie_id TEXT NOT NULL,
  movie_name TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('initializing', 'active', 'disabled')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(discord_user_id, provider, target_id, movie_id)
);

INSERT INTO subscriptions(
  id, discord_user_id, provider, target_id, auditorium_name,
  theater_id, theater_name, theater_area_code, movie_id, movie_name,
  status, created_at, updated_at
)
SELECT id, discord_user_id, provider, '', '',
       theater_id, theater_name, theater_area_code, movie_id, movie_name,
       'disabled', created_at, updated_at
FROM subscriptions_legacy;

DROP TABLE subscriptions_legacy;

CREATE TABLE notification_deliveries (
  subscription_id TEXT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  showtime_key TEXT NOT NULL REFERENCES showtimes(key) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('baseline', 'pending', 'sent', 'failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT,
  last_error TEXT,
  PRIMARY KEY(subscription_id, showtime_key)
);

ALTER TABLE showtimes ADD COLUMN target_id TEXT NOT NULL DEFAULT '';
ALTER TABLE poll_runs ADD COLUMN target_id TEXT NOT NULL DEFAULT '';
