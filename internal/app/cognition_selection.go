package app

import (
	"database/sql"
	"slices"
	"strings"
)

type cognitionSkillChoice struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
}

type cognitionMatch struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Reason    string `json:"reason"`
	Bytes     int    `json:"bytes"`
	Included  bool   `json:"included"`
}

func (s *Service) setAgentCognitionCorrection(payload string) (AgentTask, error) {
	var in struct {
		ID         string `json:"id"`
		Correction string `json:"correction"`
	}
	if err := decode(payload, &in); err != nil {
		return AgentTask{}, err
	}
	in.Correction = strings.TrimSpace(in.Correction)
	if !cognitionText(in.Correction, 1200) {
		return AgentTask{}, codedError("COGNITION_INVALID_MEMORY")
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	task, err := loadAgentTask(s.db, in.ID)
	if err != nil {
		return task, err
	}
	if task.Agent == nil || task.Status != "NEEDS_REVIEW" {
		return task, codedError("TASK_STATE")
	}
	task.Agent.CognitionCorrection = in.Correction
	task.UpdatedAt = now()
	err = s.transaction(func(tx *sql.Tx) error {
		if err := saveAgentTask(tx, task); err != nil {
			return err
		}
		return audit(tx, "", "COGNITION_TASK_CORRECTED", map[string]string{"id": task.ID})
	})
	if err == nil {
		s.changed("", task.ID)
	}
	return task, err
}

func cognitionChoice(choices []cognitionSkillChoice, id string) string {
	for _, choice := range choices {
		if choice.ID == id {
			return choice.Mode
		}
	}
	return ""
}

func (s *Service) setCognitionChoices(payload string) (AgentTask, error) {
	var in struct {
		ID      string `json:"id"`
		SkillID string `json:"skillId"`
		Mode    string `json:"mode"`
	}
	if err := decode(payload, &in); err != nil {
		return AgentTask{}, err
	}
	if in.ID == "" || in.SkillID == "" || (in.Mode != "include" && in.Mode != "exclude" && in.Mode != "auto") {
		return AgentTask{}, codedError("INVALID_REQUEST")
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	task, err := loadAgentTask(s.db, in.ID)
	if err != nil {
		return task, err
	}
	if task.Agent == nil || task.Status != "NEEDS_REVIEW" {
		return task, codedError("TASK_STATE")
	}
	skills, err := s.cognitionSkills(task.WorkspaceID)
	if err != nil {
		return task, err
	}
	if in.Mode != "auto" && !slices.ContainsFunc(skills, func(skill cognitionSkill) bool {
		return skill.ID == in.SkillID && skill.Enabled
	}) {
		return task, codedError("NOT_FOUND")
	}
	choices := slices.DeleteFunc(task.Agent.CognitionChoices, func(choice cognitionSkillChoice) bool { return choice.ID == in.SkillID })
	if in.Mode != "auto" {
		if len(choices) >= 20 {
			return task, codedError("INVALID_REQUEST")
		}
		choices = append(choices, cognitionSkillChoice{ID: in.SkillID, Mode: in.Mode})
	}
	task.Agent.CognitionChoices = choices
	task.UpdatedAt = now()
	err = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, task) })
	if err == nil {
		s.changed("", task.ID)
	}
	return task, err
}
