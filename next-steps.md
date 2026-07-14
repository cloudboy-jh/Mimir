# Next Steps

## Self-Hosted Dashboard

Add a small Vue dashboard served by the user's Mimir Worker. It remains a client of the canonical HTTP API and does not introduce a Mimir-hosted backend or account system.

Initial scope:

- Session table with repo, model, outcome, date, request count, and token usage.
- Filters for repo, model, outcome, and date range.
- Session detail with files, errors, exchange timeline, and links to redacted R2 objects.
- Explicit outcome marking.
- Deployment, D1, and R2 status.

Authentication:

- `mimir dashboard` creates a short-lived one-time code and opens the deployed dashboard.
- The Worker exchanges the code for an `HttpOnly`, `Secure`, `SameSite` cookie.
- Browser code never stores a machine bearer token.

Keep it intentionally narrow: no SaaS tenancy, team management, analytics suite, or separate dashboard backend.

## Consolidate the CLI Package

Reduce the fragmented `cmd/mimir` package from roughly twenty small implementation files to a few cohesive files organized by responsibility.

- Keep command dispatch and user-facing output together.
- Keep setup, login, connection, and terminal rendering together.
- Keep remote memory commands and MCP handling together.
- Keep local indexing, recall, Git, and storage together.
- Consolidate tests along the same boundaries.
- Preserve all command behavior, JSON contracts, credential handling, and Worker API boundaries.
- Do not introduce new packages or dependencies solely to rearrange the code.
