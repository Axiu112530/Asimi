package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"asimi/internal/command"
)

type cognitionSkillInput struct {
	Dependencies     []cognitionSkillDependency `json:"dependencies,omitempty"`
	Scope            string                     `json:"scope"`
	WorkspaceID      string                     `json:"workspaceId,omitempty"`
	Slug             string                     `json:"slug"`
	Name             string                     `json:"name"`
	Description      string                     `json:"description"`
	Instructions     string                     `json:"instructions"`
	Triggers         []string                   `json:"triggers"`
	AllowedTools     []string                   `json:"allowedTools"`
	Applicability    string                     `json:"applicability,omitempty"`
	Prerequisites    string                     `json:"prerequisites,omitempty"`
	Execution        []cognitionSkillStep       `json:"execution,omitempty"`
	Workflow         []string                   `json:"workflow,omitempty"`
	ExpectedOutputs  string                     `json:"expectedOutputs,omitempty"`
	Validation       string                     `json:"validation,omitempty"`
	Limitations      string                     `json:"limitations,omitempty"`
	SourceExperience string                     `json:"sourceExperience,omitempty"`
}

type cognitionSkill struct {
	cognitionSkillInput
	ID            string         `json:"id"`
	Namespace     string         `json:"namespace"`
	Source        string         `json:"source"`
	Enabled       bool           `json:"enabled"`
	Version       int            `json:"version"`
	Checksum      string         `json:"checksum"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
	Usage         int            `json:"usage"`
	Status        string         `json:"status,omitempty"`
	CompletedUses int            `json:"completedUses,omitempty"`
	FailedUses    int            `json:"failedUses,omitempty"`
	InputSchema   map[string]any `json:"inputSchema"`
	OutputSchema  map[string]any `json:"outputSchema"`
}

type cognitionSkillVersion struct {
	Version   int                 `json:"version"`
	Manifest  cognitionSkillInput `json:"manifest"`
	Checksum  string              `json:"checksum"`
	CreatedAt string              `json:"createdAt"`
}

var cognitionSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func validCognitionScope(scope, project, slug string) bool {
	return cognitionSlug.MatchString(slug) && ((scope == "user" && project == "") || (scope == "project" && project != ""))
}

func normalizeCognitionSkill(in *cognitionSkillInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Instructions = strings.TrimSpace(in.Instructions)
	if !validCognitionScope(in.Scope, in.WorkspaceID, in.Slug) ||
		!cognitionText(in.Name, 100) || !cognitionText(in.Description, 320) || !cognitionText(in.Instructions, 3600) ||
		strings.Contains(in.Instructions, "```") || len(in.Triggers) > 12 || len(in.AllowedTools) > 12 {
		return codedError("COGNITION_INVALID_SKILL")
	}
	if err := validateCognitionSkillText(*in); err != nil {
		return err
	}
	in.Triggers = slices.Clone(in.Triggers)
	in.AllowedTools = slices.Clone(in.AllowedTools)
	for i, trigger := range in.Triggers {
		in.Triggers[i] = strings.TrimSpace(trigger)
		if !cognitionText(in.Triggers[i], 100) {
			return codedError("COGNITION_INVALID_SKILL")
		}
	}
	for _, tool := range in.AllowedTools {
		if !cognitionRegisteredTool(tool) {
			return codedError("COGNITION_UNKNOWN_TOOL")
		}
	}
	slices.Sort(in.AllowedTools)
	in.AllowedTools = slices.Compact(in.AllowedTools)
	return validateCognitionSteps(*in)
}

func cognitionRegisteredTool(tool string) bool {
	for _, skill := range builtinSkills() {
		if slices.Contains(skill.Tools, tool) {
			return true
		}
	}
	for _, spec := range command.Catalog() {
		if spec.ID == tool && spec.AgentAllowed && !spec.RequiresDownload {
			return true
		}
	}
	return false
}

