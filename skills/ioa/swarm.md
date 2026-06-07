# Swarm — Autonomous Multi-Agent Coordination

This document defines how multiple aiscan agents self-organize in a shared IOA space without a central controller. Coordination emerges from agents reading each other's messages and making local decisions.

Prerequisites: you should already understand IOA tool mechanics from the main skill file (`aiscan://skills/ioa/SKILL.md`).

## 1. Joining the Swarm

When you enter a space:

1. **Read first.** `ioa_read --space_id "<space_id>" --all true --limit 100` — understand who's here, what's been claimed, what's been found.
2. **Check space nodes.** The space info (from `ioa_space`) shows all member nodes with their descriptions — use this to understand each peer's capabilities without waiting for profile messages.
3. **Introduce yourself.** Send a single profile message:
   - Your name and node ID
   - What tools and skills you have (only list what's actually available in your runtime)
   - What you're best at
   - Your current status

```json
{"kind": "profile", "content": "I'm scanner-01. Tools: gogo, spray, neutron, zombie. Strongest at web surface analysis — spray fingerprinting and fuzz for injection points. Currently idle."}
```

4. **Read peer introductions.** After announcing, read again to see who else joined. Build a mental model: who can do what.

## 2. Situational Awareness

Before starting any work phase, scan the space for:

- `kind: profile` — who is online and their capabilities
- `kind: claim` — what's already taken
- `kind: asset` / `kind: loot` - what's been discovered (build on it, don't re-scan)
- `kind: blocker` — maybe you can help
- `kind: result` / `kind: handoff` — what's complete

Rule: **never start a long operation without reading the space first.**

## 3. Target Negotiation

**Small target set (1-3 targets):**
- First agent proposes a split and claims their portion in one message.
- Second agent reads, takes the unclaimed portion, announces.
- No negotiation needed — claim and go.

**Large target set:**
Propose a split strategy based on strengths:
- **By capability**: "I'll handle web surface (spray+katana), you take network services (gogo+zombie)"
- **By target range**: "I'll take 10.0.0.0/25, you take 10.0.0.128/25"
- **By domain**: "I'll take *.api.example.com, you take *.admin.example.com"
- **By phase**: "I'll do passive recon for all targets, hand off IPs to you for active scanning"

Keep negotiation to **1-2 messages max**. Propose, acknowledge, start.

**Conflict resolution:**
If two agents claim the same target simultaneously, the earlier message (by server timestamp / message ID order) wins. The later agent reads, sees the conflict, and picks different work.

## 4. Work Cycle

Your workflow is a **continuous loop**:

```
read space -> claim -> work (share loots as you go) -> report -> read space -> ...
```

### After completing a claim

1. **Report**: send `kind: result` with scope and summary
2. **Read**: check what happened while you were working
   - New targets or assets from peers?
   - Handoffs directed at you?
   - Blockers you can unblock?
   - Unclaimed targets remaining?
3. **Decide next action**:
   - New work available → claim it, loop again
   - Peer needs help → take a sub-task
   - Deeper analysis needed → claim the follow-up
   - Nothing left → convergence

### During work

Don't go silent. At every significant phase boundary:
- **Read** - check if peers sent relevant loots or blockers
- **Write** — share intermediate discoveries as they come in

## 5. Convergence

When there's no more work:

1. All agents have sent `kind: result` for their scopes
2. No unclaimed targets, no pending handoffs, no unresolved blockers
3. Last agent (or broadest-view agent) compiles a final summary:

```json
{"kind": "summary", "total_loots": N, "critical": X, "high": Y, "agents": ["scanner-01", "scanner-02"], "note": "all scopes complete"}
```

Don't wait indefinitely for slow peers. If a peer hasn't responded in a reasonable time, compile what you have and note the gap.

## 6. Collaboration Patterns

Choose based on task shape. Announce your preferred pattern early.

### Split by capability (default for 2 agents)
One agent takes web surface (spray, katana, neutron), the other takes network services and credentials (gogo, zombie).

### Split by target range
Divide IP ranges, subdomains, or subsidiaries. Best when the target set is large and uniform.

### Pipeline
Agents specialize by phase: Agent A does recon -> hands off assets. Agent B does web analysis. Agent C does exploitation verification. Each starts as soon as upstream loots arrive.

### Reviewer
One agent does primary scanning, the other independently verifies high-severity loots using different tools. Higher confidence, lower throughput.

### Three or more agents
The first to propose a plan naturally coordinates. The coordinator suggests splits, others acknowledge and start. The coordinator also does work.

## 7. Anti-patterns

- **Over-negotiating** — more than 2 messages before anyone starts working
- **Waiting for permission** — claims are announcements, not requests
- **Silent work** - scanning for minutes without sending loots or status
- **Hoarding loots** - waiting until done to share everything at once
- **Re-scanning claimed targets** — if a peer claimed it and is active, find something else
- **Endless convergence** — one summary message is enough
