---
name: Mimir
description: A quiet flight recorder for understanding agent work.
colors:
  action: "#0f766e"
  canvas: "#f7f7f5"
  surface: "#ffffff"
  ink: "#1c1c1a"
  muted-ink: "#62625d"
  rule: "#deded9"
  dark-canvas: "#111110"
  dark-surface: "#191918"
  dark-ink: "#f2f2ef"
  dark-rule: "#363633"
typography:
  headline:
    fontFamily: "IBM Plex Sans, ui-sans-serif, system-ui, sans-serif"
    fontSize: "28px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "-0.025em"
  body:
    fontFamily: "IBM Plex Sans, ui-sans-serif, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
  label:
    fontFamily: "IBM Plex Sans, ui-sans-serif, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: 1.3
    letterSpacing: "normal"
  mono:
    fontFamily: "IBM Plex Mono, ui-monospace, SFMono-Regular, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "normal"
rounded:
  control: "5px"
  panel: "7px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "32px"
components:
  button-quiet:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "8px 10px"
    height: "34px"
  filter-selected:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.surface}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "8px 10px"
    height: "34px"
---

# Design System: Mimir

## Overview

**Creative North Star: "The Flight Recorder"**

Mimir reconstructs agent work after the fact. Its interface should feel like opening a precise instrument: chronological, legible, and grounded in evidence. Session summaries establish what happened; requests, files, models, providers, and errors provide the supporting record.

The system is technical, quiet, and exact. It explicitly rejects generic SaaS dashboard composition, decorative color blending, pill navigation, interchangeable metric-card walls, and oversized marketing hierarchy. The pixel-art wordmark is the only ornamental brand element.

**Key Characteristics:**
- Sessions are the primary object and default arrival surface.
- Dense information uses rules and alignment, not nested cards.
- Controls are compact, mechanical, and familiar.
- Accent color is rare and state-bearing.
- Light and dark themes preserve the same hierarchy.

## Colors

The palette is neutral and material-like, with one deep teal action color taken from the wordmark's family without washing entire surfaces in it.

### Primary
- **Instrument Teal:** Reserved for links, focus, current selection, and explicit action. It must never become a decorative background wash.

### Neutral
- **Recorder Canvas:** The app background, visually distinct from true-white working surfaces.
- **Working Surface:** Tables, controls, and overlays.
- **Carbon Ink:** Primary text and selected high-contrast controls.
- **Graphite Note:** Secondary metadata that still meets WCAG AA.
- **Hairline Rule:** The primary layout device between records and regions.
- **Night Canvas / Night Surface / Night Ink / Night Rule:** Dark-theme equivalents with preserved contrast and no blue-purple cast.

### Named Rules

**The One Signal Rule.** Teal communicates interaction or current state only. If teal appears where nothing can be acted on or selected, remove it.

**The No Blend Rule.** Never use gradients, translucent teal washes, glowing edges, or color-mixed surfaces as atmosphere.

## Typography

**Display Font:** IBM Plex Sans (with system sans fallback)<br>
**Body Font:** IBM Plex Sans (with system sans fallback)<br>
**Label/Mono Font:** IBM Plex Mono for identifiers, timestamps, token counts, and raw evidence only.

**Character:** IBM Plex Sans is humanist enough to avoid sterile infrastructure-tool sameness while remaining technical and compact. Monospace is evidence notation, not the product voice.

### Hierarchy
- **Headline** (600, 28px, 1.2): Page names and session titles only.
- **Title** (600, 16px, 1.3): Panel and grouped evidence headings.
- **Body** (400, 14px, 1.5): Explanations and table values.
- **Label** (500, 13px, 1.3): Controls, navigation, and metadata labels.
- **Mono** (400, 12px, 1.4): IDs, exact timestamps, model identifiers, and machine values.

### Named Rules

**The Evidence Mono Rule.** Use monospace only when the value benefits from character-level inspection. Navigation, headings, and prose always use IBM Plex Sans.

## Elevation

The system is flat and divided. Borders, spacing, and tonal changes establish structure. Shadows are prohibited on resting tables, navigation, and summary regions; only temporary overlays may use a broad low-opacity shadow.

### Shadow Vocabulary
- **Overlay separation** (`0 18px 50px rgba(0,0,0,0.18)`): Detail sheets and menus only.

### Named Rules

**The Flat Record Rule.** Persistent information sits on one plane. If a screen becomes a grid of floating rectangles, the hierarchy is wrong.

## Components

### Buttons
- **Shape:** Compact mechanical corners (5px radius), never pills.
- **Primary:** Carbon-ink fill with white text for decisive actions; teal is reserved for focus and links.
- **Hover / Focus:** One tonal step on hover and a visible 2px teal focus outline.
- **Secondary / Ghost:** White or transparent with a single hairline border.

### Chips
- **Style:** Small bordered labels for status and filters, with compact 5px corners rather than fully rounded capsules.
- **State:** Selected filters invert to carbon ink; outcome labels use exactly Landed, Discarded, Abandoned, or Unresolved and retain restrained semantic color plus an icon.

### Cards / Containers
- **Corner Style:** Slightly eased panels (7px radius).
- **Background:** Working Surface against Recorder Canvas.
- **Shadow Strategy:** No shadow at rest.
- **Border:** One Hairline Rule around true containers; internal records use separators.
- **Internal Padding:** 16px or 24px depending on density.

### Inputs / Fields
- **Style:** White surface, one hairline rule, 5px radius, 34px compact height.
- **Focus:** Instrument Teal outline with no glow.
- **Error / Disabled:** Explicit text and icon state; never color alone.

### Navigation
- Primary navigation uses one compact header inside the same maximum width as page content. The wordmark anchors the left, plain route links follow it, and utilities sit at the right. One bottom rule separates navigation from content. The active route uses a small square teal signal, never an underline, pill, or second navigation band.

### Session Record
- A session row leads with repository and intent, then work outcome, capture summary, recency, app/model evidence, and token totals. Capture uses explicit Empty, Pending, Saved, Failed, or Partial text with counts; it never borrows outcome color or wording. One click opens the complete session reconstruction. Request rows remain subordinate evidence within that flow.
- Session detail may show outcome source, reason, and timestamp as audit metadata. Unresolved means no evidenced result has been recorded; it does not mean capture failed.

## Do's and Don'ts

### Do:
- **Do** make Sessions the default arrival route and strongest navigation item.
- **Do** use 1px rules, exact alignment, and compact spacing to create hierarchy.
- **Do** expose requests, files, errors, and model/provider details one click from a session.
- **Do** preserve equal information hierarchy in light and dark themes.
- **Do** keep the wordmark as the sole pixel-art treatment.

### Don't:
- **Don't** resemble a generic SaaS dashboard: no pill navigation, KPI card walls, gradients, or decorative color wash.
- **Don't** make the OpenRouter-style activity table the center of Mimir; it is supporting evidence.
- **Don't** use teal for decoration or blend it into neutral surfaces.
- **Don't** use oversized marketing typography inside the product.
- **Don't** use border-left or border-right greater than 1px as a colored accent.
- **Don't** use monospace across the whole interface or imitate a terminal.