func (s *Service) manageCognitionSkill(method, payload string) (any, error) {
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	if method == "ImportCognitionSkill" {
		return s.importCognitionSkill(payload)
	}
	if method == "SaveCognitionSkill" {
		var in cognitionSkillInput
		if err := decode(payload, &in); err != nil {
			return nil, err
		}
		if err := normalizeCognitionSkill(&in); err != nil {
			return nil, err
		}
		if in.WorkspaceID != "" {
			if _, err := s.workspace(in.WorkspaceID); err != nil {
				return nil, err
			}
		}
		err := s.transaction(func(tx *sql.Tx) error {
			return saveCognitionSkill(tx, in, "manual")
		})
		return err == nil, err
	}
	var in struct {
		Scope       string `json:"scope"`
		WorkspaceID string `json:"workspaceId,omitempty"`
		Slug        string `json:"slug"`
		Operation   string `json:"operation,omitempty"`
		Version     int    `json:"version,omitempty"`
	}
	if err := decode(payload, &in); err != nil {
		return nil, err
	}
	if !validCognitionScope(in.Scope, in.WorkspaceID, in.Slug) {
		return nil, codedError("COGNITION_INVALID_SKILL")
	}
	if method == "ExportCognitionSkill" {
		return exportCognitionSkill(s.db, in.WorkspaceID, in.Slug)
	}
	var id string
	err := s.db.QueryRow("SELECT id FROM skill_packages WHERE COALESCE(workspace_id,'')=? AND slug=?", in.WorkspaceID, in.Slug).Scan(&id)
	if err != nil {
		return nil, mvpNotFound(err)
	}
	if method == "ListCognitionSkillVersions" {
		return s.cognitionSkillVersions(id)
	}
	err = s.transaction(func(tx *sql.Tx) error {
		var err error
		switch in.Operation {
		case "enable", "disable", "deprecate", "archive":
			if _, err = tx.Exec("UPDATE skill_packages SET enabled=?,updated_at=? WHERE id=?", in.Operation == "enable", now(), id); err != nil {
				return err
			}
			switch in.Operation {
			case "deprecate", "archive":
				status := "deprecated"
				if in.Operation == "archive" {
					status = "archived"
				}
				_, err = tx.Exec("INSERT INTO cognition_skill_state(package_id,status) VALUES(?,?) ON CONFLICT(package_id) DO UPDATE SET status=excluded.status", id, status)
			default:
				_, err = tx.Exec("DELETE FROM cognition_skill_state WHERE package_id=?", id)
			}
		case "restore":
			r, e := tx.Exec(`UPDATE skill_packages SET version=?,source='manual',updated_at=? WHERE id=? AND
   EXISTS(SELECT 1 FROM skill_versions WHERE package_id=? AND version=?)`, in.Version, now(), id, id, in.Version)
			err = cognitionAffected(r, e)
		case "delete":
			_, err = tx.Exec("DELETE FROM skill_packages WHERE id=?", id)
		default:
			return codedError("INVALID_REQUEST")
		}
		if err != nil {
			return err
		}
		return audit(tx, "", "COGNITION_SKILL_CHANGED", map[string]string{"id": id, "operation": in.Operation})
	})
	return err == nil, err
}

func saveCognitionSkill(tx *sql.Tx, in cognitionSkillInput, source string) error {
	if err := validateCognitionDependencies(tx, in); err != nil {
		return err
	}
	manifest := encode(in)
	checksum := cognitionChecksum(manifest)
	var id, currentSource, currentHash string
	var version int
	err := tx.QueryRow(`SELECT p.id,p.source,v.checksum FROM skill_packages p JOIN skill_versions v
 ON v.package_id=p.id AND v.version=p.version WHERE COALESCE(p.workspace_id,'')=? AND p.slug=?`, in.WorkspaceID, in.Slug).Scan(&id, &currentSource, &currentHash)
	stamp := now()
	if errors.Is(err, sql.ErrNoRows) {
		id = newID()
		version = 1
		_, err = tx.Exec("INSERT INTO skill_packages(id,workspace_id,slug,source,enabled,version,created_at,updated_at) VALUES(?,NULLIF(?,''),?,?,1,?,?,?)",
			id, in.WorkspaceID, in.Slug, source, version, stamp, stamp)
	} else if err == nil {
		if (source != "manual" && !strings.HasPrefix(source, "reviewed:")) || (currentHash == checksum && currentSource == source) {
			return nil
		}
		if err = tx.QueryRow("SELECT MAX(version)+1 FROM skill_versions WHERE package_id=?", id).Scan(&version); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE skill_packages SET source=?,version=?,updated_at=? WHERE id=?", source, version, stamp, id)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO skill_versions(package_id,version,manifest,checksum,created_at) VALUES(?,?,?,?,?)", id, version, manifest, checksum, stamp); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO cognition_package_writes(package_id,version,manifest,checksum,deleted,updated_at) VALUES(?,?,?,?,0,?)", id, version, manifest, checksum, stamp); err != nil {
		return err
	}
	return audit(tx, "", "COGNITION_SKILL_SAVED", map[string]string{"id": id, "source": source})
}

