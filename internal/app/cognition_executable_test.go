package app

import (
	"database/sql"
	"testing"
)

func TestCognitionExecutableSkillRechecksApprovalAndAllowedTool(t *testing.T) {
	s, dir, _ := fixture(t)
	w := mvpWorkspace(t, s, dir)
	manifest := cognitionSkillInput{Scope: "project", WorkspaceID: w.ID, Slug: "approved-go-check", Name: "Go check",
		Description: "Check the modified package.", Instructions: "Use the registered test.",
		Execution: []cognitionSkillStep{{Tool: "go.test", Target: "changed-package"}}}
	if err := normalizeCognitionSkill(&manifest); errorCode(err) != "COGNITION_INVALID_SKILL" {
		t.Fatalf("accepted a declaration outside AllowedTools: %v", err)
	}
	manifest.AllowedTools = []string{"go.test"}
	if err := normalizeCognitionSkill(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := s.transaction(func(tx *sql.Tx) error { return saveCognitionSkill(tx, manifest, "reviewed:fixture") }); err != nil {
		t.Fatal(err)
	}
	var dispatch agentSkillDispatch
	dispatch.Tool = "go.test"
	if err := s.db.QueryRow("SELECT p.id,p.version,v.checksum FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id AND v.version=p.version WHERE p.slug=?", manifest.Slug).Scan(&dispatch.ID, &dispatch.Version, &dispatch.Checksum); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentSkillDispatch(s.db, w.ID, &dispatch); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentSkillDispatch(s.db, "another-workspace", &dispatch); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("wrong workspace accepted: %v", err)
	}
	mustCall(t, s, "ChangeCognitionSkill", map[string]string{"scope": "project", "workspaceId": w.ID, "slug": manifest.Slug, "operation": "disable"})
	if err := validateAgentSkillDispatch(s.db, w.ID, &dispatch); errorCode(err) != "COGNITION_REVIEW_STALE" {
		t.Fatalf("disabled skill accepted: %v", err)
	}
}
