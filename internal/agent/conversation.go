package agent

import (
	"strings"

	"gleam/internal/harness/conversation"
	"gleam/pkg/types"
)

func (a *Agent) appendConvo(convoID string, msg conversation.Message) {
	if a.Convos == nil || strings.TrimSpace(convoID) == "" {
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	_, _ = a.Convos.Append(convoID, msg)
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
	c, err := a.Convos.Get(convoID)
	if err != nil {
		return nil, err
	}
	if a.Mem != nil {
		a.Mem.ResetConversation()
		for _, m := range c.Messages {
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			a.Mem.AddTurn(m.Role, m.Content)
		}
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
	if a.Mem != nil {
		a.Mem.ResetConversation()
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
