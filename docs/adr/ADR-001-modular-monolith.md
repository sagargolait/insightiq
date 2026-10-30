# ADR-001: Modular monolith

## Status
Accepted

## Decision
Start with one Go API codebase organized by business domain, plus a separately runnable worker process.

## Context
The product is early and domain boundaries are still evolving. Independent microservices would add deployment, networking, observability and consistency costs before there is a demonstrated need.

## Consequences
- Modules have explicit ownership and interfaces.
- No direct cross-module database access without an agreed repository/application boundary.
- Extract a module only when scaling, reliability or team ownership justifies it.
