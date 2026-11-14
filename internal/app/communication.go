package app

import (
	"asimi/internal/communication"
	"asimi/internal/domain"
	"strings"
)

func (s *Service) initializeCommunication() error {
	manager, e := communication.New(s.db, communication.Options{})
	if e != nil {
		return e
	}
	s.communication = manager
	return nil
}
func (s *Service) stopCommunication() error {
	if s.communication == nil {
		return nil
	}
	return s.communication.Stop()
}
func communicationReply(data any, e error) domain.Reply {
	if e == nil {
		return domain.Reply{Data: data}
	}
	// Connector/network/credential failures expose fixed codes only; never log a
	// response, URL, credential, or database error through the business boundary.
	code := e.Error()
	switch {
	case strings.HasPrefix(code, "CONNECTOR_"):
		return fail(code)
	case strings.HasPrefix(code, "CREDENTIAL_"):
		return fail("CONNECTOR_CREDENTIAL_UNAVAILABLE")
	case code == "INVALID_REQUEST":
		return fail(code)
	default:
		return fail("CONNECTOR_STORAGE_FAILED")
	}
}
func (s *Service) callCommunication(method, payload string) (domain.Reply, bool) {
	switch method {
	case "ListConnections", "SaveConnection", "RevokeConnection", "PrepareConnectorAction", "ApproveConnectorAction", "RejectConnectorAction", "ListConnectorActions", "CancelConnectorAction":
	default:
		return domain.Reply{}, false
	}
	if s.communication == nil {
		return fail("CONNECTOR_UNAVAILABLE"), true
	}
	respond := func(value any, e error) (domain.Reply, bool) {
		if e == nil && method != "ListConnections" && method != "ListConnectorActions" {
			s.changed("", "")
		}
		return communicationReply(value, e), true
	}
	switch method {
	case "ListConnections":
		if e := decode(payload, &struct{}{}); e != nil {
			return respond(nil, e)
		}
		value, e := s.communication.ListConnections()
		return respond(value, e)
	case "SaveConnection":
		var in communication.SaveConnectionInput
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		value, e := s.communication.SaveConnection(in)
		return respond(value, e)
	case "RevokeConnection":
		var in struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		e := s.communication.Revoke(in.ID)
		return respond(map[string]bool{"revoked": e == nil}, e)
	case "CancelConnectorAction":
		var in struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		value, e := s.communication.Cancel(in.ID)
		return respond(value, e)
	case "PrepareConnectorAction":
		var in communication.PrepareInput
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		value, e := s.communication.Prepare(in)
		return respond(value, e)
	case "ListConnectorActions":
		var in struct {
			ConnectionID string `json:"connectionId"`
		}
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		value, e := s.communication.ListActions(in.ConnectionID)
		return respond(value, e)
	default:
		var in struct {
			ID   string `json:"id"`
			Hash string `json:"hash"`
		}
		if e := decode(payload, &in); e != nil {
			return respond(nil, e)
		}
		if method == "RejectConnectorAction" {
			value, e := s.communication.Reject(in.ID, in.Hash)
			return respond(value, e)
		}
		value, e := s.communication.Approve(s.lifetime, in.ID, in.Hash)
		return respond(value, e)
	}
}
