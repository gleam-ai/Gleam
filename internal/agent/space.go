package agent

import (
	"errors"
	"strings"

	"gleam/internal/harness/conversation"
	"gleam/internal/harness/space"
)

var errSpaceUnavailable = errors.New("空间存储未启用")

func (a *Agent) SpaceView() map[string]any {
	if a.Spaces == nil {
		return map[string]any{"active_id": conversation.DefaultSpaceID, "spaces": []any{}, "workspace": a.Cfg.Workspace}
	}
	list, err := a.Spaces.List()
	if err != nil {
		list = nil
	}
	counts := map[string]int{}
	if a.Convos != nil {
		if c, err := a.Convos.CountBySpace(); err == nil {
			counts = c
		}
	}
	spacesOut := make([]map[string]any, 0, len(list))
	for _, sp := range list {
		spacesOut = append(spacesOut, map[string]any{
			"id":                 sp.ID,
			"name":               sp.Name,
			"path":               sp.Path,
			"created_at":         sp.CreatedAt,
			"updated_at":         sp.UpdatedAt,
			"conversation_count": counts[sp.ID],
			"is_default":         sp.ID == space.DefaultID,
		})
	}
	return map[string]any{
		"active_id": a.ActiveSpaceID(),
		"spaces":    spacesOut,
		"workspace": a.Cfg.Workspace,
	}
}

func (a *Agent) SpaceCreate(name, path string) (map[string]any, error) {
	if a.Spaces == nil {
		return nil, errSpaceUnavailable
	}
	path = strings.TrimSpace(path)
	if path != "" {
		if err := a.applyWorkspace(path); err != nil {
			return nil, err
		}
	}
	sp, err := a.Spaces.Create(name, path)
	if err != nil {
		return nil, err
	}
	if err := a.Spaces.SetActive(sp.ID); err != nil {
		return nil, err
	}
	return a.SpaceView(), nil
}

func (a *Agent) SpaceRename(id, name string) (map[string]any, error) {
	if a.Spaces == nil {
		return nil, errSpaceUnavailable
	}
	if _, err := a.Spaces.Rename(id, name); err != nil {
		return nil, err
	}
	return a.SpaceView(), nil
}

func (a *Agent) SpaceSetPath(id, path string) (map[string]any, error) {
	if a.Spaces == nil {
		return nil, errSpaceUnavailable
	}
	if _, err := a.Spaces.SetPath(id, strings.TrimSpace(path)); err != nil {
		return nil, err
	}
	return a.SpaceView(), nil
}

func (a *Agent) SpaceActivate(id string) (map[string]any, error) {
	if a.Spaces == nil {
		return nil, errSpaceUnavailable
	}
	sp, err := a.Spaces.Get(id)
	if err != nil {
		return nil, err
	}
	if err := a.Spaces.SetActive(id); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sp.Path) != "" {
		if err := a.applyWorkspace(sp.Path); err != nil {
			return a.SpaceView(), err
		}
	}
	return a.SpaceView(), nil
}

func (a *Agent) SpaceDelete(id string) (map[string]any, error) {
	if a.Spaces == nil {
		return nil, errSpaceUnavailable
	}
	if id == space.DefaultID {
		return nil, space.ErrDefaultSpace
	}
	wasActive := a.ActiveSpaceID() == id
	if a.Convos != nil {
		if _, err := a.Convos.ReassignSpace(id, space.DefaultID); err != nil {
			return nil, err
		}
	}
	if err := a.Spaces.Delete(id); err != nil {
		return nil, err
	}
	if wasActive {
		_ = a.Spaces.SetActive(space.DefaultID)
		if def, err := a.Spaces.Get(space.DefaultID); err == nil && strings.TrimSpace(def.Path) != "" {
			_ = a.applyWorkspace(def.Path)
		}
	}
	return a.SpaceView(), nil
}
