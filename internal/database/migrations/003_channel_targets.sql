CREATE TABLE installations (
  guild_id TEXT PRIMARY KEY,
  owner_user_id TEXT NOT NULL,
  category_id TEXT NOT NULL,
  control_channel_id TEXT NOT NULL,
  control_message_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE target_states (
  target_id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  updated_at TEXT NOT NULL
);

CREATE TABLE target_baselines (
  target_id TEXT NOT NULL REFERENCES target_states(target_id) ON DELETE CASCADE,
  showtime_id TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  PRIMARY KEY(target_id, showtime_id)
);

CREATE TABLE target_showtimes (
  target_id TEXT NOT NULL REFERENCES target_states(target_id) ON DELETE CASCADE,
  showtime_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  theater_id TEXT NOT NULL,
  theater_name TEXT NOT NULL,
  movie_id TEXT NOT NULL,
  movie_name TEXT NOT NULL,
  movie_english_name TEXT NOT NULL DEFAULT '',
  play_date TEXT NOT NULL,
  starts_at TEXT NOT NULL,
  ends_at TEXT NOT NULL,
  auditorium TEXT NOT NULL,
  format TEXT NOT NULL,
  rating TEXT NOT NULL DEFAULT '',
  remaining_seats INTEGER NOT NULL DEFAULT 0,
  total_seats INTEGER NOT NULL DEFAULT 0,
  seat_count_known INTEGER NOT NULL CHECK (seat_count_known IN (0, 1)),
  poster_url TEXT NOT NULL DEFAULT '',
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  PRIMARY KEY(target_id, showtime_id)
);

CREATE TABLE channel_deliveries (
  target_id TEXT NOT NULL,
  showtime_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT,
  last_error TEXT,
  PRIMARY KEY(target_id, showtime_id),
  FOREIGN KEY(target_id, showtime_id)
    REFERENCES target_showtimes(target_id, showtime_id) ON DELETE CASCADE
);
