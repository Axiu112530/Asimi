package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"asimi/internal/developer"
	"asimi/internal/domain"
)

const cognitionSchema = `
CREATE TABLE IF NOT EXISTS cognition_settings(id INTEGER PRIMARY KEY CHECK(id=1),settings TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS memory_records(
 id TEXT PRIMARY KEY, workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('user','project','episode','procedure')),
 key TEXT NOT NULL, text TEXT NOT NULL, source TEXT NOT NULL,
 verified INTEGER NOT NULL CHECK(verified IN (0,1)), updated_at TEXT NOT NULL,
 CHECK((kind='user' AND workspace_id IS NULL) OR (kind<>'user' AND workspace_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS memory_scope_key ON memory_records(COALESCE(workspace_id,''),kind,key);
CREATE INDEX IF NOT EXISTS memory_recent ON memory_records(workspace_id,updated_at);
CREATE TABLE IF NOT EXISTS skill_packages(
 id TEXT PRIMARY KEY, workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
 slug TEXT NOT NULL, source TEXT NOT NULL, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS skill_scope_slug ON skill_packages(COALESCE(workspace_id,''),slug);
CREATE TABLE IF NOT EXISTS skill_versions(
 package_id TEXT NOT NULL REFERENCES skill_packages(id) ON DELETE CASCADE,
 version INTEGER NOT NULL, manifest TEXT NOT NULL, checksum TEXT NOT NULL, created_at TEXT NOT NULL,
 PRIMARY KEY(package_id,version)
);
CREATE TABLE IF NOT EXISTS cognition_package_writes(
 package_id TEXT NOT NULL,version INTEGER NOT NULL,manifest TEXT NOT NULL,checksum TEXT NOT NULL,
 deleted INTEGER NOT NULL CHECK(deleted IN (0,1)),updated_at TEXT NOT NULL,PRIMARY KEY(package_id,version)
);
CREATE TRIGGER IF NOT EXISTS cognition_queue_deleted_skill BEFORE DELETE ON skill_versions BEGIN
 INSERT INTO cognition_package_writes(package_id,version,manifest,checksum,deleted,updated_at)
 VALUES(OLD.package_id,OLD.version,'','',1,strftime('%Y-%m-%dT%H:%M:%fZ','now'))
 ON CONFLICT(package_id,version) DO UPDATE SET manifest='',checksum='',deleted=1,updated_at=excluded.updated_at;
END;
CREATE TABLE IF NOT EXISTS skill_usage(
 package_id TEXT NOT NULL, version INTEGER NOT NULL,
 task_id TEXT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE, created_at TEXT NOT NULL,
 PRIMARY KEY(package_id,version,task_id), FOREIGN KEY(package_id,version) REFERENCES skill_versions(package_id,version) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS learning_jobs(
 task_id TEXT PRIMARY KEY REFERENCES agent_tasks(id) ON DELETE CASCADE,
 status TEXT NOT NULL CHECK(status IN ('pending','done','paused')), updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS learning_pending ON learning_jobs(status,updated_at);
CREATE TABLE IF NOT EXISTS cognition_learning_control(id INTEGER PRIMARY KEY CHECK(id=1),paused INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS cognition_patterns(
 pattern TEXT NOT NULL, task_id TEXT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE, PRIMARY KEY(pattern,task_id)
);
`

type cognitionSettings struct {
	Memory                 bool `json:"memory"`
	AutomaticLearning      bool `json:"automaticLearning"`
	AutomaticSkillCreation bool `json:"automaticSkillCreation"`
	ProjectLearning        bool `json:"projectLearning"`
}

type cognitionMemory struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Text        string `json:"text"`
	Source      string `json:"source"`
	Verified    bool   `json:"verified"`
	Archived    bool   `json:"archived,omitempty"`
	Priority    int    `json:"priority,omitempty"`
	UpdatedAt   string `json:"updatedAt"`
}

type cognitionState struct {
	MemoryChanges []cognitionMemoryChange   `json:"memoryChanges"`
	Settings      cognitionSettings         `json:"settings"`
	Memories      []cognitionMemory         `json:"memories"`
	Skills        []cognitionSkill          `json:"skills"`
	Candidates    []cognitionCandidate      `json:"candidates"`
	Learning      []cognitionLearningRecord `json:"learning"`
	Pending       int                       `json:"pending"`
	Paused        int                       `json:"paused"`
	Completed     int                       `json:"completed"`
}

func loadCognitionSettings(q querier) (cognitionSettings, error) {
	var settings cognitionSettings
	var raw string
	err := q.QueryRow("SELECT settings FROM cognition_settings WHERE id=1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return cognitionSettings{Memory: true, AutomaticLearning: true, ProjectLearning: true}, nil
	}
	if err != nil {
		return settings, err
	}
	return settings, json.Unmarshal([]byte(raw), &settings)
}

