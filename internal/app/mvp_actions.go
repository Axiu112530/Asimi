package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"asimi/internal/command"
	"asimi/internal/developer"
)

// The permit binds every input, including workspace, task, limits and expiry.
// Status/result are journal state, never part of a mutable execution proposal.
func productHash(a ProductAction) string {
	b := encode(struct {
		ID, TaskID, Kind, CreatedAt, ExpiresAt string
		Proposal                               ProductProposal
	}{a.ID, a.TaskID, a.Kind, a.CreatedAt, a.ExpiresAt, a.Proposal})
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}
func (s *Service) prepareProduct(workspaceID, taskID, kind, path, content, commandID string, args []string, network bool, exclusions ...[]string) (ProductAction, error) {
	s.mvp.mu.Lock()
	epoch := s.mvpGeneration()
	w, e := s.workspace(workspaceID)
	s.mvp.mu.Unlock()
	if e != nil {
		return ProductAction{}, e
	}
	p := ProductProposal{WorkspaceID: w.ID, Root: w.Root}
	if _, e = developer.ValidateRoot(w.Root); e != nil {
		return ProductAction{}, e
	}
	switch kind {
	case "file":
		options := developer.Options{}
		if len(exclusions) > 0 {
			options.Exclusions = exclusions[0]
		}
		v, err := developer.PrepareWithOptions(w.Root, path, content, options)
		if err != nil {
			return ProductAction{}, err
		}
		p.File = &v
		recordGitFileContext(&p)
	case "command":
		v, err := command.Prepare(commandID, args, w.Root, network)
		if err != nil {
			return ProductAction{}, err
		}
		p.Command = &v
	default:
		return ProductAction{}, codedError("INVALID_REQUEST")
	}
	a := ProductAction{ID: newID(), TaskID: taskID, Kind: kind, Status: "PENDING", CreatedAt: now(), UpdatedAt: now(), ExpiresAt: time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano), Proposal: p}
	a.Hash = productHash(a)
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	if !s.mvpAdmission(epoch, taskID) {
		return ProductAction{}, codedError("ACTION_CANCELLED")
	}
	// Recheck existence and linkage after preparation; no I/O occurs in this transaction.
	e = s.transaction(func(tx *sql.Tx) error {
		if taskID != "" {
			t, err := loadAgentTask(tx, taskID)
			if err != nil {
				return err
			}
			if t.Agent != nil || t.WorkspaceID != w.ID || t.Status == "RUNNING" || t.Status == "WAITING_APPROVAL" || t.Status == "PAUSED" || t.Status == "CANCELLED" {
				return codedError("TASK_STATE")
			}
			t.Steps = append(t.Steps, AgentStep{ID: newID(), Kind: kind, Title: "APPROVE_" + kind, Status: "WAITING_APPROVAL", ActionID: a.ID})
			t.CurrentStep = len(t.Steps) - 1
			t.Status = "WAITING_APPROVAL"
			t.UpdatedAt = now()
			if err = saveAgentTask(tx, t); err != nil {
				return err
			}
		}
		var tid any
		if taskID != "" {
			tid = taskID
		}
		return savePreparedProduct(tx, a, tid)
	})
	if e == nil {
		s.mvp.immutable[a.ID] = encode(a)
		s.changed("", taskID)
	}
	return a, e
}

func recordGitFileContext(proposal *ProductProposal) {
	git, err := developer.ReadGitContext(proposal.Root)
	if err != nil {
		proposal.GitStatus = "ERROR"
		return
	}
	proposal.GitStatus = git.Status
	proposal.GitDirtyFile = git.DirtyPath(proposal.File.RelativePath)
	if git.Truncated {
		proposal.GitStatus = "PARTIAL"
	}
}

func savePreparedProduct(tx *sql.Tx, a ProductAction, task any) error {
	if _, err := tx.Exec("INSERT INTO product_actions(id,workspace_id,task_id,kind,status,proposal_json,hash,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)", a.ID, a.Proposal.WorkspaceID, task, a.Kind, a.Status, encode(a.Proposal), a.Hash, a.ExpiresAt, a.CreatedAt, a.UpdatedAt); err != nil {
		return err
	}
	return productEvent(tx, a, "PREPARED")
}

