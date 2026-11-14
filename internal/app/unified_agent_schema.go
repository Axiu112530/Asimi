package app

const unifiedAgentSchema = `
CREATE TABLE IF NOT EXISTS unified_runs(
 id TEXT PRIMARY KEY,kind TEXT NOT NULL,goal TEXT NOT NULL,status TEXT NOT NULL,
 task_id TEXT NOT NULL DEFAULT '',request_id TEXT NOT NULL DEFAULT '',action_id TEXT NOT NULL DEFAULT '',
 conversation_id TEXT NOT NULL DEFAULT '',parent_id TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS unified_runs_recent ON unified_runs(created_at,id);
CREATE TABLE IF NOT EXISTS unified_summary_links(
 source_id TEXT NOT NULL REFERENCES unified_runs(id) ON DELETE CASCADE,request_id TEXT NOT NULL UNIQUE REFERENCES ai_requests(id) ON DELETE CASCADE,
 context_hash TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(source_id,request_id));

CREATE TABLE IF NOT EXISTS unified_summary_intents(
 source_id TEXT NOT NULL REFERENCES unified_runs(id) ON DELETE CASCADE,
 context_hash TEXT NOT NULL,request_id TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,
 PRIMARY KEY(source_id,context_hash));

CREATE TRIGGER IF NOT EXISTS unified_task_insert AFTER INSERT ON agent_tasks BEGIN
 INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,task_id,conversation_id,created_at,updated_at)
 VALUES(NEW.id,CASE WHEN json_extract(NEW.json,'$.desktop')=1 THEN 'desktop' ELSE 'development' END,
 COALESCE(json_extract(NEW.json,'$.goal'),''),NEW.status,NEW.id,COALESCE(json_extract(NEW.json,'$.conversationId'),''),NEW.created_at,NEW.updated_at);
END;
CREATE TRIGGER IF NOT EXISTS unified_task_update AFTER UPDATE ON agent_tasks BEGIN
 UPDATE unified_runs SET status=NEW.status,updated_at=NEW.updated_at WHERE task_id=NEW.id;
END;
CREATE TRIGGER IF NOT EXISTS unified_task_delete AFTER DELETE ON agent_tasks BEGIN
 DELETE FROM unified_runs WHERE task_id=OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS unified_chat_insert AFTER INSERT ON ai_requests BEGIN
 INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,request_id,conversation_id,created_at,updated_at)
 VALUES(NEW.id,'chat',COALESCE((SELECT content FROM messages WHERE conversation_id=NEW.conversation_id AND role='user'
 AND created_at<=NEW.created_at ORDER BY created_at DESC LIMIT 1),''),NEW.status,NEW.id,NEW.conversation_id,NEW.created_at,NEW.created_at);
END;
CREATE TRIGGER IF NOT EXISTS unified_chat_update AFTER UPDATE OF status ON ai_requests BEGIN
 UPDATE unified_runs SET status=NEW.status,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE request_id=NEW.id;
END;
CREATE TRIGGER IF NOT EXISTS unified_chat_delete AFTER DELETE ON ai_requests BEGIN
 DELETE FROM unified_runs WHERE request_id=OLD.id;
END;
INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,task_id,conversation_id,created_at,updated_at)
 SELECT id,CASE WHEN json_extract(json,'$.desktop')=1 THEN 'desktop' ELSE 'development' END,
 COALESCE(json_extract(json,'$.goal'),''),status,id,COALESCE(json_extract(json,'$.conversationId'),''),created_at,updated_at FROM agent_tasks;
INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,request_id,conversation_id,created_at,updated_at)
 SELECT r.id,'chat',COALESCE((SELECT content FROM messages WHERE conversation_id=r.conversation_id AND role='user'
 AND created_at<=r.created_at ORDER BY created_at DESC LIMIT 1),''),r.status,r.id,r.conversation_id,r.created_at,r.created_at FROM ai_requests r;
`
