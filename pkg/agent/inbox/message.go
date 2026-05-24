package inbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

type Origin string

const (
	OriginUser   Origin = "user"
	OriginPeer   Origin = "peer"
	OriginTask   Origin = "task"
	OriginSystem Origin = "system"
)

type Priority int

const (
	PriorityLow    Priority = -10
	PriorityNormal Priority = 0
	PriorityHigh   Priority = 10
)

type Attachment struct {
	Type    string // "file", "skill", "raw"
	Ref     string // e.g. "@/tmp/targets.txt", "@scan"
	Content string
	Error   string
}

type Message struct {
	ChatMessage provider.ChatMessage
	Origin      Origin
	Priority    Priority
	Attachments []Attachment
	Meta        map[string]any
	CreatedAt   time.Time
}

func NewMessage(origin Origin, role, content string) Message {
	return Message{
		ChatMessage: provider.NewTextMessage(role, content),
		Origin:      origin,
		CreatedAt:   time.Now(),
	}
}

func NewUserMessage(content string) Message {
	return NewMessage(OriginUser, "user", content)
}

func NewSystemMessage(content string) Message {
	return NewMessage(OriginSystem, "user", content)
}

func FromChatMessage(msg provider.ChatMessage, origin Origin) Message {
	return Message{
		ChatMessage: msg,
		Origin:      origin,
		CreatedAt:   time.Now(),
	}
}

func (m Message) ToChatMessages() []provider.ChatMessage {
	if len(m.Attachments) == 0 {
		return []provider.ChatMessage{m.ChatMessage}
	}
	var sb strings.Builder
	if m.ChatMessage.Content != nil {
		sb.WriteString(*m.ChatMessage.Content)
	}
	for _, att := range m.Attachments {
		if att.Error != "" {
			sb.WriteString(fmt.Sprintf("\n\n<attachment_error type=%q ref=%q>%s</attachment_error>", att.Type, att.Ref, att.Error))
			continue
		}
		if att.Content == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n\n<attachment type=%q ref=%q>\n%s\n</attachment>", att.Type, att.Ref, att.Content))
	}
	content := sb.String()
	msg := m.ChatMessage
	msg.Content = &content
	return []provider.ChatMessage{msg}
}

func (m Message) WithMeta(key string, value any) Message {
	if m.Meta == nil {
		m.Meta = make(map[string]any)
	}
	m.Meta[key] = value
	return m
}

func (m Message) WithPriority(p Priority) Message {
	m.Priority = p
	return m
}