// finishPendingAction is called with the MVP lock held. Consumed actions are
// deliberately ineligible: Stop, reject and expiry cannot make them replayable.
func (s *Service) finishPendingAction(id, status, code string) (ProductAction, error) {
	a, e := loadProductAction(s.db, id)
	if e != nil {
		return a, e
	}
	if a.Status != "PENDING" {
		return a, codedError("ACTION_STATE")
	}
	a.Status = status
	a.Error = code
	a.UpdatedAt = now()
	e = s.transaction(func(tx *sql.Tx) error {
		r, err := tx.Exec("UPDATE product_actions SET status=?,error_code=?,updated_at=? WHERE id=? AND status='PENDING' AND consumed=0", status, code, a.UpdatedAt, id)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return codedError("ACTION_STATE")
		}
		if err = productEvent(tx, a, status); err != nil {
			return err
		}
		return finishAgentAction(tx, a)
	})
	if e == nil {
		delete(s.mvp.immutable, id)
		s.changed("", a.TaskID)
	}
	return a, e
}

func finishAgentAction(q querier, a ProductAction) error {
	if a.TaskID == "" {
		return nil
	}
	t, e := loadAgentTask(q, a.TaskID)
	if e != nil {
		return e
	}
	for i := range t.Steps {
		if t.Steps[i].ActionID == a.ID {
			t.Steps[i].Status = a.Status
			t.Steps[i].Result = a.Result
			if a.Status == "SUCCEEDED" {
				t.Steps[i].Status = "COMPLETED"
			}
		}
	}
	if t.Status != "CANCELLED" && t.Status != "PAUSED" {
		t.Status = "FAILED"
		if a.Status == "SUCCEEDED" {
			t.Status = "COMPLETED"
		}
		if a.Status == "EXECUTING" {
			t.Status = "RUNNING"
		}
		if a.Status == "RECOVERY_REQUIRED" {
			t.Status = "PAUSED"
		}
	}
	t.Summary = a.Error
	if a.Status == "SUCCEEDED" {
		t.Summary = "ACTION_VERIFIED"
	}
	if t.Agent != nil && t.Status != "PAUSED" && t.Status != "CANCELLED" && a.Status != "EXECUTING" {
		t.Status = "NEEDS_REVIEW"
	}
	t.UpdatedAt = now()
	return saveAgentTask(q, t)
}

