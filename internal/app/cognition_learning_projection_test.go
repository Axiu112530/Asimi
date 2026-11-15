package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Read the asynchronous disk projection independently of the authoritative DB.
func awaitLearnedSkillProjection(t *testing.T, s *Service, workspaceID, slug, sourceTaskID string) (cognitionSkillPackage, []byte, []byte) {
	t.Helper()
	var packageID string
	if err := s.db.QueryRow("SELECT id FROM skill_packages WHERE workspace_id=? AND slug=? AND version=1 AND enabled=1", workspaceID, slug).Scan(&packageID); err != nil {
		t.Fatal("reviewed candidate did not become an enabled versioned skill", err)
	}
	var projected cognitionSkillPackage
	var markdown, manifest []byte
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		var markdownErr, manifestErr error
		packageRoot := filepath.Join(s.dataDir, "skills", packageID+"-v1")
		markdown, markdownErr = os.ReadFile(filepath.Join(packageRoot, "SKILL.md"))
		manifest, manifestErr = os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
		if markdownErr == nil && manifestErr == nil && json.Unmarshal(manifest, &projected) == nil && string(markdown) == projected.SkillMarkdown {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if projected.Version != 1 || projected.Manifest.SourceExperience != sourceTaskID || projected.Manifest.Slug != slug || projected.Checksum != cognitionChecksum(encode(projected.Manifest)) || len(markdown) == 0 || string(markdown) != projected.SkillMarkdown {
		t.Fatal("reviewed learned skill was not independently projected to SKILL.md and manifest.json")
	}
	return projected, markdown, manifest
}
