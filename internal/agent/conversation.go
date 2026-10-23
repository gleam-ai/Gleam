package agent

import (
	"errors"
	"strings"

	"gleam/internal/harness/conversation"
	"gleam/pkg/types"
)

func (a *Agent) appendConvo(convoID string, msg conversation.Message) (int, error) {
	if a.Convos == nil || strings.TrimSpace(convoID) == "" {
		return 0, nil
	}
	if strings.TrimSpace(msg.Content) == "" {
		return 0, nil
	}
	c, err := a.Convos.Append(convoID, msg)
	if err != nil {
		return 0, err
	}
	return len(c.Messages), nil
}

func convoReply(r *types.GoalResult) string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.Summary) != "" {
		return r.Summary
	}
	if strings.TrimSpace(r.Error) != "" {
		return "⚠️ " + r.Error
	}
	if strings.TrimSpace(r.Suggestion) != "" {
		return r.Suggestion
	}
	return ""
}

func (a *Agent) LoadConversationContext(convoID string) (*conversation.Conversation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.idleLocked(); err != nil {
		return nil, err
	}
	return a.loadConversationContextLocked(convoID)
}

func (a *Agent) loadConversationContextLocked(convoID string) (*conversation.Conversation, error) {
	if a.Convos == nil {
		return nil, errors.New("会话存储未启用")
	}
	c, err := a.Convos.Get(convoID)
	if err != nil {
		return nil, err
	}
	if a.Mem != nil {
		a.Mem.SwitchConversation(c.ID)
		start := a.Mem.Cursor()
		if start < 0 || start > len(c.Messages) {
			start = 0
		}
		for _, m := range c.Messages[start:] {
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			a.Mem.AddTurn(m.Role, m.Content)
		}
		a.Mem.AdvanceCursor(len(c.Messages))
	}
	if a.Spaces != nil {
		sid := c.SpaceID
		if sid == "" {
			sid = conversation.DefaultSpaceID
		}
		_ = a.Spaces.SetActive(sid)
		if sp, err := a.Spaces.Get(sid); err == nil && strings.TrimSpace(sp.Path) != "" {
			_ = a.applyWorkspace(sp.Path)
		}
	}
	return c, nil
}

func (a *Agent) NewConversation(spaceIDs ...string) (*conversation.Conversation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	spaceID := ""
	if len(spaceIDs) > 0 {
		spaceID = strings.TrimSpace(spaceIDs[0])
	}
	if spaceID == "" {
		spaceID = a.ActiveSpaceID()
	}
	c, err := a.Convos.Create(spaceID)
	if err != nil {
		return nil, err
	}
	if a.Mem != nil && a.idleLocked() == nil {
		a.Mem.SwitchConversation(c.ID)
	}
	return c, nil
}

func (a *Agent) ActiveSpaceID() string {
	if a.Spaces == nil {
		return conversation.DefaultSpaceID
	}
	if id, err := a.Spaces.ActiveID(); err == nil {
		return id
	}
	return conversation.DefaultSpaceID
}
