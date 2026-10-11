# ADR-001: Single-Agent Architecture

- **Status**: Accepted
- **Date**: 2026-10-09
- **Deciders**: Gleam maintainers

## Context

Gleam is a local-first desktop AI agent. Early discussions considered whether to adopt a multi-agent architecture (graph engineering, agent collaboration) or keep the system simple with a single agent executing a plan-execute-reflect loop.

Multi-agent systems offer benefits for distributed workloads and specialized expertise, but introduce complexity in coordination, context isolation, and failure handling. For Gleam's use case—a single user's desktop assistant running tasks sequentially—the overhead of multi-agent coordination was not justified.

## Decision

Gleam uses a **single-agent architecture** with a plan-execute-reflect loop:

1. **Plan**: Decompose the user's goal into structured steps
2. **Execute**: Run each step sequentially, with tool calls and permission gates
3. **Reflect**: Evaluate outcomes and decide whether to replan or complete

The agent has access to tools (file operations, shell commands, HTTP, etc.) and can delegate lightweight sub-tasks via `agent.delegate`, but there is no persistent agent pool, no inter-agent communication protocol, and no graph-based orchestration.

## Consequences

### Positive Consequences

- **Simplicity**: No coordination layer, no distributed state, no consensus protocols
- **Debuggability**: Single execution context makes it easy to trace decisions and tool calls
- **Resource efficiency**: No overhead from managing multiple agent instances
- **Predictable behavior**: Sequential execution is easier for users to understand and reason about
- **Auditability**: Clear linear history of what the agent did and why

### Negative Consequences

- **No parallelism**: Cannot execute independent sub-tasks concurrently (though this is rarely needed for desktop tasks)
- **No specialization**: Cannot have dedicated agents for different domains (e.g., coding agent, research agent)
- **Scaling ceiling**: If Gleam ever needs to handle multiple users or long-running background tasks, the single-agent model will need to be revisited

## Alternatives Considered

### Multi-Agent with Graph Orchestration

Use a framework like LangGraph or AutoGen to define agent roles and communication patterns. Rejected because:
- Adds significant complexity for a single-user desktop app
- Context isolation between agents is hard to get right
- Failure modes are harder to debug
- Overkill for sequential task execution

### Agent Pool with Task Queue

Maintain a pool of agents that pick up tasks from a queue. Rejected because:
- Gleam runs on a single user's machine; there's no workload distribution problem
- Would require persistent agent state and lifecycle management
- Adds latency from queueing and scheduling

## References

- [docs/known-limits.md](../known-limits.md) §2: "多 Agent 协作 / Graph Engineering" is explicitly listed as a deliberate non-feature
- `internal/agent/agent.go`: The single-agent implementation
- `internal/tools/std/std.go`: The `agent.delegate` tool for lightweight sub-task delegation
