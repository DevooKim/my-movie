ALTER TABLE installations ADD COLUMN viewer_role_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN notice_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN guide_channel_id TEXT NOT NULL DEFAULT '';
ALTER TABLE installations ADD COLUMN guide_message_id TEXT NOT NULL DEFAULT '';
