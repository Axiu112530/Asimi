package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type cognitionSkillPackage struct {
	Manifest      cognitionSkillInput `json:"manifest"`
	Version       int                 `json:"version"`
	Checksum      string              `json:"checksum"`
	SkillMarkdown string              `json:"skillMarkdown"`
}

func newCognitionSkillPackage(in cognitionSkillInput, version int, checksum string) cognitionSkillPackage {
	var markdown strings.Builder
	markdown.WriteString("# " + strings.ReplaceAll(in.Name, "\n", " ") + "\n\n" + in.Description + "\n\n" + in.Instructions + "\n")
	for _, section := range []struct{ title, text string }{
		{"Applicability", in.Applicability}, {"Prerequisites", in.Prerequisites}, {"Workflow", strings.Join(in.Workflow, "\n- ")},
		{"Expected outputs", in.ExpectedOutputs}, {"Validation", in.Validation}, {"Known limitations", in.Limitations}, {"Source experience", in.SourceExperience},
	} {
		if section.text != "" {
			markdown.WriteString("\n## " + section.title + "\n\n")
			if section.title == "Workflow" {
				markdown.WriteString("- ")
			}
			markdown.WriteString(section.text + "\n")
		}
	}
	return cognitionSkillPackage{Manifest: in, Version: version, Checksum: checksum, SkillMarkdown: markdown.String()}
}

func exportCognitionSkill(q querier, project, slug string) (cognitionSkillPackage, error) {
	var raw string
	var version int
	err := q.QueryRow(`SELECT v.manifest,v.version FROM skill_packages p JOIN skill_versions v
 ON v.package_id=p.id AND v.version=p.version WHERE COALESCE(p.workspace_id,'')=? AND p.slug=?`, project, slug).
		Scan(&raw, &version)
	if err != nil {
		return cognitionSkillPackage{}, mvpNotFound(err)
	}
	var in cognitionSkillInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return cognitionSkillPackage{}, err
	}
	// A portable package always requires a destination scope on import.
	in.WorkspaceID = ""
	in.SourceExperience = ""
	return newCognitionSkillPackage(in, version, cognitionChecksum(encode(in))), nil
}

func (s *Service) importCognitionSkill(payload string) (any, error) {
	var in struct {
		Scope       string                `json:"scope"`
		WorkspaceID string                `json:"workspaceId,omitempty"`
		Package     cognitionSkillPackage `json:"package"`
	}
	if err := decode(payload, &in); err != nil {
		return nil, err
	}
	if in.Package.Version < 1 || in.Package.Checksum != cognitionChecksum(encode(in.Package.Manifest)) {
		return nil, codedError("COGNITION_INVALID_SKILL")
	}
	skill := in.Package.Manifest
	skill.SourceExperience = ""
	skill.Scope = in.Scope
	skill.WorkspaceID = in.WorkspaceID
	if err := normalizeCognitionSkill(&skill); err != nil {
		return nil, err
	}
	if skill.WorkspaceID != "" {
		if _, err := s.workspace(skill.WorkspaceID); err != nil {
			return nil, err
		}
	}
	err := s.transaction(func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM skill_packages WHERE COALESCE(workspace_id,'')=? AND slug=?)", skill.WorkspaceID, skill.Slug).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return codedError("COGNITION_SKILL_EXISTS")
		}
		return saveCognitionSkill(tx, skill, "imported")
	})
	return err == nil, err
}

// The projection uses generated package IDs and immutable versions. SQLite is
// authoritative and retains enough information to retry a partial write.
func (s *Service) syncCognitionPackage() {
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	if s.lifetime.Err() != nil {
		return
	}
	var id, raw, checksum string
	var version int
	var deleted bool
	err := s.db.QueryRow(`SELECT package_id,version,manifest,checksum,deleted FROM cognition_package_writes
 ORDER BY updated_at,package_id,version LIMIT 1`).Scan(&id, &version, &raw, &checksum, &deleted)
	if err != nil {
		return
	}
	err = s.writeCognitionPackage(id, version, raw, checksum, deleted)
	if err != nil {
		_, _ = s.db.Exec("UPDATE cognition_package_writes SET updated_at=? WHERE package_id=? AND version=?", now(), id, version)
		return
	}
	_, _ = s.db.Exec("DELETE FROM cognition_package_writes WHERE package_id=? AND version=?", id, version)
}

func (s *Service) writeCognitionPackage(id string, version int, raw, checksum string, deleted bool) error {
	if !validAgentSHA(id+id) || version < 1 {
		return codedError("COGNITION_INVALID_SKILL")
	}
	base := filepath.Join(s.dataDir, "skills")
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return codedError("COGNITION_INVALID_SKILL")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := fmt.Sprintf("%s-v%d", id, version)
	if deleted {
		for _, file := range []string{"SKILL.md", "manifest.json"} {
			if err := root.Remove(filepath.Join(dir, file)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := root.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var in cognitionSkillInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return err
	}
	if cognitionChecksum(raw) != checksum {
		return codedError("COGNITION_INVALID_SKILL")
	}
	if err := root.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err = root.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return codedError("COGNITION_INVALID_SKILL")
	}
	packageFile := newCognitionSkillPackage(in, version, checksum)
	manifest, err := json.MarshalIndent(packageFile, "", "  ")
	if err != nil {
		return err
	}
	for _, file := range []struct {
		name    string
		content []byte
	}{
		{"SKILL.md", []byte(packageFile.SkillMarkdown)},
		{"manifest.json", append(manifest, '\n')},
	} {
		path := filepath.Join(dir, file.name)
		temp := filepath.Join(dir, "."+newID()+".tmp")
		handle, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := handle.Write(file.content)
		if err = errors.Join(writeErr, handle.Sync(), handle.Close()); err == nil {
			err = root.Rename(temp, path)
		}
		if err != nil {
			_ = root.Remove(temp)
			return err
		}
	}
	return nil
}
