# Architecture Decision Records (ADR)

This directory contains Architecture Decision Records for Gleam. Each ADR captures a significant architectural decision, its context, and consequences.

## What is an ADR?

An Architecture Decision Record (ADR) is a short text file that documents a significant architectural decision made during a project. ADRs help:

- **New contributors** understand why the system is built the way it is
- **Future maintainers** know the constraints and trade-offs that shaped decisions
- **Reviewers** evaluate proposed changes against established architectural direction

## ADR Format

We use a lightweight format inspired by [MADR (Markdown Architectural Decision Records)](https://adr.github.io/madr/):

```markdown
# ADR-NNN: Decision Title

- **Status**: Proposed | Accepted | Deprecated | Superseded
- **Date**: YYYY-MM-DD
- **Deciders**: Who was involved in making this decision

## Context

What is the issue that we're seeing that this architectural decision relates to?

## Decision

What is the change that we're proposing and/or doing?

## Consequences

What becomes easier or more difficult to do because of this change?

### Positive Consequences

- ...

### Negative Consequences

- ...

## References

- Links to related ADRs, issues, or documentation
```

## Naming Convention

Files are named `ADR-NNN-short-title.md` where NNN is a zero-padded number (e.g., `ADR-001-single-agent-architecture.md`).

## Process

1. **Propose**: Create a new ADR file with status "Proposed"
2. **Discuss**: Reference the ADR in pull requests or issues for discussion
3. **Accept**: Update status to "Accepted" once the decision is finalized
4. **Deprecate/Supersede**: When a decision is reversed or replaced, update the original ADR's status and create a new ADR explaining the change

## Relationship to Other Documents

- **AGENTS.md** (internal, not in git): Contains agent-specific guidelines and conventions
- **docs/known-limits.md**: Documents deliberate non-features and known limitations
- **CONTRIBUTING.md**: Development workflow and repository layout
- **ADRs**: Capture the "why" behind architectural decisions

ADRs complement these documents by providing historical context for decisions that shaped the architecture.

## When to Write an ADR

Write an ADR when:

- Making a decision that affects the overall system architecture
- Choosing between multiple viable approaches with different trade-offs
- Establishing a pattern that will be followed across the codebase
- Reversing or significantly changing a previous architectural decision

Do **not** write an ADR for:

- Implementation details that don't affect architecture
- Decisions that are obvious or have only one reasonable approach
- Temporary workarounds (document these in code comments or known-limits.md instead)
