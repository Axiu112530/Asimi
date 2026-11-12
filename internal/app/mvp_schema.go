package app

// Version 5 is additive. The existing task_steps belongs to legacy chat tasks.
const schemaV5 = `
CREATE TABLE workspaces(id TEXT PRIMARY KEY,root TEXT NOT NULL COLLATE NOCASE UNIQUE,name TEXT NOT NULL,created_at TEXT NOT NULL,last_used_at TEXT NOT NULL,trust_status TEXT NOT NULL CHECK(trust_status='USER_SELECTED'));
CREATE TABLE agent_tasks(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL REFERENCES workspaces(id),status TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE agent_task_steps(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES agent_tasks(id),ordinal INTEGER NOT NULL,status TEXT NOT NULL,json TEXT NOT NULL,UNIQUE(task_id,ordinal));
CREATE TABLE product_actions(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL REFERENCES workspaces(id),task_id TEXT REFERENCES agent_tasks(id),kind TEXT NOT NULL,status TEXT NOT NULL,proposal_json TEXT NOT NULL,hash TEXT NOT NULL,consumed INTEGER NOT NULL DEFAULT 0 CHECK(consumed IN (0,1)),expires_at TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,result_json TEXT,error_code TEXT NOT NULL DEFAULT '');
CREATE TABLE product_action_journal(id TEXT PRIMARY KEY,action_id TEXT NOT NULL REFERENCES product_actions(id),event TEXT NOT NULL,json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE product_action_intents(action_id TEXT PRIMARY KEY REFERENCES product_actions(id),proposal_json TEXT NOT NULL,hash TEXT NOT NULL,settled INTEGER NOT NULL DEFAULT 0 CHECK(settled IN (0,1)),created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE skill_settings(id TEXT PRIMARY KEY,enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),updated_at TEXT NOT NULL);
CREATE TABLE onboarding_state(id TEXT PRIMARY KEY CHECK(id='onboarding'),completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1)),updated_at TEXT NOT NULL);
INSERT INTO onboarding_state(id,completed,updated_at) VALUES('onboarding',0,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
CREATE INDEX product_actions_status ON product_actions(status,expires_at);
CREATE INDEX agent_tasks_status ON agent_tasks(status,updated_at);
PRAGMA user_version=5;`
