# ADR-002: Shared PostgreSQL database for tenants

## Status
Accepted for initial release

## Decision
Use shared tables with organization ownership, composite constraints and PostgreSQL Row-Level Security for tenant-owned data.

## Consequences
Every tenant-owned query must run with transaction-local tenant context. The application role must not own protected tables or have BYPASSRLS. Add cross-tenant integration tests before production.
