ALTER TABLE poll_alert_states ADD COLUMN control_delivered INTEGER NOT NULL DEFAULT 1;
ALTER TABLE poll_alert_states ADD COLUMN status_delivered INTEGER NOT NULL DEFAULT 1;

UPDATE poll_alert_states SET status_delivered = 0 WHERE failed = 1;