func cognitionChecksum(manifest string) string {
	digest := sha256.Sum256([]byte(manifest))
	return hex.EncodeToString(digest[:])
}

func (s *Service) cognitionSkillVersions(id string) ([]cognitionSkillVersion, error) {
	rows, err := s.db.Query("SELECT version,manifest,checksum,created_at FROM skill_versions WHERE package_id=? ORDER BY version DESC LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []cognitionSkillVersion{}
	for rows.Next() {
		var item cognitionSkillVersion
		var raw string
		if err := rows.Scan(&item.Version, &raw, &item.Checksum, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Manifest); err != nil {
			return nil, err
		}
		versions = append(versions, item)
	}
	return versions, rows.Err()
}

func (s *Service) cognitionSkills(project string) ([]cognitionSkill, error) {
	rows, err := s.db.Query(`SELECT p.id,p.source,p.enabled,p.version,p.created_at,p.updated_at,v.manifest,v.checksum,
 0,
 COALESCE((SELECT status FROM cognition_skill_state x WHERE x.package_id=p.id), CASE WHEN p.enabled=1 THEN 'active' ELSE 'disabled' END),
 0,0 FROM skill_packages p JOIN skill_versions v
 ON v.package_id=p.id AND v.version=p.version WHERE p.workspace_id IS NULL OR p.workspace_id=?
 ORDER BY p.workspace_id IS NULL,p.updated_at DESC,p.id LIMIT 100`, project)
	if err != nil {
		return nil, err
	}
	items := []cognitionSkill{}
	for rows.Next() {
		var item cognitionSkill
		var raw string
		if err = rows.Scan(&item.ID, &item.Source, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt, &raw, &item.Checksum, &item.Usage, &item.Status, &item.CompletedUses, &item.FailedUses); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &item.cognitionSkillInput); err != nil {
			break
		}
		item.Namespace = item.Scope + ":" + item.Slug
		item.InputSchema = objectSchema(map[string]any{"goal": stringSchema()}, "goal")
		item.OutputSchema = objectSchema(map[string]any{"summary": stringSchema()}, "summary")
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	insights, err := s.evolutionInsights(project)
	if err != nil {
		return nil, err
	}
	metrics := map[string]evolutionSkillMetric{}
	for _, m := range insights.Skills {
		metrics[evolutionKey(m.ID, m.Version)] = m
	}
	for i := range items {
		m := metrics[evolutionKey(items[i].ID, items[i].Version)]
		items[i].Usage = m.ContextUses
		items[i].CompletedUses = m.Successes
		items[i].FailedUses = m.Failures
	}
	for _, skill := range builtinSkills() {
		item := cognitionSkill{cognitionSkillInput: cognitionSkillInput{Scope: "official", Slug: skill.ID, Name: skill.Name,
			Description: skill.Description, Instructions: skill.Description, AllowedTools: skill.Tools},
			ID: skill.ID, Namespace: "official:" + skill.ID, Source: "official", Version: 1, InputSchema: skill.InputSchema, OutputSchema: skill.OutputSchema}
		if err := s.db.QueryRow("SELECT enabled FROM skill_settings WHERE id=?", skill.ID).Scan(&item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
