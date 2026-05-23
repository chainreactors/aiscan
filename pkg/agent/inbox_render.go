package agent

import (
	"encoding/json"
	"encoding/xml"
	"sort"
	"strings"

	"github.com/chainreactors/aiscan/pkg/provider"
)

func inboxMessageToChatMessage(msg InboxMessage) provider.ChatMessage {
	return provider.NewTextMessage("user", renderInboxMessage(msg))
}

func renderInboxMessage(msg InboxMessage) string {
	if inboxMessageHasNoEnvelope(msg) {
		return msg.Content
	}
	if msg.Source == "task" && (msg.Kind == "completion" || msg.Kind == "reminder") {
		return msg.Content
	}

	tag := "inbox_message"
	if msg.Source == "ioa" && msg.Kind == "peer_message" {
		tag = "swarm_peer"
	}

	var sb strings.Builder
	sb.WriteByte('<')
	sb.WriteString(tag)
	writeXMLAttr(&sb, "source", msg.Source)
	writeXMLAttr(&sb, "kind", msg.Kind)
	writeXMLAttr(&sb, "sender", msg.Sender)
	writeXMLAttr(&sb, "message_id", msg.MessageID)
	for _, key := range sortedStringKeys(msg.Attributes) {
		writeXMLAttr(&sb, key, msg.Attributes[key])
	}
	sb.WriteString(">\n")

	if msg.Content != "" {
		_ = xml.EscapeText(&sb, []byte(msg.Content))
		sb.WriteByte('\n')
	}
	writeJSONElement(&sb, "targets", msg.Targets)
	writeJSONElement(&sb, "refs", msg.Refs)
	writeJSONElement(&sb, "meta", msg.Meta)
	writeJSONElement(&sb, "raw_content", msg.RawContent)

	sb.WriteString("</")
	sb.WriteString(tag)
	sb.WriteByte('>')
	return sb.String()
}

func inboxMessageHasNoEnvelope(msg InboxMessage) bool {
	return msg.Source == "" &&
		msg.Kind == "" &&
		msg.Sender == "" &&
		msg.MessageID == "" &&
		len(msg.Attributes) == 0 &&
		len(msg.Targets) == 0 &&
		len(msg.Refs) == 0 &&
		len(msg.Meta) == 0 &&
		len(msg.RawContent) == 0
}

func writeXMLAttr(sb *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	sb.WriteByte(' ')
	sb.WriteString(name)
	sb.WriteString("=\"")
	_ = xml.EscapeText(sb, []byte(value))
	sb.WriteByte('"')
}

func writeJSONElement(sb *strings.Builder, name string, value any) {
	if isEmptyJSONValue(value) {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	sb.WriteByte('<')
	sb.WriteString(name)
	sb.WriteByte('>')
	_ = xml.EscapeText(sb, data)
	sb.WriteString("</")
	sb.WriteString(name)
	sb.WriteString(">\n")
}

func isEmptyJSONValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case []string:
		return len(v) == 0
	case map[string][]string:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func sortedStringKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
