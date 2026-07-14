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
