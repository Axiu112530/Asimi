package app

// Historical migrations remain immutable. AI state is additive and separate from Phase 1 profiles.
const schemaV3 = `
CREATE TABLE ai_profiles(id TEXT PRIMARY KEY,json TEXT NOT NULL,deleted INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE ai_models(provider_id TEXT NOT NULL REFERENCES ai_profiles(id),id TEXT NOT NULL,json TEXT NOT NULL,PRIMARY KEY(provider_id,id));
CREATE TABLE ai_credentials(credential_id TEXT PRIMARY KEY,provider_id TEXT NOT NULL UNIQUE REFERENCES ai_profiles(id),encrypted_blob BLOB NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE ai_settings(id TEXT PRIMARY KEY CHECK(id='ai'),json TEXT NOT NULL);
CREATE TABLE ai_conversations(conversation_id TEXT PRIMARY KEY REFERENCES conversations(id),playground INTEGER NOT NULL DEFAULT 0,context_since TEXT NOT NULL DEFAULT '',deleted INTEGER NOT NULL DEFAULT 0);
CREATE TABLE ai_message_metadata(message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,json TEXT NOT NULL);
CREATE TABLE ai_requests(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),message_id TEXT NOT NULL REFERENCES messages(id),provider_id TEXT NOT NULL,model_id TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE ai_usage(request_id TEXT PRIMARY KEY REFERENCES ai_requests(id),json TEXT NOT NULL);
CREATE TABLE ai_attachments(id TEXT PRIMARY KEY,json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE ai_message_attachments(message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,attachment_id TEXT NOT NULL REFERENCES ai_attachments(id),PRIMARY KEY(message_id,attachment_id));
CREATE TABLE ai_attachment_cleanup(attachment_id TEXT PRIMARY KEY,conversation_id TEXT REFERENCES conversations(id),created_at TEXT NOT NULL);
PRAGMA user_version=3;`
