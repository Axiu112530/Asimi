package app

const unifiedCommunicationSchema = `
CREATE TRIGGER IF NOT EXISTS unified_connector_insert AFTER INSERT ON communication_actions BEGIN
 INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,action_id,created_at,updated_at)
 VALUES(NEW.id,'communication',COALESCE(json_extract(NEW.json,'$.operation'),'external operation'),NEW.status,NEW.id,NEW.created_at,NEW.updated_at);
END;
CREATE TRIGGER IF NOT EXISTS unified_connector_update AFTER UPDATE ON communication_actions BEGIN
 UPDATE unified_runs SET status=NEW.status,updated_at=NEW.updated_at WHERE action_id=NEW.id;
END;
INSERT OR IGNORE INTO unified_runs(id,kind,goal,status,action_id,created_at,updated_at)
 SELECT id,'communication',COALESCE(json_extract(json,'$.operation'),'external operation'),status,id,created_at,updated_at FROM communication_actions;
`

func (s *Service) initializeUnifiedCommunication() error {
	_, err := s.db.Exec(unifiedCommunicationSchema)
	return err
}
