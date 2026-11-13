package app

// This extension runs after the historical v6 schema and the V1 cognition
// tables. All DDL, triggers and the initial FTS index are committed together.
const cognitionV11Schema = `
CREATE TABLE IF NOT EXISTS cognition_revision(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL);
INSERT OR IGNORE INTO cognition_revision VALUES(1,0);
CREATE TABLE IF NOT EXISTS cognition_memory_state(
 memory_id TEXT PRIMARY KEY REFERENCES memory_records(id) ON DELETE CASCADE,
 archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0,1)), priority INTEGER NOT NULL DEFAULT 0 CHECK(priority BETWEEN 0 AND 3)
);
CREATE VIRTUAL TABLE IF NOT EXISTS cognition_memory_search USING fts5(
 key,text,content='memory_records',content_rowid='rowid',tokenize='unicode61'
);
CREATE TABLE IF NOT EXISTS cognition_schema_revisions(version INTEGER PRIMARY KEY);
INSERT INTO cognition_memory_search(cognition_memory_search)
 SELECT 'rebuild' WHERE NOT EXISTS(SELECT 1 FROM cognition_schema_revisions WHERE version=11);
INSERT OR IGNORE INTO cognition_schema_revisions VALUES(11);
CREATE TRIGGER IF NOT EXISTS cognition_memory_insert AFTER INSERT ON memory_records BEGIN
 INSERT INTO cognition_memory_search(rowid,key,text) VALUES(NEW.rowid,NEW.key,NEW.text);
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_memory_update AFTER UPDATE ON memory_records BEGIN
 INSERT INTO cognition_memory_search(cognition_memory_search,rowid,key,text) VALUES('delete',OLD.rowid,OLD.key,OLD.text);
 INSERT INTO cognition_memory_search(rowid,key,text) VALUES(NEW.rowid,NEW.key,NEW.text);
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_memory_delete AFTER DELETE ON memory_records BEGIN
 INSERT INTO cognition_memory_search(cognition_memory_search,rowid,key,text) VALUES('delete',OLD.rowid,OLD.key,OLD.text);
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_memory_state_insert AFTER INSERT ON cognition_memory_state BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_memory_state_update AFTER UPDATE ON cognition_memory_state BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_skill_insert AFTER INSERT ON skill_packages BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_skill_update AFTER UPDATE ON skill_packages BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_skill_delete AFTER DELETE ON skill_packages BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_official_skill_update AFTER UPDATE ON skill_settings BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_settings_insert AFTER INSERT ON cognition_settings BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TRIGGER IF NOT EXISTS cognition_settings_update AFTER UPDATE ON cognition_settings BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1;
END;
CREATE TABLE IF NOT EXISTS cognition_skill_state(
 package_id TEXT PRIMARY KEY REFERENCES skill_packages(id) ON DELETE CASCADE,
 status TEXT NOT NULL CHECK(status IN ('deprecated','archived'))
);
CREATE TABLE IF NOT EXISTS cognition_memory_changes(
 id INTEGER PRIMARY KEY, memory_id TEXT NOT NULL REFERENCES memory_records(id) ON DELETE CASCADE,
 previous TEXT NOT NULL,current TEXT NOT NULL,source TEXT NOT NULL,created_at TEXT NOT NULL
);
CREATE TRIGGER IF NOT EXISTS cognition_memory_conflict AFTER UPDATE OF text ON memory_records
 WHEN OLD.text<>NEW.text AND NEW.kind IN ('user','project') BEGIN
 INSERT INTO cognition_memory_changes(memory_id,previous,current,source,created_at)
 VALUES(NEW.id,OLD.text,NEW.text,NEW.source,NEW.updated_at);
END;
CREATE TABLE IF NOT EXISTS cognition_skill_candidates(
 id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 manifest TEXT NOT NULL, base_version INTEGER NOT NULL, base_checksum TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','approved','rejected')), created_at TEXT NOT NULL, reviewed_at TEXT,
 UNIQUE(task_id)
);
CREATE INDEX IF NOT EXISTS cognition_candidates_review ON cognition_skill_candidates(workspace_id,status,created_at);
CREATE TABLE IF NOT EXISTS cognition_learning_history(
 task_id TEXT PRIMARY KEY REFERENCES learning_jobs(task_id) ON DELETE CASCADE,
 attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
 duration_ms INTEGER NOT NULL DEFAULT 0, completed_at TEXT, created_at TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','done','failed','cancelled','expired')),
 next_attempt_at TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO cognition_learning_history(task_id,created_at,status)
 SELECT task_id,updated_at,CASE WHEN status='done' THEN 'done' ELSE 'queued' END FROM learning_jobs;
CREATE TRIGGER IF NOT EXISTS cognition_learning_enqueued AFTER INSERT ON learning_jobs BEGIN
 INSERT INTO cognition_learning_history(task_id,created_at) VALUES(NEW.task_id,NEW.updated_at);
END;
`
