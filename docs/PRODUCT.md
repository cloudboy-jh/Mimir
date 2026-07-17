# Product

## Register

product

## Platform

web

## Users

Mimir is for an individual developer working across coding agents, repositories, and machines. They open the dashboard while investigating what an agent did, why a session succeeded or failed, or what prior work should inform the next attempt. The interface must support fast scanning on desktop and remain usable as responsive web UI.

## Product Purpose

The dashboard turns Mimir's captured model traffic into understandable work sessions. Its primary job is one-click session understanding: what the agent attempted, which models and tools were involved, what files and errors mattered, and whether the work landed, was discarded, was abandoned, or remains unresolved. The request log is supporting evidence, not the center of the product.

Success means a developer can open Mimir, recognize the relevant session, and understand its shape and outcome without reading a transcript from the beginning.

## Positioning

Mimir is a private memory plane that makes agent work legible across time while keeping the data inside the developer's own Cloudflare account.

## Brand Personality

Technical, quiet, exact. Mimir should feel like a trusted developer instrument: direct, information-dense, and confident without becoming sterile or ornamental. The pixel-art wordmark carries the brand personality; the product interface should remain operational and restrained.

## Anti-references

Do not resemble a generic SaaS dashboard. Avoid pill navigation, decorative gradients, blended color washes, interchangeable KPI card walls, oversized marketing typography, and arbitrary accent color. Do not turn the product into an OpenRouter clone: its activity-table clarity is a useful reference, but Mimir's defining experience is session comprehension.

## Design Principles

1. Lead with sessions, not infrastructure. Organize the interface around understandable episodes of agent work.
2. Make evidence one click away. Requests, files, errors, models, providers, and raw exchanges should support the session story without overwhelming it.
3. Preserve operational density. Use familiar tables and controls with clear hierarchy rather than decorative containers.
4. Let the wordmark carry the identity. Keep the surrounding interface quiet enough that Mimir feels distinctive without themed component gimmicks.
5. Keep persistence and work result separate. Empty, Pending, Saved, Failed, or Partial capture describes durable memory; Landed, Discarded, Abandoned, or Unresolved describes the work outcome.
6. Make persistence visible without turning it into log output. After meaningful work, the harness should show one compact receipt such as `Saved to Mimir · 14 exchanges`; when dashboard Access is configured, a `View session` action provides exact IDs, timestamps, and failure details.

## Accessibility & Inclusion

Target WCAG 2.2 AA. Maintain sufficient text and control contrast in light and dark themes, complete keyboard operation, visible focus states, reduced-motion support, non-color state cues, semantic tables, and responsive layouts that do not require whole-page horizontal scrolling.
