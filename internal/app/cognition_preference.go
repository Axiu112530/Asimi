package app

import (
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

const cognitionPreferenceSchema = `
CREATE TABLE IF NOT EXISTS cognition_preference_reviews(
 id INTEGER PRIMARY KEY, memory_id TEXT NOT NULL REFERENCES memory_records(id) ON DELETE CASCADE,
 previous TEXT NOT NULL, current TEXT NOT NULL, source TEXT NOT NULL, base_updated_at TEXT NOT NULL,
 created_at TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','rejected')),
 UNIQUE(memory_id,source)
);
CREATE INDEX IF NOT EXISTS cognition_preference_pending ON cognition_preference_reviews(status,memory_id,id);
`

var naturalPreference = regexp.MustCompile(`(?i)^(?:i (?:usually prefer|prefer|would rather)|我(?:通常|平时|更)?(?:喜欢|倾向于|偏好))\s*(.+)$`)

type cognitionConcept struct {
	token   string
	phrases []string
}

var cognitionConcepts = []cognitionConcept{
	{"focusedchanges", []string{"focused patch", "small patch", "minimal change", "small change", "incremental change", "keep this patch focused", "最小范围", "小步修复", "局部修改", "只改一处"}},
	{"analyzefirst", []string{"analyze first", "diagnose first", "inspect first", "先分析", "先排查", "先定位"}},
	{"focusedtests", []string{"targeted test", "focused test", "relevant test", "regression test", "定向测试", "相关测试", "回归测试"}},
}

func cognitionConceptMatches(text string) []cognitionConcept {
	text = strings.ToLower(text)
	var matched []cognitionConcept
	for _, concept := range cognitionConcepts {
		for _, phrase := range concept.phrases {
			if strings.Contains(text, phrase) {
				matched = append(matched, concept)
				break
			}
		}
	}
	return matched
}

func captureNaturalPreference(q querier, messageID, text string) error {
	if naturalPreference.FindStringSubmatch(text) == nil || !cognitionText(text, 480) {
		return nil
	}
	// Keep broad conversation statements out of persistent memory. These
	// categories describe stable development preferences and are local only.
	var key string
	concepts := cognitionConceptMatches(text)
	if len(concepts) > 0 {
		key = concepts[0].token
	} else {
		return nil
	}
	var id, previous, updated string
	err := q.QueryRow(`SELECT id,text,updated_at FROM memory_records
		WHERE workspace_id IS NULL AND kind='user' AND key=?`, key).Scan(&id, &previous, &updated)
	source := "preference:" + messageID
	if errors.Is(err, sql.ErrNoRows) {
		return saveCognitionMemory(q, "", "user", key, text, source, false)
	}
	if err != nil || previous == text {
		return err
	}
	_, err = q.Exec(`INSERT OR IGNORE INTO cognition_preference_reviews
		(memory_id,previous,current,source,base_updated_at,created_at) VALUES(?,?,?,?,?,?)`,
		id, previous, text, source, updated, now())
	return err
}

func (s *Service) resolveCognitionMemoryChange(payload string) error {
	var in struct {
		ID       int64  `json:"id"`
		Decision string `json:"decision"`
		Pending  bool   `json:"pending"`
	}
	if err := decode(payload, &in); err != nil {
		return err
	}
	if in.ID <= 0 || !in.Pending || (in.Decision != "accept" && in.Decision != "keep") {
		return codedError("INVALID_REQUEST")
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	return s.transaction(func(tx *sql.Tx) error {
		var id, previous, current, source, updated string
		err := tx.QueryRow(`SELECT p.memory_id,p.previous,p.current,p.source,p.base_updated_at
			FROM cognition_preference_reviews p JOIN memory_records m ON m.id=p.memory_id
			WHERE p.id=? AND p.status='pending' AND m.workspace_id IS NULL`, in.ID).
			Scan(&id, &previous, &current, &source, &updated)
		if err != nil {
			return mvpNotFound(err)
		}
		status := "rejected"
		if in.Decision == "accept" {
			result, err := tx.Exec(`UPDATE memory_records SET text=?,source=?,verified=1,updated_at=?
				WHERE id=? AND text=? AND updated_at=?`, current, source, now(), id, previous, updated)
			if err := cognitionAffected(result, err); err != nil {
				if errorCode(err) == "NOT_FOUND" {
					return codedError("COGNITION_REVIEW_STALE")
				}
				return err
			}
			status = "accepted"
		}
		_, err = tx.Exec("UPDATE cognition_preference_reviews SET status=? WHERE id=?", status, in.ID)
		return err
	})
}
