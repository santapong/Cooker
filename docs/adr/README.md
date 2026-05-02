# Architecture Decision Records

Lightweight ADRs that capture the *why* behind the bigger structural decisions in Cooker. Each ADR is a small, dated, immutable record — if a decision is reversed, write a new ADR that *supersedes* the old one rather than editing in place.

## Format

Each record uses the [Nygard MADR-lite](https://adr.github.io/madr/) shape:

```
# Title
Date: YYYY-MM-DD
Status: proposed | accepted | superseded by ADR-NNNN | deprecated

## Context
## Decision
## Consequences
```

## Index

| # | Title | Status |
|---|---|---|
| [0001](0001-strategy-pattern-interfaces.md) | Strategy-pattern interfaces for builder / pusher / deployer / secrets / deploytarget | accepted |
| [0002](0002-secrets-manager.md) | Pluggable `secrets.Manager` with KeepSave as system of record | accepted |
| [0003](0003-jsonb-graph-storage.md) | JSONB columns for pipeline graphs and environment secrets | accepted |
