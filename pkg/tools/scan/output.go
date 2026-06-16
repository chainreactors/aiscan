package scan

import (
	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/parsers"
)

func formatEventLine(event event, color bool) string {
	c := output.NewColor(color)
	switch event.Kind {
	case eventTarget:
		switch target := event.Target.(type) {
		case serviceTarget:
			if target.Result == nil {
				return ""
			}
			label := "service"
			if target.Result.IsHttp() {
				label = "web"
			}
			return output.FormatLine(output.OutputPrefix(label, c.Green), target.Result.OutputLine(), c)
		case webTarget:
			if target.URL == "" {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("web", c.Green), parsers.JoinOutput(target.URL, target.HostHeader), c)
		case webProbeTarget:
			if !reportableSprayResultForCapability(target.Result, target.Capability) {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("web", c.Green), target.Result.OutputLine(), c)
		}
	case eventLoot:
		if event.Loot == nil {
			return ""
		}
		loot := event.Loot
		switch loot.Kind {
		case output.LootFingerprint:
			focus, _ := loot.Data["focus"].(bool)
			if !focus {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("fingerprint", c.ForPriority(loot.Priority)), loot.Description, c)
		case output.LootWeakpass:
			return output.FormatLine(output.OutputPrefix("risk", c.ForPriority(loot.Priority)), loot.Description, c)
		case output.LootVuln:
			return output.FormatLine(output.OutputPrefix("vuln", c.ForPriority(loot.Priority)), loot.Description, c)
		default:
			return output.FormatLine(output.OutputPrefix(loot.Kind, c.ForPriority(loot.Priority)), loot.Description, c)
		}
	case eventError:
		if event.Error.Message == "" {
			return ""
		}
		return output.FormatLine(output.OutputPrefix("error", c.Red), parsers.JoinOutput(event.Error.Message), c)
	}
	return ""
}