func cognitionText(value string, maxRunes int) bool {
	return strings.TrimSpace(value) != "" && validAgentText(value, maxRunes) &&
		developer.RedactAgentText(value) == value && !cognitionCredential.MatchString(value)
}

var cognitionCredential = regexp.MustCompile(`(?i)(?:api[ _-]?key|password|passphrase|private[ _-]?key|(?:access|refresh|session)[ _-]?token|密码|密钥|令牌)[[:space:]]*(?:is[[:space:]]+|[=:：])[[:space:]]*\S`)

func (s *Service) callCognition(method, payload string) (domain.Reply, bool) {
	done := func(v any, err error) (domain.Reply, bool) { return result(v, err), true }
	switch method {
	case "GetCognitionState":
		var in struct {
			WorkspaceID string `json:"workspaceId"`
		}
		if err := decode(payload, &in); err != nil {
			return done(nil, err)
		}
		value, err := s.getCognitionState(in.WorkspaceID)
		return done(value, err)
	case "SetCognitionSettings":
		var settings cognitionSettings
		if err := decode(payload, &settings); err != nil {
			return done(nil, err)
		}
		s.cognitionMu.Lock()
		defer s.cognitionMu.Unlock()
		_, err := s.db.Exec("INSERT INTO cognition_settings(id,settings) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET settings=excluded.settings", encode(settings))
		return done(settings, err)
	case "SaveCognitionMemory":
		var in struct {
			WorkspaceID string `json:"workspaceId,omitempty"`
			Kind        string `json:"kind"`
			Key         string `json:"key"`
			Text        string `json:"text"`
		}
		if err := decode(payload, &in); err != nil {
			return done(nil, err)
		}
		if (in.Kind != "user" || in.WorkspaceID != "") && (in.Kind != "project" || in.WorkspaceID == "") ||
			!cognitionText(in.Key, 100) || !cognitionText(in.Text, 2400) {
			return done(nil, codedError("COGNITION_INVALID_MEMORY"))
		}
		if in.WorkspaceID != "" {
			if _, err := s.workspace(in.WorkspaceID); err != nil {
				return done(nil, err)
			}
		}
		s.cognitionMu.Lock()
		defer s.cognitionMu.Unlock()
		err := s.transaction(func(tx *sql.Tx) error {
			if err := saveCognitionMemory(tx, in.WorkspaceID, in.Kind, strings.ToLower(strings.TrimSpace(in.Key)), strings.TrimSpace(in.Text), "manual", true); err != nil {
				return err
			}
			return audit(tx, "", "COGNITION_MEMORY_SAVED", map[string]string{"kind": in.Kind, "workspaceId": in.WorkspaceID})
		})
		return done(err == nil, err)
	case "DismissCognitionMemoryChange":
		err := s.dismissCognitionMemoryChange(payload)
		return done(err == nil, err)
	case "ArchiveCognitionMemory":
		err := s.archiveCognitionMemory(payload)
		return done(err == nil, err)
	case "DeleteCognitionMemory":
		var in struct {
			ID          string `json:"id"`
			WorkspaceID string `json:"workspaceId,omitempty"`
		}
		if err := decode(payload, &in); err != nil {
			return done(nil, err)
		}
		s.cognitionMu.Lock()
		defer s.cognitionMu.Unlock()
		err := s.transaction(func(tx *sql.Tx) error {
			var kind, key string
			if err := tx.QueryRow("SELECT kind,key FROM memory_records WHERE id=? AND COALESCE(workspace_id,'')=?", in.ID, in.WorkspaceID).Scan(&kind, &key); err != nil {
				return mvpNotFound(err)
			}
			if kind == "episode" {
				if _, err := tx.Exec("UPDATE cognition_skill_candidates SET status='rejected',reviewed_at=? WHERE task_id=? AND workspace_id=? AND status='pending'", now(), key, in.WorkspaceID); err != nil {
					return err
				}
				if _, err := tx.Exec("UPDATE skill_packages SET enabled=0,updated_at=? WHERE source=?", now(), "reviewed:"+key); err != nil {
					return err
				}
				if _, err := tx.Exec("DELETE FROM memory_records WHERE workspace_id=? AND kind='procedure' AND source=?", in.WorkspaceID, "task:"+key); err != nil {
					return err
				}
				if _, err := tx.Exec("DELETE FROM cognition_patterns WHERE task_id=? AND workspace_id=?", key, in.WorkspaceID); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE skill_packages SET enabled=0,updated_at=? WHERE workspace_id IS NULL
				AND source LIKE 'learned:%' AND enabled=1
				AND (SELECT COUNT(*) FROM cognition_patterns WHERE pattern=substr(skill_packages.source,9))<3`, now()); err != nil {
					return err
				}
			}
			r, err := tx.Exec("DELETE FROM memory_records WHERE id=? AND COALESCE(workspace_id,'')=?", in.ID, in.WorkspaceID)
			if err := cognitionAffected(r, err); err != nil {
				return err
			}
			return audit(tx, "", "COGNITION_MEMORY_DELETED", map[string]string{"id": in.ID})
		})
		return done(err == nil, err)
	case "SaveCognitionSkill", "ChangeCognitionSkill", "ListCognitionSkillVersions", "ImportCognitionSkill", "ExportCognitionSkill":
		value, err := s.manageCognitionSkill(method, payload)
		return done(value, err)
	case "SetAgentCognitionCorrection":
		value, err := s.setAgentCognitionCorrection(payload)
		return done(value, err)
	case "SetAgentCognitionChoice":
		value, err := s.setCognitionChoices(payload)
		return done(value, err)
	case "ReviewCognitionCandidate":
		err := s.reviewCognitionCandidate(payload)
		return done(err == nil, err)
	case "ChangeCognitionJob":
		err := s.updateCognitionJob(payload)
		return done(err == nil, err)
	case "PauseCognitionLearning", "ResumeCognitionLearning":
		if err := decode(payload, &struct{}{}); err != nil {
			return done(nil, err)
		}
		err := s.changeCognitionLearning(method == "PauseCognitionLearning")
		return done(err == nil, err)
	}
	return domain.Reply{}, false
}

func cognitionAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return codedError("NOT_FOUND")
	}
	return nil
}

func saveCognitionMemory(q querier, project, kind, key, text, source string, verified bool) error {
	_, err := q.Exec(`INSERT INTO memory_records(id,workspace_id,kind,key,text,source,verified,updated_at)
 VALUES(?,NULLIF(?,''),?,?,?,?,?,?) ON CONFLICT DO UPDATE SET text=excluded.text,source=excluded.source,
 verified=excluded.verified,updated_at=excluded.updated_at
 WHERE memory_records.source<>'manual' OR excluded.source='manual' OR excluded.source LIKE 'correction:%'`,
		newID(), project, kind, key, text, source, verified, now())
	return err
}

func (s *Service) getCognitionState(project string) (cognitionState, error) {
	result := cognitionState{Memories: []cognitionMemory{}, Skills: []cognitionSkill{}}
	if project != "" {
		if _, err := s.workspace(project); err != nil {
			return result, err
		}
	}
	var err error
	result.Settings, err = loadCognitionSettings(s.db)
	if err != nil {
		return result, err
	}
	result.Memories, err = s.cognitionMemories(project)
	if err != nil {
		return result, err
	}
	result.MemoryChanges, err = s.cognitionMemoryChanges(project)
	if err != nil {
		return result, err
	}
	result.Skills, err = s.cognitionSkills(project)
	if err != nil {
		return result, err
	}
	result.Candidates, err = s.cognitionCandidates(project)
	if err != nil {
		return result, err
	}
	result.Learning, err = s.cognitionHistory(project)
	if err != nil {
		return result, err
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FILTER(WHERE j.status='pending' AND h.status='queued'),COUNT(*) FILTER(WHERE j.status='paused' AND h.status='queued'),
 COUNT(*) FILTER(WHERE h.status='done') FROM learning_jobs j JOIN agent_tasks t ON t.id=j.task_id
 JOIN cognition_learning_history h ON h.task_id=j.task_id WHERE t.workspace_id=?`, project).
		Scan(&result.Pending, &result.Paused, &result.Completed)
	return result, err
}

func (s *Service) cognitionMemories(project string) ([]cognitionMemory, error) {
	return s.searchCognitionMemories(project, "", true)
}

func cognitionTerms(value string) map[string]bool {
	terms := map[string]bool{}
	var previous rune
	for _, r := range strings.ToLower(value) {
		if unicode.Is(unicode.Han, r) && unicode.Is(unicode.Han, previous) {
			terms[string([]rune{previous, r})] = true
		}
		previous = r
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		switch word {
		case "the", "and", "for", "with", "from", "this", "that", "run", "using", "into", "about", "help":
			continue
		}
		if len(word) > 1 {
			terms[word] = true
		}
	}
	return terms
}

func cognitionScore(query map[string]bool, text string) int {
	hits := 0
	for term := range cognitionTerms(text) {
		if query[term] {
			hits++
		}
	}
	return hits
}