func (s *Service) approveProduct(id, hash string, networkConsent ...bool) (ProductAction, error) {
	s.mvp.mu.Lock()
	a, e := loadProductAction(s.db, id)
	if e != nil {
		s.mvp.mu.Unlock()
		return a, e
	}
	var immutable ProductAction
	raw, ok := s.mvp.immutable[id]
	if !ok || json.Unmarshal([]byte(raw), &immutable) != nil || encode(a.Proposal) != encode(immutable.Proposal) || productHash(a) != a.Hash || a.Hash != hash || immutable.Hash != hash {
		s.mvp.mu.Unlock()
		return a, codedError("APPROVAL_MISMATCH")
	}
	if a.Status != "PENDING" {
		s.mvp.mu.Unlock()
		return a, codedError("ACTION_STATE")
	}
	if a.Proposal.PolicyVersion == agent02Policy && a.Proposal.Command != nil && a.Proposal.Command.Network && (len(networkConsent) != 1 || !networkConsent[0]) {
		s.mvp.mu.Unlock()
		return a, codedError("COMMAND_NETWORK_CONSENT")
	}
	if !validProductExpiry(a) {
		a, e = s.finishPendingAction(id, "EXPIRED", "APPROVAL_EXPIRED")
		s.mvp.mu.Unlock()
		if e == nil {
			e = codedError("APPROVAL_EXPIRED")
		}
		return a, e
	}
	ctx, cancel := context.WithCancel(s.lifetime)
	epoch := s.mvpGeneration()
	if ctx.Err() != nil || !s.mvpAdmission(epoch, a.TaskID) {
		cancel()
		s.mvp.mu.Unlock()
		return a, codedError("ACTION_CANCELLED")
	}
	a.Status = "EXECUTING"
	a.UpdatedAt = now()
	e = s.transaction(func(tx *sql.Tx) error {
		// Exact hash, expiry, single CAS consumption and durable intent are one commit.
		r, err := tx.Exec("UPDATE product_actions SET status='EXECUTING',consumed=1,updated_at=? WHERE id=? AND status='PENDING' AND consumed=0 AND hash=? AND expires_at>?", a.UpdatedAt, a.ID, hash, now())
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return codedError("ACTION_STATE")
		}
		if a.TaskID != "" {
			t, err := loadAgentTask(tx, a.TaskID)
			if err != nil {
				return err
			}
			if t.Status != "WAITING_APPROVAL" {
				return codedError("TASK_STATE")
			}
			if t.Agent != nil {
				version := a.Proposal.PlanVersion
				if a.Proposal.PolicyVersion != agent02Policy || version != len(t.Agent.Rounds) || version < 1 || t.Agent.Rounds[version-1].ActionID != a.ID || t.CurrentStep < 0 || t.CurrentStep >= len(t.Steps) || t.Steps[t.CurrentStep].ID != a.Proposal.StepID || t.Steps[t.CurrentStep].ActionID != a.ID {
					return codedError("APPROVAL_MISMATCH")
				}
			}
		}
		if _, err = tx.Exec("INSERT INTO product_action_intents(action_id,proposal_json,hash,created_at,updated_at) VALUES(?,?,?,?,?)", a.ID, encode(a.Proposal), hash, now(), now()); err != nil {
			return err
		}
		if err = productEvent(tx, a, "CONSUMED"); err != nil {
			return err
		}
		return finishAgentAction(tx, a)
	})
	if e != nil {
		cancel()
		s.mvp.mu.Unlock()
		return a, e
	}
	delete(s.mvp.immutable, id)
	s.registerMVPControl("action:"+id, cancel, epoch, a.TaskID)
	if a.TaskID != "" {
		s.registerMVPControl("effect:"+a.TaskID, cancel, epoch, a.TaskID)
	}
	s.mvp.mu.Unlock()
	s.changed("", a.TaskID)
	// Effect I/O starts only after the intent commit and never while holding a DB transaction.
	var outcome any
	var effectErr error
	if ctx.Err() != nil {
		effectErr = codedError("ACTION_CANCELLED")
	} else if _, effectErr = developer.ValidateRoot(a.Proposal.Root); effectErr == nil {
		switch a.Kind {
		case "file":
			if a.Proposal.File == nil {
				effectErr = codedError("APPROVAL_MISMATCH")
				break
			}
			v, err := developer.Apply(ctx, a.Proposal.Root, *a.Proposal.File)
			outcome = v
			effectErr = err
			if err == nil && (!v.Applied || !v.Verified) {
				effectErr = codedError("VERIFICATION_FAILED")
			}
			if effectErr == nil {
				witness, readErr := developer.ReadWithOptions(a.Proposal.Root, a.Proposal.File.RelativePath, developer.Options{Exclusions: a.Proposal.File.Exclusions})
				if readErr != nil || witness.Hash != a.Proposal.File.AfterHash || witness.Content != a.Proposal.File.AfterContent || witness.Identity != v.Identity {
					effectErr = codedError("UNKNOWN_RESULT")
				}
			}
		case "command":
			if a.Proposal.Command == nil {
				effectErr = codedError("APPROVAL_MISMATCH")
				break
			}
			v, err := command.Run(ctx, *a.Proposal.Command)
			outcome = v
			effectErr = err
			if err == nil && v.ExitCode != 0 {
				effectErr = codedError("COMMAND_FAILED")
			}
		default:
			effectErr = codedError("APPROVAL_MISMATCH")
		}
	}
	a.Result = json.RawMessage(encode(outcome))
	a.Status = "SUCCEEDED"
	if effectErr != nil {
		a.Status = "FAILED"
		a.Error = errorCode(effectErr)
		if ctx.Err() != nil {
			a.Status = "CANCELLED"
		}
		if a.Error == "UNKNOWN_RESULT" || a.Error == "CONTROL_UNKNOWN_RESULT" || a.Error == "FILE_RECOVERY_REQUIRED" {
			a.Status = "RECOVERY_REQUIRED"
		}
	}
	a.UpdatedAt = now()
	s.mvp.mu.Lock()
	s.removeMVPControl("action:" + id)
	if a.TaskID != "" {
		s.removeMVPControl("effect:" + a.TaskID)
	}
	e = s.transaction(func(tx *sql.Tx) error {
		r, err := tx.Exec("UPDATE product_actions SET status=?,result_json=?,error_code=?,updated_at=? WHERE id=? AND status='EXECUTING' AND consumed=1", a.Status, string(a.Result), a.Error, a.UpdatedAt, a.ID)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return codedError("ACTION_STATE")
		}
		if _, err = tx.Exec("UPDATE product_action_intents SET settled=1,updated_at=? WHERE action_id=? AND settled=0", now(), id); err != nil {
			return err
		}
		if err = productEvent(tx, a, "VERIFIED_"+a.Status); err != nil {
			return err
		}
		return finishAgentAction(tx, a)
	})
	if e != nil {
		s.mvp.faults[id] = true
		s.mvp.faultErr = errors.Join(s.mvp.faultErr, e)
		a.Status = "RECOVERY_REQUIRED"
		a.Error = "OUTCOME_NOT_SAVED"
	}
	s.mvp.mu.Unlock()
	s.changed("", a.TaskID)
	// A failed execution is a durable reviewable action result, while failed
	// persistence is a bridge error and must never be reported as saved success.
	return a, e
}
