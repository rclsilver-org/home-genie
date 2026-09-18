DROP INDEX idx_messages_publish_token;
ALTER TABLE messages DROP COLUMN publish_token_id;
ALTER TABLE publish_tokens DROP COLUMN icon_type;
ALTER TABLE publish_tokens DROP COLUMN icon;
