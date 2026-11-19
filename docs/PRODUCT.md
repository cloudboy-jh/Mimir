# Product

## Register

product

## Platform

agents first, web second

## Users

Mimir is for an individual developer working across coding agents, repositories,
and machines. Their agents query Mimir to recall prior work, chart activity, and
inspect captured commits from inside whichever harness they run, through MCP,
native harness tools, or the CLI. The developer opens the dashboard occasionally
to look over a day, browse sessions, or manage devices.

## Product Purpose

Mimir turns captured model traffic and recovered local harness history into
work sessions that agents can query in one or two calls: what was attempted,
which models, files, errors and commits were involved, and whether the work was
committed or pushed. Capture is automatic; agents never record, mark, verify, or
end anything.

Success means an agent answers "how did I fix this before?", "what did I do on
this repository last week?", or "chart my sessions by repo" with one or two tool
calls and structured results it can present as text, tables, or charts.

The dashboard is a small secondary viewer with three pages: Day, Sessions (with
the session page), and Devices.

## Positioning

Mimir is a private memory plane that makes agent work queryable across time,
harnesses, and machines while keeping the data inside the developer's own
Cloudflare account.

## Brand Personality

Technical, quiet, exact. Mimir should feel like a trusted developer instrument:
direct, information-dense, and confident without becoming sterile or ornamental.
The pixel-art wordmark carries the brand personality; the product interface
should remain operational and restrained.

## Anti-references

Do not resemble a generic SaaS dashboard. Avoid pill navigation, decorative
gradients, blended color washes, interchangeable KPI card walls, oversized
marketing typography, and arbitrary accent color. Do not build an analytics
suite: agents draw their own charts from the query API.

## Design Principles

1. Agents first. Every fact the dashboard shows is available to agents through
   the same query API, MCP tools, and CLI.
2. Few calls. Prefer one query that answers a whole question (`stats`, `day`,
   `search`) over chains of lookups.
3. Lead with sessions, not infrastructure. Requests are evidence inside a
   session, not a product of their own.
4. Derive, don't ask. Git status (`none`, `committed`, `pushed`) comes from
   captured commits; agents are never asked to judge or mark their work.
5. Keep persistence separate from work. Capture status (Empty, Pending, Saved,
   Failed, Partial) describes durable memory, not whether work succeeded.
6. Make repair safe and unsurprising. Import and backfill merge
   deterministically so reruns fill gaps without duplicating evidence.

## Accessibility & Inclusion

Target WCAG 2.2 AA. Maintain sufficient text and control contrast in light and
dark themes, complete keyboard operation, visible focus states, reduced-motion
support, non-color state cues, semantic tables, and responsive layouts that do
not require whole-page horizontal scrolling.
