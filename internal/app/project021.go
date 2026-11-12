package app

import (
	"deskpilot/internal/command"
	"deskpilot/internal/developer"
)

type DeveloperProjectInspection struct {
	developer.ProjectInspection
	Runtimes []command.RuntimeDiagnostic `json:"runtimes"`
}

func (s *Service) InspectDeveloperProject(workspaceID string, exclusions []string) (DeveloperProjectInspection, error) {
	w, err := s.workspace(workspaceID)
	if err != nil {
		return DeveloperProjectInspection{}, err
	}
	project, err := developer.InspectProject(w.Root, exclusions)
	if err != nil {
		return DeveloperProjectInspection{}, err
	}
	return DeveloperProjectInspection{ProjectInspection: project, Runtimes: command.InspectProjectRuntimes(w.Root, project)}, nil
}
