package app

import "asimi/internal/domain"

func (s *Service) securityStatus(payload string) domain.Reply {
	if err := decode(payload, &struct{}{}); err != nil {
		return fail(errorCode(err))
	}
	s.mu.Lock()
	err := s.expireLocked()
	s.mu.Unlock()
	if err != nil {
		return fail(errorCode(err))
	}

	s.mvp.mu.Lock()
	err = s.expireMVP()
	s.mvp.mu.Unlock()
	if err != nil {
		return fail(errorCode(err))
	}
	control, _ := s.callControl("GetControlStatus", "{}")
	if control.Error != nil {
		return control
	}
	available, _ := control.Data.(map[string]any)["available"].(bool)
	stopped, _ := control.Data.(map[string]any)["stopped"].(bool)

	var pending, active, recovery int
	err = s.db.QueryRow(`SELECT
		(SELECT count(*) FROM approval_records WHERE status='PENDING') +
		(SELECT count(*) FROM product_actions WHERE status='PENDING') +
		(SELECT count(*) FROM computer_actions WHERE status='AWAITING_APPROVAL'),
		(SELECT count(*) FROM task_sessions WHERE status IN ('CREATED','PLANNING','WAITING_APPROVAL','RUNNING','VERIFYING')) +
		(SELECT count(*) FROM agent_tasks WHERE status IN ('NEEDS_REVIEW','WAITING_APPROVAL','RUNNING')
		 AND (json_type(json,'$.agent.deadline') IS NULL OR status='RUNNING' OR julianday(json_extract(json,'$.agent.deadline'))>julianday('now'))) +
		(SELECT count(*) FROM product_actions WHERE task_id IS NULL AND status IN ('PENDING','EXECUTING')) +
		(SELECT count(*) FROM computer_actions c WHERE c.status IN ('AWAITING_APPROVAL','REVALIDATING','EXECUTING','VERIFYING')
		 AND NOT EXISTS (SELECT 1 FROM computer_task_evidence e JOIN agent_tasks t ON t.id=e.task_id WHERE e.action_id=c.id
		 AND t.status IN ('NEEDS_REVIEW','WAITING_APPROVAL','RUNNING'))),
		(SELECT count(*) FROM product_actions WHERE status='RECOVERY_REQUIRED') +
		(SELECT count(*) FROM computer_actions WHERE status='RECOVERY_REQUIRED')`).Scan(&pending, &active, &recovery)
	if err != nil {
		return fail(errorCode(err))
	}
	var connectorPending, connectorActive, connectorRecovery, connections int
	toolNames := []string{"filesystem.file.create"}
	if s.communication != nil {
		err = s.db.QueryRow(`SELECT
			(SELECT count(*) FROM communication_actions WHERE status='NEEDS_APPROVAL' AND julianday(json_extract(json,'$.expiresAt'))>julianday('now')),
			(SELECT count(*) FROM communication_actions WHERE status='RUNNING'),
			(SELECT count(*) FROM communication_actions WHERE status='UNKNOWN'),
			(SELECT count(*) FROM communication_connections WHERE json_extract(json,'$.enabled')=1)`).Scan(&connectorPending, &connectorActive, &connectorRecovery, &connections)
		if err != nil {
			return fail(errorCode(err))
		}
		configured, err := s.communication.ListConnections()
		if err != nil {
			return fail("CONNECTOR_STORAGE_FAILED")
		}
		seen := map[string]bool{}
		for _, c := range configured {
			if c.Enabled {
				for _, scope := range c.Scopes {
					for _, op := range scope.Operations {
						name := "connector." + c.Kind + "." + op
						if !seen[name] {
							toolNames = append(toolNames, name)
							seen[name] = true
						}
					}
				}
			}
		}
	}
	pending += connectorPending
	active += connectorPending + connectorActive
	recovery += connectorRecovery
	return result(map[string]any{
		"workspace": s.registry.Workspace(), "maxFileBytes": 65536,
		"toolNames": toolNames, "pendingApprovals": pending,
		"activeTasks": active, "recoveryActions": recovery, "stopAvailable": true,
		"externalNetwork": connections > 0, "externalModelNetwork": "USER_CONFIRMED_BYOK_ONLY",
		"connectorPending": connectorPending, "connectorActive": connectorActive, "connectorRecovery": connectorRecovery, "connections": connections,
		"realModelTools": false, "desktopControl": available, "desktopControlStopped": stopped,
	}, nil)
}
