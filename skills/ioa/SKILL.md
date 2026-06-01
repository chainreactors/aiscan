---
name: ioa
description: Use when collaborating with peer agents via IOA (shared message spaces). Covers tool API, message format, and basic coordination rules. For multi-agent swarm coordination without a central controller, also read aiscan://skills/ioa/swarm.md.
---

# IOA — Inter-Operator Async Collaboration

IOA provides shared message spaces for agent coordination. Three tools: `ioa_space`, `ioa_send`, `ioa_read`.

## 1. Tool API

### ioa_space

Create or join a space. Returns space info including all member nodes with their descriptions.

```
ioa_space --name "case-target" --description "Your role or intent in this space"
```

The response includes `nodes[]` — each node's ID, name, and description. Use this to understand who else is in the space and what they can do.

### ioa_send

Send a message to a space. The `--space_id` parameter is always required.

```
ioa_send --space_id "<id>" --content '{"kind": "asset", "ips": ["10.0.0.1"]}'
```

Optional parameters:
- `--refs '{"messages": ["<msg_id>"], "nodes": ["<node_id>"]}'` — reference a prior message or address a specific node
- `--meta '{"kind": "task_dispatch"}'` — metadata for routing (e.g. task dispatch)

### ioa_read

Read messages from a space. The `--space_id` parameter is always required.

```
ioa_read --space_id "<id>" --all true              # all messages
ioa_read --space_id "<id>" --all true --limit 50   # last 50
ioa_read --space_id "<id>" --message_id "<id>"     # thread from a message
ioa_read --space_id "<id>" --after "<id>"          # messages after cursor
```

Without `--all true`, only messages explicitly directed at your node are returned.

## 2. Message Format

Messages use structured JSON content with a `kind` field for routing:

| kind | purpose | key fields |
|------|---------|------------|
| `claim` | announce work you're about to start | `scope`, `eta_min` |
| `asset` | share discovered targets | `ips`, `domains`, `source` |
| `finding` | share vulnerabilities or dead ends | `severity`, `target`, `vuln`, `evidence` |
| `handoff` | signal phase transition | `from_phase`, `next`, `context` |
| `blocker` | request help | `reason`, `need` |
| `result` | report completed work | `scope`, `summary`, `findings_count` |

### Refs

- `refs.messages`: reference a prior message (reply, follow-up)
- `refs.nodes`: address a specific node. Omit to broadcast to all space members.

### Task dispatch

To start a new task on a peer node, the content **must** include a `content` key (string with the task), and meta must have `kind: task_dispatch`:

```json
ioa_send --space_id "<id>" --content '{"content": "Scan 10.0.0.0/24 for web services", "meta": {"kind": "task_dispatch"}, "targets": ["10.0.0.0/24"]}' --refs '{"nodes": ["<target_node_id>"]}'
```

## 3. Basic Coordination Rules

These apply in any multi-agent scenario:

1. **Read before write** — always `ioa_read --all true` before starting work. A peer may have already claimed your target.
2. **Claim before work** — send `kind: claim` with your scope before any significant operation.
3. **Share as you go** — emit findings immediately, not in a final batch. Peers need your data to make decisions now.
4. **No noise** — the space is shared memory, not chat. No "ok", "thanks", or thinking-out-loud.
5. **Conflict resolution** — if two agents claim the same scope simultaneously, earlier message (by server ID order) wins. The later agent adapts.

## 4. Multi-Agent Swarm

When working in a swarm (2+ agents, no central controller), read the full coordination protocol at `aiscan://skills/ioa/swarm.md`. It covers: semantic self-introduction, target negotiation strategies, work cycles, convergence criteria, and collaboration patterns (split-by-skill, pipeline, reviewer).
