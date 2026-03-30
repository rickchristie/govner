# Cooper TUI Design Document

> "Barrel-proof containers for undiluted AI."

This document specifies every screen, component, animation, color, and keybinding for the Cooper TUI.
It is implementation-ready: an engineer can build each screen from this spec without ambiguity.

---

## Table of Contents

1. [Color Palette](#1-color-palette)
2. [Typography and Icons](#2-typography-and-icons)
3. [Shared Components](#3-shared-components)
4. [Screen 1: Loading Screen (Startup)](#screen-1-loading-screen-startup)
5. [Screen 2: Main Tab Bar](#screen-2-main-tab-bar)
6. [Screen 3: Containers Tab](#screen-3-containers-tab)
7. [Screen 4: Proxy Monitor Tab](#screen-4-proxy-monitor-tab)
8. [Screen 5: Proxy Blocked Tab](#screen-5-proxy-blocked-tab)
9. [Screen 6: Proxy Allowed Tab](#screen-6-proxy-allowed-tab)
10. [Screen 7: Execution Bridge Logs Tab](#screen-7-execution-bridge-logs-tab)
11. [Screen 8: Execution Bridge Routes Tab](#screen-8-execution-bridge-routes-tab)
12. [Screen 9: Configure Tab](#screen-9-configure-tab)
13. [Screen 10: About Tab](#screen-10-about-tab)
14. [Screen 11: Exit Confirmation Modal](#screen-11-exit-confirmation-modal)
15. [Screen 12: Shutdown Screen](#screen-12-shutdown-screen)
16. [Screen 13: Configure Wizard -- Welcome](#screen-13-configure-wizard-welcome)
17. [Screen 14: Configure Wizard -- Programming Tool Setup](#screen-14-configure-wizard-programming-tool-setup)
18. [Screen 15: Configure Wizard -- AI CLI Tool Setup](#screen-15-configure-wizard-ai-cli-tool-setup)
19. [Screen 16: Configure Wizard -- Proxy Whitelist Setup](#screen-16-configure-wizard-proxy-whitelist-setup)
20. [Screen 17: Configure Wizard -- Port Forwarding Setup](#screen-17-configure-wizard-port-forwarding-setup)
21. [Screen 18: Configure Wizard -- Proxy Setup](#screen-18-configure-wizard-proxy-setup)
22. [Screen 19: Configure Wizard -- Save and Build](#screen-19-configure-wizard-save-and-build)
23. [Animation Reference Appendix](#animation-reference-appendix)
24. [Go Implementation Notes](#go-implementation-notes)

---

## 1. Color Palette

The palette draws from whiskey, charred oak, copper, and amber. Dark backgrounds with warm accent
tones evoke a rickhouse at dusk. Cool blues and greens are used sparingly for status and information
contrast.

### Base Colors

| Name             | Hex       | Usage                                              |
|------------------|-----------|----------------------------------------------------|
| `Charcoal`       | `#0c0e12` | Primary background (terminal default override)     |
| `OakDark`        | `#151920` | Surface background (panels, cards, panes)          |
| `OakMid`         | `#1e2530` | Elevated surface (selected row bg, active pane)    |
| `OakLight`       | `#283040` | Borders, dividers, subtle separators               |
| `Stave`          | `#3a4556` | Disabled elements, inactive borders                |

### Text Colors

| Name             | Hex       | Usage                                              |
|------------------|-----------|----------------------------------------------------|
| `Parchment`      | `#e8e0d4` | Primary text (headings, important content)         |
| `Linen`          | `#c4b8a8` | Secondary text (body, descriptions)                |
| `Dusty`          | `#8a7e70` | Tertiary text (timestamps, labels, help text)      |
| `Faded`          | `#5c5248` | Ghost text (placeholders, disabled, dimmed bg)     |
| `Void`           | `#0c0e12` | Text on bright backgrounds (inverted)              |

### Accent Colors -- Warm

| Name             | Hex       | Usage                                              |
|------------------|-----------|----------------------------------------------------|
| `Amber`          | `#f0a830` | Primary accent (brand, active tab, selected items) |
| `Copper`         | `#d4783c` | Secondary accent (warnings, timer mid-range)       |
| `Barrel`         | `#b85c28` | Tertiary accent (deep warning, aged elements)      |
| `Flame`          | `#e84820` | Danger (deny, error, critical timer)               |
| `Wheat`          | `#e8d8a0` | Highlights on warm backgrounds                     |

### Accent Colors -- Cool

| Name             | Hex       | Usage                                              |
|------------------|-----------|----------------------------------------------------|
| `Proof`          | `#58c878` | Success, approved, healthy, running                |
| `Verdigris`      | `#48a89c` | Info, links, secondary success                     |
| `SlateBlue`      | `#6888b8` | Neutral info, method badges, counts                |
| `Mist`           | `#889cb8` | Subtle info text, secondary labels                 |

### Timer Gradient (for countdown bars)

| Time Remaining   | Color     | Hex       |
|------------------|-----------|-----------|
| > 80%            | `Proof`   | `#58c878` |
| 60-80%           | `Amber`   | `#f0a830` |
| 40-60%           | `Copper`  | `#d4783c` |
| 20-40%           | `Barrel`  | `#b85c28` |
| < 20%            | `Flame`   | `#e84820` |

### Semantic Aliases

```
ColorBg          = Charcoal     #0c0e12
ColorSurface     = OakDark      #151920
ColorSurfaceHi   = OakMid       #1e2530
ColorBorder      = OakLight     #283040
ColorBorderDim   = Stave        #3a4556

ColorText        = Parchment    #e8e0d4
ColorTextSec     = Linen        #c4b8a8
ColorTextDim     = Dusty        #8a7e70
ColorTextGhost   = Faded        #5c5248

ColorAccent      = Amber        #f0a830
ColorWarn        = Copper       #d4783c
ColorDanger      = Flame        #e84820
ColorOK          = Proof        #58c878
ColorInfo        = SlateBlue    #6888b8
```

---

## 2. Typography and Icons

### Unicode Characters

```
Brand
  BarrelEmoji     = "🥃"
  CaskEmoji       = "📦"

Status Icons
  IconCheck       = "✓"
  IconCross       = "✗"
  IconWarn        = "⚠"
  IconArrowRight  = "▶"
  IconArrowDown   = "▼"
  IconDot         = "●"
  IconDotEmpty    = "○"
  IconBlock       = "█"
  IconBlockHalf   = "▓"
  IconShade       = "░"
  IconLock        = "🔒"
  IconUnlock      = "🔓"
  IconShield      = "🛡️"
  IconPlug        = "🔌"
  IconGear        = "⚙"
  IconTimer       = "⏱"

Box Drawing
  BorderH         = "─"
  BorderV         = "│"
  BorderTL        = "┌"
  BorderTR        = "┐"
  BorderBL        = "└"
  BorderBR        = "┘"
  BorderTee       = "┬"
  BorderBTee      = "┴"
  BorderLTee      = "├"
  BorderRTee      = "┤"
  BorderCross     = "┼"
  BorderHBold     = "━"
  BorderVBold     = "┃"

Tab Indicators
  TabActive       = "━"     (bold horizontal, used under active tab)
  TabInactive     = "─"     (thin horizontal, under inactive tabs)

Progress Bar Characters
  ProgressFull    = "━"
  ProgressEmpty   = "─"
  ProgressTip     = "╸"     (half-block at leading edge during animation)
```

---

## 3. Shared Components

### 3.1 Tab Bar

The tab bar spans the full terminal width. The active tab name is rendered in `Amber` with a bold
underline (`━`). Inactive tabs are rendered in `Dusty` with thin underline (`─`). Tabs are
navigated with number keys (1-8) or Tab/Shift-Tab to cycle.

```
  Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
  ━━━━━━━━━━  ───────  ───────  ───────  ───────────  ──────  ─────────  ─────
```

Active tab text: `Amber` (#f0a830), bold.
Active underline: `Amber` (#f0a830), `━` repeated to match tab text width.
Inactive tab text: `Dusty` (#8a7e70).
Inactive underline: `OakLight` (#283040), `─` repeated.

#### Sub-tab Bar

Used inside Proxy and Bridge sections. Same pattern but indented 2 spaces, uses `Verdigris`
(#48a89c) for the active sub-tab.

```
    Monitor  Blocked  Allowed
    ━━━━━━━  ───────  ───────
```

#### Tab Layout

The `cooper up` TUI groups screens under these top-level tabs:

| #   | Tab Label    | Key | Content                              |
|-----|-------------|-----|--------------------------------------|
| 1   | Containers  | 1   | Container list with stats            |
| 2   | Monitor     | 2   | Proxy monitor (approve/deny)         |
| 3   | Blocked     | 3   | Blocked request history              |
| 4   | Allowed     | 4   | Allowed request history              |
| 5   | Bridge Logs | 5   | Execution bridge logs                |
| 6   | Routes      | 6   | Bridge route management              |
| 7   | Configure   | 7   | Runtime settings                     |
| 8   | About       | 8   | Versions, status                     |

### 3.2 Header Bar

The header sits above the tab bar on a single line. It shows the brand, active container count,
and proxy status.

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers  ⏱ 3 pending
```

- `🥃 Cooper`: `Amber` bold.
- `barrel-proof`: `Dusty` italic.
- `🛡️ Proxy ✓`: `Proof` if proxy is healthy, `Flame` + `✗` if unhealthy.
- `📦 2 containers`: `Parchment`. Count of running CLI containers.
- `⏱ 3 pending`: `Copper` bold. Only shown when pending requests exist. Pulses (see animations).

### 3.3 Help Bar (Footer)

Fixed to the bottom line. Shows context-sensitive keybindings.

```
[q Quit]  [1-8 Tabs]  [↑↓ Navigate]  [a Approve]  [d Deny]       🥃
```

Format: `[` + key in `Amber` + ` ` + description in `Dusty` + `]`.
The `🥃` is always at the far right as a brand anchor.

### 3.4 Modal Dialog

Centered overlay on a dimmed background. Uses double-border box in `Amber`.

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🥃 Exit Cooper?                    ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   This will stop the proxy and all   ║
              ║   containers. AI sessions             ║
              ║   will lose network access.           ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Confirm]    [Esc Cancel]  ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

- Border: `Amber` (#f0a830), double-line (`╔═╗║╚═╝`).
- Title: `Parchment` bold.
- Divider: `OakLight` (#283040), `─` repeated.
- Body: `Linen` (#c4b8a8).
- Confirm button: `Proof` (#58c878) bold.
- Cancel button: `Dusty` (#8a7e70).
- Background dimming: all text behind the modal is re-rendered with `Faded` (#5c5248) foreground.
- Modal width: 46 characters inner, 50 with border.
- Padding: 2 characters horizontal inside border, 1 line vertical.

### 3.5 Countdown Timer Bar

Used in the Proxy Monitor for pending requests. Rendered inline as a horizontal bar that depletes
left-to-right.

```
[━━━━━━━━━━━━━━━━━━━━] 5.0s     (full, green)
[━━━━━━━━━━━━━━──────] 3.5s     (depleting, amber)
[━━━━━━────────────── ] 1.2s     (critical, red)
[──────────────────── ] 0.0s     (expired)
```

- Bar width: 20 characters.
- Filled: `ProgressFull` (`━`) in the timer gradient color for current percentage.
- Empty: `ProgressEmpty` (`─`) in `OakLight` (#283040).
- Time label: same color as filled portion, right of bar, formatted `X.Xs`.
- Update interval: every 100ms for smooth depletion.

### 3.6 Scrollable List

Vertical list with cursor selection. Selected row has `OakMid` (#1e2530) background with `Amber`
arrow indicator. Scroll indicator appears at right edge when list exceeds visible area.

```
  ▶ Selected row text                               ▲
    Normal row text                                  █
    Normal row text                                  █
    Normal row text                                  ░
    Normal row text                                  ░
    Normal row text                                  ▼
```

- Arrow: `▶` in `Amber`, only on selected row.
- Selected row: full-width `OakMid` background, text in `Parchment`.
- Normal rows: no background, text in `Linen`.
- Scroll track: `░` in `Stave` (#3a4556).
- Scroll thumb: `█` in `Dusty` (#8a7e70).
- Scroll arrows: `▲` `▼` in `Dusty`.
- Visible height: terminal height minus header(1) - tab bar(2) - footer(1) - content padding.

### 3.7 Progress Bar

Horizontal bar with optional percentage and label. Used in loading screens and container stats.

```
  ━━━━━━━━━━━━━━━━━━━━━━━━────────── 72%
```

- Width: configurable (default 30 characters for loading, 10 for inline stats).
- Filled: `━` in specified color (default `Amber`).
- Tip: `╸` in lighter shade at leading edge (optional, for animated version).
- Empty: `─` in `OakLight` (#283040).
- Percentage: same color as filled, right of bar.
- Staggered animation: display progress trails actual by 15% increments at 80ms intervals.

### 3.8 Detail Pane

Used for request details (Monitor, Blocked, Allowed tabs). Right-side panel with labeled fields.

```
┌─ Request Detail ─────────────────────────┐
│                                          │
│  URL     https://api.example.com/v1/chat │
│  Method  POST                            │
│  Source  barrel-myproject                 │
│  Time    14:32:05                        │
│                                          │
│  ── Headers ──────────────────────────── │
│  Content-Type   application/json         │
│  Authorization  Bearer sk-...redacted    │
│  User-Agent     claude-code/1.2.3        │
│                                          │
└──────────────────────────────────────────┘
```

- Border: `OakLight` (#283040), single-line.
- Title: `Linen` (#c4b8a8) bold, embedded in top border.
- Labels: `Dusty` (#8a7e70), right-aligned within label column (8 chars wide).
- Values: `Parchment` (#e8e0d4).
- Method badge: colored background chip:
  - GET: `SlateBlue` (#6888b8) bg, `Void` text.
  - POST: `Amber` (#f0a830) bg, `Void` text.
  - PUT: `Copper` (#d4783c) bg, `Void` text.
  - DELETE: `Flame` (#e84820) bg, `Parchment` text.
  - PATCH: `Verdigris` (#48a89c) bg, `Void` text.
- Section headers (e.g., `── Headers ──`): `Dusty` with `─` fill.

### 3.9 Text Input Field

Used in configure screens for domain entry, version entry, port numbers.

```
  Domain: [api.example.com         ]
```

- Bracket: `OakLight` (#283040).
- Active bracket: `Amber` (#f0a830).
- Input text: `Parchment` (#e8e0d4).
- Placeholder: `Faded` (#5c5248) italic.
- Cursor: block cursor alternating `Amber`/`OakDark` at 530ms interval.
- Field width: configurable, typically 30 characters.
- Error state: bracket becomes `Flame`, error message below in `Flame` italic.

### 3.10 Toggle/Checkbox

```
  [●] Include subdomains          (on)
  [○] Include subdomains          (off)
```

- On: `●` in `Amber` (#f0a830), label in `Parchment`.
- Off: `○` in `Dusty` (#8a7e70), label in `Linen`.
- Square brackets: `OakLight` (#283040).

### 3.11 Selection Buttons (Radio Group)

Used for version mode selection (Mirror / Latest / Pin).

```
  ▶ ● Mirror   (v1.22.4 from host)
    ○ Latest   (v1.22.5 available)
    ○ Pin      [enter version]
```

- Selected option: `●` in `Amber`, label in `Parchment` bold.
- Unselected: `○` in `Dusty`, label in `Linen`.
- Hint text in parentheses: `Dusty`.
- Navigation arrow `▶` on focused option: `Amber`.

---

## Screen 1: Loading Screen (Startup)

### Purpose
Displayed when `cooper up` starts. Shows progress of proxy startup, network creation, and SSL
setup. Vertically centered, full terminal.

### ASCII Mockup

```
                          (vertical centering padding)


                       · 🥃 ·


                    c o o p e r

                  rolling out the barrel...


               ━━━━━━━━━━━━━━╸────────────── 52%


               ✓ Docker networks created
               ✓ SSL certificates loaded
               · Proxy container starting...
               · CLI image version check...


               [q Cancel]                      🥃


                          (vertical centering padding)
```

### Color Specification

| Element                  | Color            | Hex       |
|--------------------------|------------------|-----------|
| `🥃` emoji flanking      | (native)         | --        |
| Dot animation `·`        | `Dusty`          | #8a7e70   |
| Title `c o o p e r`      | `Amber` bold     | #f0a830   |
| Subtitle                 | `Dusty`          | #8a7e70   |
| Progress bar filled      | `Amber`          | #f0a830   |
| Progress bar tip `╸`     | `Wheat`          | #e8d8a0   |
| Progress bar empty       | `OakLight`       | #283040   |
| Percentage               | `Amber`          | #f0a830   |
| Completed step `✓`       | `Proof`          | #58c878   |
| Completed step text      | `Linen`          | #c4b8a8   |
| In-progress step `·`     | `Amber` pulsing  | #f0a830   |
| In-progress step text    | `Dusty`          | #8a7e70   |
| Help key                 | `Amber`          | #f0a830   |
| Help desc                | `Dusty`          | #8a7e70   |
| Brand anchor `🥃`        | (native)         | --        |

### Animation Details

**Barrel Roll Animation** (flanking the emoji)
- Dot pattern cycles around the barrel emoji.
- 4 frames at 150ms interval (600ms full cycle):

```
Frame 0:  · 🥃 ·
Frame 1:  · 🥃 · ·
Frame 2:  · 🥃 · · ·
Frame 3:  · 🥃 · ·
```

- Dots use `Dusty` (#8a7e70).

**Progress Bar Stagger**
- Target progress is set by actual events (network created = 25%, SSL loaded = 50%, etc.).
- Display progress chases target in 15% increments at 80ms intervals.
- Tip character `╸` appears at leading edge during chase, replaced by `━` when settled.
- On reaching 100%, holds for 800ms then transitions to main view.

**Step Completion Transition**
- When a step completes: its `·` morphs to `✓` in `Proof`.
- Text color transitions from `Dusty` to `Linen` instantaneously on completion.

**Subtitle Variations by State**

| State              | Subtitle text                        |
|--------------------|--------------------------------------|
| Starting           | `rolling out the barrel...`          |
| Version mismatch   | `checking barrel contents...`        |
| Error              | `the barrel sprung a leak`           |
| Complete           | `barrel-proof and ready`             |
| Shutdown (reuse)   | `sealing the barrel...`              |

### Loading Steps

| Step                        | Progress Target | Message                          |
|-----------------------------|-----------------|----------------------------------|
| Init                        | 0%              | --                               |
| Creating Docker networks    | 15%             | `Creating cooper networks...`    |
| Starting proxy container    | 35%             | `Proxy container starting...`    |
| SSL certificate validation  | 50%             | `SSL certificates loaded`        |
| Starting execution bridge   | 65%             | `Execution bridge starting...`   |
| CLI image version check     | 80%             | `CLI image version check...`     |
| ACL socket ready            | 95%             | `ACL listener ready`             |
| Ready                       | 100%            | `barrel-proof and ready`         |

### Key Bindings

| Key       | Action                                              |
|-----------|-----------------------------------------------------|
| `q`       | Cancel startup and quit (prompts if work has begun) |
| `Ctrl+C`  | Same as `q`                                         |

### State Transitions

| From            | Trigger                    | To                     |
|-----------------|----------------------------|------------------------|
| (entry)         | `cooper up` invoked        | Loading screen         |
| Loading screen  | All steps complete, 100%   | Main view (Containers) |
| Loading screen  | `q` pressed                | Quit                   |
| Loading screen  | Step fails                 | Error state (stays)    |

### Error State

When a step fails, the loading screen remains visible:
- Progress bar turns `Flame` (#e84820).
- Failed step shows `✗` in `Flame` with error message.
- Subtitle changes to `the barrel sprung a leak`.
- Help bar changes to `[q Quit]  [r Retry]`.

```
               ✓ Docker networks created
               ✓ SSL certificates loaded
               ✗ Proxy container failed to start
                 Error: port 3128 already in use

               [q Quit]  [r Retry]             🥃
```

---

## Screen 2: Main Tab Bar

### Purpose
Top-level navigation for the `cooper up` control panel. Always visible on the top 3 lines
(header + tab bar + tab underline).

### ASCII Mockup (Full Header Region)

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers  ⏱ 3 pending
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ━━━━━━━━━━  ───────  ───────  ───────  ───────────  ──────  ─────────  ─────
```

### Color Specification

Line 1 (header): See Section 3.2.
Line 2 (divider): `OakLight` (#283040), `─` full terminal width.
Line 3 (tab names): active tab in `Amber` bold, inactive in `Dusty`.
Line 4 (underlines): active in `Amber` `━`, inactive in `OakLight` `─`.

### Tab Notification Badges

Tabs can show notification badges for activity:

```
 Monitor (3)        -- 3 pending requests, badge in Copper
 Blocked +2         -- 2 new blocks since last viewed, badge in Flame
 Bridge Logs +5     -- 5 new log entries, badge in Verdigris
```

- Badge count: shown in parentheses or with `+` prefix.
- Badge color: varies by urgency (see table in each tab spec).
- Badge clears when user switches to that tab.

### Key Bindings

| Key          | Action                            |
|--------------|-----------------------------------|
| `1` - `8`    | Jump to tab by number             |
| `Tab`        | Next tab                          |
| `Shift+Tab`  | Previous tab                      |
| `q`          | Open exit confirmation modal      |
| `Ctrl+C`     | Same as `q`                       |

### State Transitions

| From      | Trigger             | To          |
|-----------|---------------------|-------------|
| Loading   | Startup complete    | Tab bar + Containers tab (default) |
| Any tab   | Number key / Tab    | Target tab  |
| Any tab   | `q`                 | Exit modal  |

---

## Screen 3: Containers Tab

### Purpose
Lists all running Cooper containers (proxy + CLI containers) with resource stats and lifecycle
controls.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ━━━━━━━━━━  ───────  ───────  ───────  ───────────  ──────  ─────────  ─────

 NAME                STATUS    CPU    MEM      UPTIME     NETWORK
 ─────────────────────────────────────────────────────────────────────────────
 ▶ cooper-proxy      ● Run     2.1%   48 MB    2h 14m     external+internal
   barrel-myproject  ● Run     8.4%   312 MB   1h 02m     internal
   barrel-backend    ● Run     3.2%   156 MB   0h 45m     internal



 [q Quit]  [↑↓ Nav]  [s Stop]  [r Restart]  [Enter Detail]              🥃
```

### Color Specification

| Element               | Color            | Hex       |
|-----------------------|------------------|-----------|
| Column headers        | `Dusty` bold     | #8a7e70   |
| Divider line          | `OakLight`       | #283040   |
| Selection arrow `▶`   | `Amber`          | #f0a830   |
| Selected row bg       | `OakMid`         | #1e2530   |
| Container name        | `Parchment`      | #e8e0d4   |
| `● Run` (running)     | `Proof`          | #58c878   |
| `● Stop` (stopped)    | `Flame`          | #e84820   |
| `● Start` (starting)  | `Amber` pulsing  | #f0a830   |
| CPU percentage        | `Linen`          | #c4b8a8   |
| CPU > 80%             | `Copper`         | #d4783c   |
| Memory                | `Linen`          | #c4b8a8   |
| Uptime                | `Dusty`          | #8a7e70   |
| Network label         | `Mist`           | #889cb8   |

### Key Bindings

| Key       | Action                               |
|-----------|--------------------------------------|
| `↑` / `k` | Move selection up                   |
| `↓` / `j` | Move selection down                 |
| `s`       | Stop selected container              |
| `r`       | Restart selected container           |
| `S`       | Start selected (if stopped)          |
| `Enter`   | Show container detail (expanded row) |
| `1`-`8`   | Switch tab                           |
| `q`       | Exit confirmation                    |

### Container Detail (Expanded)

When Enter is pressed, the selected row expands to show additional info:

```
 ▶ cooper-proxy      ● Run     2.1%   48 MB    2h 14m     external+internal
   ├─ Image:    cooper-proxy:latest
   ├─ ID:       a3f1b2c4d5e6
   ├─ Networks: cooper-external, cooper-internal
   ├─ Ports:    3128 (squid), 4343 (bridge relay)
   └─ Started:  2025-03-27 14:32:05
```

- Tree lines `├─` `└─` in `OakLight` (#283040).
- Labels in `Dusty`.
- Values in `Linen`.

### Empty State

```
                        🥃

             No containers running.

          Run  cooper cli  to start a container.

 [q Quit]                                                                 🥃
```

- `🥃` centered, large.
- Message in `Dusty` italic, centered.
- Command in `Amber` (inline code style).

### Error State

If Docker stats query fails:

```
 ⚠ Unable to fetch container stats
   Error: Docker daemon not responding
   Retrying in 5s...
```

- `⚠` in `Copper`.
- Error text in `Linen`.
- Retry message in `Dusty`.
- Auto-retry every 5 seconds.

### Animation Details

**Status Pulse (Starting/Stopping)**
- The `●` dot pulses when a container is transitioning.
- 3 frames at 300ms interval:

```
Frame 0: ● (Amber #f0a830)
Frame 1: ● (Copper #d4783c, dimmer)
Frame 2: ● (Amber #f0a830, brighter)
```

**CPU/Mem Refresh**
- Stats update every 2 seconds via `docker stats --no-stream`.
- New values fade in: first frame in `Dusty`, second frame in target color. Duration: 200ms.

### State Transitions

| From          | Trigger            | To                |
|---------------|--------------------|-------------------|
| Tab switch    | Press `1`          | Containers tab    |
| Containers    | `s` on container   | Stop confirmation |
| Containers    | Container stopped  | Row updates       |

---

## Screen 4: Proxy Monitor Tab

### Purpose
Real-time approval UI for non-whitelisted network requests. Two-pane layout: left pane lists
pending requests sorted by urgency (least time remaining at top), right pane shows details of the
selected request.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers  ⏱ 3 pending
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor (3)  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ──────────  ━━━━━━━━━━━  ───────  ───────  ───────────  ──────  ─────────  ─────

 Pending Requests                    │ Request Detail
 ────────────────────────────────────│──────────────────────────────────────────
                                     │
 ▶ api.npmjs.org        [━━━━━─────] │  URL     https://api.npmjs.org/lodash
   1.2s  barrel-myproject       POST │  Method  POST
                                     │  Source  barrel-myproject
   registry.yarnpkg.com [━━━━━━━━──] │  Time    14:32:05
   3.8s  barrel-backend          GET │
                                     │  ── Headers ──────────────────────────
   uploads.github.com   [━━━━━━━━━━] │  Content-Type   application/json
   4.9s  barrel-myproject       POST │  User-Agent     claude-code/1.2.3
                                     │  Accept         */*
                                     │
                                     │
                                     │
                                     │
                                     │
 ─────────────────────────────────────────────────────────────────────────────
 [a Approve]  [d Deny]  [↑↓ Nav]  [q Quit]                               🥃
```

### Layout

- Left pane: 44% of terminal width.
- Vertical divider: `│` in `OakLight` (#283040).
- Right pane: remaining width.
- Each pending request in left pane takes 2 rows:
  - Row 1: domain name (left-aligned), countdown bar (right-aligned).
  - Row 2: time remaining, source container, HTTP method badge (right-aligned).
- Requests sorted by time remaining ascending (most urgent at top).

### Color Specification

| Element                     | Color               | Hex       |
|-----------------------------|---------------------|-----------|
| Left pane header            | `Linen` bold        | #c4b8a8   |
| Right pane header           | `Linen` bold        | #c4b8a8   |
| Vertical divider            | `OakLight`          | #283040   |
| Horizontal dividers         | `OakLight`          | #283040   |
| Selected request bg         | `OakMid`            | #1e2530   |
| Selection arrow             | `Amber`             | #f0a830   |
| Domain name (selected)      | `Parchment`         | #e8e0d4   |
| Domain name (unselected)    | `Linen`             | #c4b8a8   |
| Time remaining (safe)       | `Proof`             | #58c878   |
| Time remaining (warning)    | `Copper`            | #d4783c   |
| Time remaining (critical)   | `Flame`             | #e84820   |
| Container source            | `Mist`              | #889cb8   |
| Method badge GET            | `SlateBlue` bg      | #6888b8   |
| Method badge POST           | `Amber` bg          | #f0a830   |
| Detail labels               | `Dusty`             | #8a7e70   |
| Detail values               | `Parchment`         | #e8e0d4   |
| Detail section header       | `Dusty`             | #8a7e70   |

### Key Bindings

| Key       | Action                                        |
|-----------|-----------------------------------------------|
| `↑` / `k` | Move selection up in pending list             |
| `↓` / `j` | Move selection down in pending list           |
| `a`       | Approve selected request                      |
| `Enter`   | Same as `a` (approve)                         |
| `d`       | Deny selected request immediately             |
| `A`       | Approve all currently pending requests        |
| `1`-`8`   | Switch tab                                    |
| `q`       | Exit confirmation                             |

### Countdown Timer Behavior

- Default timeout: 5 seconds (configurable in Configure tab).
- Timer bar updates every 100ms.
- Color transitions through timer gradient (see Section 1).
- When timer reaches 0: request is automatically denied, removed from pending list with a brief
  flash (row background flashes `Flame` for 150ms then fades out).
- Denied-by-timeout requests move to the Blocked tab with reason `timeout`.

### Approval/Deny Transitions

**On Approve (`a`):**
1. Selected row background flashes `Proof` (#58c878) for 200ms.
2. Row slides out to the right (3 frames at 100ms: indent +4, +8, removed).
3. Request appears in Allowed tab.
4. Selection moves to next item (or previous if at bottom).

**On Deny (`d`):**
1. Selected row background flashes `Flame` (#e84820) for 200ms.
2. Row slides out to the left (3 frames at 100ms: indent -4, -8, removed).
3. Request appears in Blocked tab with reason `manual`.
4. Selection moves to next item.

### Empty State (No Pending Requests)

```
 Pending Requests                    │ Request Detail
 ────────────────────────────────────│──────────────────────────────────────────
                                     │
                                     │
                                     │
            🛡️                        │
                                     │       No request selected.
     All clear. No pending           │
     requests to review.             │
                                     │
                                     │
                                     │
```

- Left pane: `🛡️` centered, message in `Dusty` italic.
- Right pane: `No request selected.` in `Dusty` italic.

### Error State

If ACL socket disconnects:

```
 ⚠ Proxy monitor disconnected
   The ACL socket at ~/.cooper/run/acl.sock is not responding.
   Requests will be denied automatically (fail-closed).
   Reconnecting...
```

- `⚠` in `Flame`.
- Message in `Linen`.
- Auto-reconnect attempts every 2 seconds.

---

## Screen 5: Proxy Blocked Tab

### Purpose
History of blocked network requests (both auto-denied by timeout and manually denied). Scrollable
list with detail view.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked (47)  Allowed  Bridge Logs  Routes  Configure  About
 ──────────  ───────  ━━━━━━━━━━━━  ───────  ───────────  ──────  ─────────  ─────

 TIME      DOMAIN                      SOURCE             METHOD  REASON
 ──────────────────────────────────────────────────────────────────────────────
 ▶ 14:35:02  registry.npmjs.org        barrel-myproject   GET     timeout
   14:34:58  pypi.org                  barrel-backend     GET     denied
   14:34:41  evil.example.com          barrel-myproject   POST    denied
   14:33:12  downloads.sourceforge.net barrel-myproject   GET     timeout
   14:32:55  cdn.jsdelivr.net          barrel-backend     GET     timeout

 ┌─ Blocked Detail ──────────────────────────────────────────────────────────┐
 │                                                                          │
 │  URL      https://registry.npmjs.org/lodash                              │
 │  Method   GET                                                            │
 │  Source   barrel-myproject                                               │
 │  Time     14:35:02                                                       │
 │  Reason   timeout (5.0s expired)                                         │
 │                                                                          │
 │  ── Request Headers ─────────────────────────────────────────────────── │
 │  User-Agent     claude-code/1.2.3                                        │
 │  Accept         application/json                                         │
 │                                                                          │
 └──────────────────────────────────────────────────────────────────────────┘

 [↑↓ Nav]  [Enter Detail]  [Esc Close Detail]  [q Quit]                   🥃
```

### Layout

- Top section: scrollable list of blocked requests.
- Bottom section: detail pane for selected request (shown on `Enter`, hidden on `Esc`).
- List takes full width. When detail is open, list compresses to top half, detail takes bottom half.
- When detail is closed, list takes full content area.

### Color Specification

| Element               | Color            | Hex       |
|-----------------------|------------------|-----------|
| Column headers        | `Dusty` bold     | #8a7e70   |
| Time column           | `Dusty`          | #8a7e70   |
| Domain (selected)     | `Parchment`      | #e8e0d4   |
| Domain (unselected)   | `Linen`          | #c4b8a8   |
| Source container      | `Mist`           | #889cb8   |
| Method badge          | (see 3.8)        | --        |
| Reason `timeout`      | `Copper`         | #d4783c   |
| Reason `denied`       | `Flame`          | #e84820   |
| Detail pane border    | `OakLight`       | #283040   |
| Detail title          | `Flame` bold     | #e84820   |
| Tab badge count       | `Flame`          | #e84820   |

### Key Bindings

| Key        | Action                       |
|------------|------------------------------|
| `↑` / `k`  | Move selection up           |
| `↓` / `j`  | Move selection down         |
| `Enter`    | Toggle detail pane          |
| `Esc`      | Close detail pane           |
| `G`        | Jump to bottom (newest)     |
| `g`        | Jump to top (oldest visible)|
| `1`-`8`    | Switch tab                  |
| `q`        | Exit confirmation           |

### Empty State

```
                     🛡️

           No blocked requests yet.

    All network requests have been approved
    or no requests have been made.
```

- Centered, `Dusty` italic.

### State Transitions

| From       | Trigger                | To                    |
|------------|------------------------|-----------------------|
| Tab switch | Press `3`              | Blocked tab           |
| Blocked    | Request denied/timeout | New row appears at top|
| Blocked    | `Enter`                | Detail pane opens     |
| Blocked    | `Esc`                  | Detail pane closes    |

### Animation

**New Row Insertion**
- When a new blocked request arrives, it slides in from the top.
- 2 frames at 80ms: row appears with `OakMid` background highlight, then settles to normal.
- If user is not scrolled to top, a "New items ▲" indicator appears at top in `Flame`.

---

## Screen 6: Proxy Allowed Tab

### Purpose
History of allowed network requests (both whitelisted and manually approved). Includes response
data (status code, response headers) captured after the request completed.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed (124)  Bridge Logs  Routes  Configure  About
 ──────────  ───────  ───────  ━━━━━━━━━━━━━  ───────────  ──────  ─────────  ─────

 TIME      DOMAIN                      SOURCE             METHOD  STATUS  TYPE
 ──────────────────────────────────────────────────────────────────────────────
 ▶ 14:35:08  api.anthropic.com         barrel-myproject   POST    200     whitelist
   14:35:01  api.anthropic.com         barrel-myproject   POST    200     whitelist
   14:34:45  api.openai.com            barrel-backend     POST    200     whitelist
   14:34:30  docs.python.org           barrel-backend     GET     200     approved

 ┌─ Allowed Detail ──────────────────────────────────────────────────────────┐
 │                                                                          │
 │  URL       https://api.anthropic.com/v1/messages                         │
 │  Method    POST                                                          │
 │  Source    barrel-myproject                                               │
 │  Time      14:35:08                                                      │
 │  Type      whitelist                                                     │
 │                                                                          │
 │  ── Request Headers ─────────────────────────────────────────────────── │
 │  Content-Type     application/json                                       │
 │  X-Api-Key        sk-ant-...redacted                                     │
 │                                                                          │
 │  ── Response ────────────────────────────────────────────────────────── │
 │  Status    200 OK                                                        │
 │  ── Response Headers ────────────────────────────────────────────────── │
 │  Content-Type     application/json                                       │
 │  X-Request-Id     req_01abc...                                           │
 │                                                                          │
 └──────────────────────────────────────────────────────────────────────────┘

 [↑↓ Nav]  [Enter Detail]  [Esc Close]  [q Quit]                          🥃
```

### Color Specification

| Element                | Color            | Hex       |
|------------------------|------------------|-----------|
| Status `200`           | `Proof`          | #58c878   |
| Status `3xx`           | `SlateBlue`      | #6888b8   |
| Status `4xx`           | `Copper`         | #d4783c   |
| Status `5xx`           | `Flame`          | #e84820   |
| Type `whitelist`       | `Proof`          | #58c878   |
| Type `approved`        | `Amber`          | #f0a830   |
| Detail pane title      | `Proof` bold     | #58c878   |
| Response section       | `Verdigris`      | #48a89c   |
| Tab badge count        | `Proof`          | #58c878   |

(All other elements follow the same conventions as Blocked tab.)

### Key Bindings

Same as Blocked tab (Section 5).

### Empty State

```
                     🥃

          No requests recorded yet.

    Requests will appear here once AI tools
    start making network calls.
```

### Response Data in Detail

The detail pane includes a `── Response ──` section below request headers, showing:
- Status code with colored badge (200 green, 4xx amber, 5xx red).
- Response headers.
- This data is only available after the request has completed through the proxy.

---

## Screen 7: Execution Bridge Logs Tab

### Purpose
Live log viewer for execution bridge API calls. Shows when AI tools invoke bridge scripts,
with expandable detail showing request body, stdout, stderr, and response.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs (12)  Routes  Configure  About
 ──────────  ───────  ───────  ───────  ━━━━━━━━━━━━━━━━  ──────  ─────────  ─────

 Bridge API: http://localhost:4343

 TIME      ROUTE              SOURCE             STATUS   DURATION
 ──────────────────────────────────────────────────────────────────────────────
 ▶ 14:36:02  /deploy-staging   barrel-myproject   ✓ 0      2.4s
   14:35:44  /go-mod-tidy      barrel-myproject   ✓ 0      0.8s
   14:35:12  /restart-dev      barrel-backend     ✗ 1      0.3s
   14:34:50  /deploy-staging   barrel-myproject   ✓ 0      3.1s

 ┌─ Execution Detail ────────────────────────────────────────────────────────┐
 │                                                                          │
 │  Route     /deploy-staging                                               │
 │  Script    ~/scripts/deploy-staging.sh                                   │
 │  Source    barrel-myproject                                               │
 │  Time      14:36:02                                                      │
 │  Duration  2.4s                                                          │
 │  Exit      0                                                             │
 │                                                                          │
 │  ── stdout ──────────────────────────────────────────────────────────── │
 │  Deploying to staging...                                                 │
 │  Building container image...                                             │
 │  Pushing to registry...                                                  │
 │  Deploy complete: https://staging.example.com                            │
 │                                                                          │
 │  ── stderr ──────────────────────────────────────────────────────────── │
 │  (empty)                                                                 │
 │                                                                          │
 └──────────────────────────────────────────────────────────────────────────┘

 [↑↓ Nav]  [Enter Detail]  [Esc Close]  [q Quit]                          🥃
```

### Color Specification

| Element               | Color            | Hex       |
|-----------------------|------------------|-----------|
| Bridge API URL        | `Verdigris`      | #48a89c   |
| Route path            | `Parchment`      | #e8e0d4   |
| Exit `✓ 0`            | `Proof`          | #58c878   |
| Exit `✗ N` (nonzero)  | `Flame`          | #e84820   |
| Duration              | `Dusty`          | #8a7e70   |
| stdout section header | `Verdigris`      | #48a89c   |
| stdout text           | `Linen`          | #c4b8a8   |
| stderr section header | `Copper`         | #d4783c   |
| stderr text           | `Copper`         | #d4783c   |
| `(empty)` placeholder | `Faded` italic   | #5c5248   |
| Tab badge count       | `Verdigris`      | #48a89c   |

### Key Bindings

| Key        | Action                       |
|------------|------------------------------|
| `↑` / `k`  | Move selection up           |
| `↓` / `j`  | Move selection down         |
| `Enter`    | Toggle detail pane          |
| `Esc`      | Close detail pane           |
| `G`        | Jump to bottom              |
| `g`        | Jump to top                 |
| `1`-`8`    | Switch tab                  |
| `q`        | Exit confirmation           |

### Empty State

```
                     🔌

         No bridge executions yet.

    Configure routes in the Routes tab,
    then AI tools can call them via
    http://localhost:4343/{route}
```

### New Entry Animation

Same as Blocked tab: new entries slide in from top with 2-frame highlight.

---

## Screen 8: Execution Bridge Routes Tab

### Purpose
Manage execution bridge route mappings. Each route maps an API path to a script on the host.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ──────────  ───────  ───────  ───────  ───────────  ━━━━━━  ─────────  ─────

 Bridge Routes                                    Bridge API: localhost:4343

 API PATH              SCRIPT                           STATUS
 ──────────────────────────────────────────────────────────────────────────────
 ▶ /deploy-staging     ~/scripts/deploy-staging.sh      ✓ exists
   /go-mod-tidy        ~/scripts/go-mod-tidy.sh         ✓ exists
   /restart-dev        ~/scripts/restart-dev.sh         ✗ not found
   /run-tests          ~/scripts/run-tests.sh           ✓ exists




 ┌──────────────────────────────────────────────────────────────────────────┐
 │  Best practice: Bridge scripts should take NO input.                    │
 │  If scripts take input, they must validate religiously.                 │
 │  Scripts run on the HOST machine with your user's permissions.          │
 └──────────────────────────────────────────────────────────────────────────┘

 [n New]  [e Edit]  [x Delete]  [↑↓ Nav]  [q Quit]                       🥃
```

### Color Specification

| Element                  | Color            | Hex       |
|--------------------------|------------------|-----------|
| Route path               | `Parchment`      | #e8e0d4   |
| Script path              | `Verdigris`      | #48a89c   |
| `✓ exists`               | `Proof`          | #58c878   |
| `✗ not found`            | `Flame`          | #e84820   |
| Info box border          | `OakLight`       | #283040   |
| Info box text            | `Dusty`          | #8a7e70   |
| Info box emphasis        | `Copper` bold    | #d4783c   |

### Key Bindings

| Key       | Action                              |
|-----------|-------------------------------------|
| `↑` / `k` | Move selection up                  |
| `↓` / `j` | Move selection down                |
| `n`       | Add new route (opens edit modal)    |
| `e`       | Edit selected route                 |
| `Enter`   | Same as `e`                         |
| `x`       | Delete selected route (confirm)     |
| `1`-`8`   | Switch tab                          |
| `q`       | Exit confirmation                   |

### Add/Edit Route Modal

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🔌 Add Bridge Route                ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   API Path:                          ║
              ║   [/deploy-staging              ]    ║
              ║                                      ║
              ║   Script Path:                       ║
              ║   [~/scripts/deploy-staging.sh  ]    ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Save]    [Esc Cancel]     ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

- Active field has `Amber` brackets, inactive has `OakLight`.
- `Tab` moves between fields.
- Path validation: API path must start with `/`, script path must be non-empty.

### Delete Confirmation Modal

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🔌 Delete Route?                   ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   Route: /deploy-staging              ║
              ║   Script: ~/scripts/deploy.sh         ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Delete]   [Esc Cancel]    ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

### Empty State

```
                     🔌

          No bridge routes configured.

    Press  n  to add your first route.
    Routes let AI tools execute host scripts
    via http://localhost:4343/{path}
```

---

## Screen 9: Configure Tab

### Purpose
Runtime configuration for the TUI. Changes take effect immediately. Editable settings: monitor
timeout, log history limits.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ──────────  ───────  ───────  ───────  ───────────  ──────  ━━━━━━━━━  ─────

 Runtime Settings                                     Changes take effect immediately

 SETTING                              VALUE
 ──────────────────────────────────────────────────────────────────────────────

 ▶ Monitor approval timeout           [  5 ] seconds
   Blocked history limit              [500 ] entries
   Allowed history limit              [500 ] entries
   Bridge log limit                   [500 ] entries


 ┌──────────────────────────────────────────────────────────────────────────┐
 │  Monitor approval timeout: How long to wait for approval before         │
 │  automatically denying a request. Lower values are more secure but      │
 │  require faster reactions.                                              │
 │                                                                         │
 │  Full logs are always written to ~/.cooper/logs/ regardless of these    │
 │  display limits.                                                        │
 └──────────────────────────────────────────────────────────────────────────┘

 [↑↓ Nav]  [Enter Edit]  [←→ Adjust]  [q Quit]                           🥃
```

### Color Specification

| Element                  | Color            | Hex       |
|--------------------------|------------------|-----------|
| Section title            | `Linen` bold     | #c4b8a8   |
| Subtitle                 | `Dusty` italic   | #8a7e70   |
| Setting label            | `Parchment`      | #e8e0d4   |
| Value in brackets        | `Amber`          | #f0a830   |
| Brackets (active)        | `Amber`          | #f0a830   |
| Brackets (inactive)      | `OakLight`       | #283040   |
| Unit suffix              | `Dusty`          | #8a7e70   |
| Info box border          | `OakLight`       | #283040   |
| Info box text            | `Dusty`          | #8a7e70   |

### Key Bindings

| Key           | Action                                  |
|---------------|-----------------------------------------|
| `↑` / `k`     | Move selection up                      |
| `↓` / `j`     | Move selection down                    |
| `Enter`       | Enter edit mode for selected setting   |
| `←` / `h`     | Decrease value (in edit mode)          |
| `→` / `l`     | Increase value (in edit mode)          |
| `Esc`         | Exit edit mode (value saved)           |
| `1`-`8`       | Switch tab                             |
| `q`           | Exit confirmation                      |

### Edit Mode

When Enter is pressed on a setting, the brackets become `Amber` and the value becomes editable:
- Number input: type digits directly, Backspace to delete.
- Left/Right arrows: increment/decrement by 1.
- Shift+Left/Right: increment/decrement by 10.
- Enter or Esc: confirm and exit edit mode.

Value changes are applied immediately. The info box updates to show the description of the
currently selected setting.

### Validation

| Setting           | Min  | Max    | Default |
|-------------------|------|--------|---------|
| Approval timeout  | 1    | 60     | 5       |
| Blocked limit     | 50   | 10000  | 500     |
| Allowed limit     | 50   | 10000  | 500     |
| Bridge log limit  | 50   | 10000  | 500     |

Out-of-range values are clamped and the brackets flash `Flame` briefly (one frame, 200ms).

---

## Screen 10: About Tab

### Purpose
Shows tool versions, container configuration, and comparison between installed and host versions.

### ASCII Mockup

```
🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 2 containers
──────────────────────────────────────────────────────────────────────────────────
 Containers  Monitor  Blocked  Allowed  Bridge Logs  Routes  Configure  About
 ──────────  ───────  ───────  ───────  ───────────  ──────  ─────────  ━━━━━

 Cooper v0.1.0                                           github.com/user/cooper

 ── Programming Tools ────────────────────────────────────────────────────────

 TOOL          CONTAINER VERSION    HOST VERSION     MODE       STATUS
 ──────────────────────────────────────────────────────────────────────────────
 Go            1.22.4            1.22.4           mirror     ✓ match
 Node.js       20.11.1           20.12.0          mirror     ⚠ mismatch
 Python        3.12.2            3.12.2           pin        ✓ pinned
 Rust          (not installed)   1.77.0           off        ─

 ── AI CLI Tools ─────────────────────────────────────────────────────────────

 TOOL          CONTAINER VERSION    HOST VERSION     MODE       STATUS
 ──────────────────────────────────────────────────────────────────────────────
 Claude Code   1.0.5             1.0.5            latest     ✓ latest
 Copilot CLI   (not installed)   0.4.2            off        ─
 Codex CLI     0.1.2             0.1.2            mirror     ✓ match
 OpenCode      (not installed)   (not installed)  off        ─

 ── Infrastructure ───────────────────────────────────────────────────────────

 Proxy Port       3128                Docker Engine    24.0.7
 Bridge Port      4343                Squid Version    6.6
 Cooper CA        ✓ valid (exp 2027)  Image Size       1.2 GB


 [u Update]  [q Quit]                                                     🥃
```

### Color Specification

| Element                    | Color            | Hex       |
|----------------------------|------------------|-----------|
| Cooper version             | `Amber` bold     | #f0a830   |
| GitHub URL                 | `Dusty`          | #8a7e70   |
| Section header `──`        | `Dusty`          | #8a7e70   |
| Column headers             | `Dusty` bold     | #8a7e70   |
| Tool name                  | `Parchment`      | #e8e0d4   |
| Container version          | `Linen`          | #c4b8a8   |
| Host version               | `Linen`          | #c4b8a8   |
| Mode `mirror`              | `SlateBlue`      | #6888b8   |
| Mode `latest`              | `Verdigris`      | #48a89c   |
| Mode `pin`                 | `Amber`          | #f0a830   |
| Mode `off`                 | `Faded`          | #5c5248   |
| `✓ match` / `✓ latest`     | `Proof`          | #58c878   |
| `⚠ mismatch`              | `Copper` bold    | #d4783c   |
| `✓ pinned`                 | `Proof`          | #58c878   |
| `─` (off)                  | `Faded`          | #5c5248   |
| `(not installed)`          | `Faded` italic   | #5c5248   |
| Infrastructure labels      | `Dusty`          | #8a7e70   |
| Infrastructure values      | `Linen`          | #c4b8a8   |
| CA `✓ valid`               | `Proof`          | #58c878   |
| CA `✗ expired`             | `Flame`          | #e84820   |

### Key Bindings

| Key   | Action                                            |
|-------|---------------------------------------------------|
| `u`   | Prompt to run `cooper update` (if mismatches)     |
| `1`-`8` | Switch tab                                     |
| `q`   | Exit confirmation                                 |

### Mismatch Warning

When version mismatches are detected, a warning banner appears:

```
 ⚠ Version mismatches detected. Run  cooper update  to rebuild the container image.
```

- Banner in `Copper` background, `Void` text.
- Appears between header and content, full width.

---

## Screen 11: Exit Confirmation Modal

### Purpose
Confirms that the user wants to quit Cooper, stopping proxy and all containers.

### ASCII Mockup

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🥃 Seal the barrel?                ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   This will stop the proxy and all   ║
              ║   containers.                        ║
              ║                                      ║
              ║   2 active containers will lose       ║
              ║   network access immediately.        ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Confirm]    [Esc Cancel]  ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

### Color Specification

| Element              | Color            | Hex       |
|----------------------|------------------|-----------|
| Border               | `Amber`          | #f0a830   |
| Title                | `Parchment` bold | #e8e0d4   |
| Dividers             | `OakLight`       | #283040   |
| Body text            | `Linen`          | #c4b8a8   |
| Active container count | `Amber` bold    | #f0a830   |
| Confirm button       | `Proof` bold     | #58c878   |
| Cancel button        | `Dusty`          | #8a7e70   |
| Dimmed background    | `Faded`          | #5c5248   |

### Key Bindings

| Key       | Action                     |
|-----------|----------------------------|
| `Enter`   | Confirm exit (shutdown)    |
| `y`       | Same as Enter              |
| `Esc`     | Cancel, return to TUI      |
| `n`       | Same as Esc                |

### State Transitions

| From        | Trigger   | To              |
|-------------|-----------|-----------------|
| Any screen  | `q`       | Exit modal      |
| Exit modal  | `Enter`   | Shutdown screen |
| Exit modal  | `Esc`     | Previous screen |

### Dynamic Content

The body text adapts:
- If containers are running: shows count of active containers.
- If no containers: just "This will stop the proxy."
- If pending requests: "N pending requests will be denied."

---

## Screen 12: Shutdown Screen

### Purpose
Graceful shutdown progress. Reuses the loading screen layout with shutdown-specific messaging.

### ASCII Mockup

```
                          (vertical centering padding)


                       · 🥃 ·


                    c o o p e r

                   sealing the barrel...


               ━━━━━━━━━━━━━━━━━━━━━━╸───── 78%


               ✓ Pending requests denied
               ✓ CLI containers stopped
               · Proxy container stopping...
               · Networks removing...


                                               🥃


                          (vertical centering padding)
```

### Color Specification

Same base colors as loading screen (Screen 1), with these differences:

| Element                     | Color           | Hex       |
|-----------------------------|-----------------|-----------|
| Subtitle `sealing the...`   | `Dusty`         | #8a7e70   |
| Completed steps             | `Proof`         | #58c878   |
| In-progress steps           | `Amber`         | #f0a830   |

### Shutdown Steps

| Step                           | Progress | Message                          |
|--------------------------------|----------|----------------------------------|
| Deny pending requests          | 10%      | `Denying pending requests...`    |
| Stop CLI containers            | 40%      | `Stopping containers...`         |
| Stop proxy container           | 70%      | `Stopping proxy container...`    |
| Remove Docker networks         | 90%      | `Removing cooper networks...`    |
| Complete                       | 100%     | `barrel sealed`                  |

### Animation

Same barrel roll animation as loading screen. Subtitle changes:

| State     | Subtitle                       |
|-----------|--------------------------------|
| Active    | `sealing the barrel...`        |
| Error     | `the barrel won't close`       |
| Complete  | `barrel sealed`                |

On completion at 100%:
- Hold for 800ms, then quit the application.
- The `🥃` flanking animation stops (static `· 🥃 ·`).

### Key Bindings

No keys are active during shutdown. The user cannot cancel a shutdown in progress.

### Error State

If shutdown fails (e.g., Docker daemon unresponsive):
- Progress bar turns `Flame`.
- Failed step shows `✗` and error message.
- Help bar shows `[q Force Quit]` -- which exits without waiting for containers.

---

## Screen 13: Configure Wizard -- Welcome

### Purpose
Entry screen for `cooper configure`. Shows main menu with setup categories. This is
a separate TUI program from `cooper up`.

### ASCII Mockup

```
                          (vertical centering padding)

                            🥃

                    c o o p e r   c o n f i g u r e

                    Barrel-proof containers for undiluted AI.


    ┌──────────────────────────────────────────────────────────────────────┐
    │                                                                      │
    │  ▶ 1. Programming Tools     Go, Node.js, Python, Rust               │
    │    2. AI CLI Tools          Claude Code, Copilot, Codex, OpenCode   │
    │    3. Proxy Whitelist       Domain whitelist, port forwarding        │
    │    4. Proxy Settings        Proxy port, bridge port                  │
    │    5. Save & Build          Write config, build images               │
    │                                                                      │
    └──────────────────────────────────────────────────────────────────────┘


    Status: No existing configuration found. Starting fresh.

    [↑↓ Nav]  [Enter Select]  [q Quit]                                    🥃
```

If existing config is found:

```
    Status: Existing configuration loaded from ~/.cooper/config.json
```

### Color Specification

| Element                   | Color            | Hex       |
|---------------------------|------------------|-----------|
| `🥃` emoji                | (native)         | --        |
| Title `c o o p e r`       | `Amber` bold     | #f0a830   |
| `c o n f i g u r e`       | `Linen`          | #c4b8a8   |
| Tagline                   | `Dusty` italic   | #8a7e70   |
| Menu box border           | `OakLight`       | #283040   |
| Menu number               | `Amber`          | #f0a830   |
| Menu label                | `Parchment`      | #e8e0d4   |
| Menu description          | `Dusty`          | #8a7e70   |
| Selected row bg           | `OakMid`         | #1e2530   |
| Selection arrow           | `Amber`          | #f0a830   |
| Status (no config)        | `Copper`         | #d4783c   |
| Status (config found)     | `Proof`          | #58c878   |

### Key Bindings

| Key       | Action                      |
|-----------|-----------------------------|
| `↑` / `k` | Move selection up          |
| `↓` / `j` | Move selection down        |
| `Enter`   | Enter selected section      |
| `1`-`5`   | Jump to section by number   |
| `q`       | Quit configure              |

### State Transitions

| From      | Trigger    | To                          |
|-----------|------------|-----------------------------|
| (entry)   | Start      | Welcome screen              |
| Welcome   | `Enter`    | Selected setup screen       |
| Welcome   | `q`        | Quit (confirm if unsaved)   |

---

## Screen 14: Configure Wizard -- Programming Tool Setup

### Purpose
Select and configure programming languages/tools to install in the container image.

### ASCII Mockup (List View)

```
🥃 Configure > Programming Tools

 Detected host tools are shown. Toggle tools on/off, select to configure version.

 TOOL          STATUS    CONTAINER VERSION    HOST VERSION     MODE
 ──────────────────────────────────────────────────────────────────────────────
 ▶ [●] Go      on       1.22.4            1.22.4           mirror
   [●] Node.js on       20.11.1           20.12.0          latest
   [●] Python  on       3.12.2            3.12.2           pin
   [○] Rust    off      ─                 1.77.0           ─

 ┌──────────────────────────────────────────────────────────────────────────┐
 │  Tools not in this list can be added manually in                        │
 │  ~/.cooper/cli/Dockerfile.user  which layers on top of the generated    │
 │  Dockerfile. Cooper never modifies Dockerfile.user.                     │
 └──────────────────────────────────────────────────────────────────────────┘

 [Space Toggle]  [Enter Configure]  [↑↓ Nav]  [Esc Back]                  🥃
```

### ASCII Mockup (Tool Detail View)

When Enter is pressed on a tool, the detail configuration opens:

```
🥃 Configure > Programming Tools > Go

 ┌─ Go Configuration ──────────────────────────────────────────────────────┐
 │                                                                         │
 │  Status:  [●] Enabled                                                   │
 │                                                                         │
 │  Version Mode:                                                          │
 │                                                                         │
 │    ▶ ● Mirror    Install same version as host: 1.22.4                   │
 │      ○ Latest    Install latest available: 1.22.5                       │
 │      ○ Pin       Specify exact version: [             ]                 │
 │                                                                         │
 │  ── Version Info ─────────────────────────────────────────────────────  │
 │                                                                         │
 │  Host version:     1.22.4                                               │
 │  Latest version:   1.22.5   (resolved from go.dev)                      │
 │  Container version: 1.22.4  (current image)                             │
 │                                                                         │
 │  Mirror and Latest modes will update when you run  cooper update .      │
 │                                                                         │
 └─────────────────────────────────────────────────────────────────────────┘

 [↑↓ Nav]  [Space Select]  [Esc Back]                                     🥃
```

### Color Specification

| Element                    | Color            | Hex       |
|----------------------------|------------------|-----------|
| Breadcrumb `>`             | `Dusty`          | #8a7e70   |
| Breadcrumb active          | `Amber`          | #f0a830   |
| Toggle `[●]` on            | `Amber`          | #f0a830   |
| Toggle `[○]` off           | `Dusty`          | #8a7e70   |
| `on` text                  | `Proof`          | #58c878   |
| `off` text                 | `Faded`          | #5c5248   |
| Mode `mirror`              | `SlateBlue`      | #6888b8   |
| Mode `latest`              | `Verdigris`      | #48a89c   |
| Mode `pin`                 | `Amber`          | #f0a830   |
| Radio selected `●`         | `Amber`          | #f0a830   |
| Radio unselected `○`       | `Dusty`          | #8a7e70   |
| Version number             | `Linen`          | #c4b8a8   |
| Version source hint        | `Dusty` italic   | #8a7e70   |
| Config box border          | `OakLight`       | #283040   |
| Inline code                | `Amber`          | #f0a830   |

### Key Bindings (List View)

| Key        | Action                            |
|------------|-----------------------------------|
| `↑` / `k`  | Move selection up                |
| `↓` / `j`  | Move selection down              |
| `Space`    | Toggle selected tool on/off       |
| `Enter`    | Open version configuration        |
| `Esc`      | Back to welcome menu              |
| `q`        | Quit (confirm if unsaved)         |

### Key Bindings (Detail View)

| Key        | Action                            |
|------------|-----------------------------------|
| `↑` / `k`  | Move between version modes       |
| `↓` / `j`  | Move between version modes       |
| `Space`    | Select version mode               |
| `Enter`    | Select version mode               |
| `Esc`      | Back to tool list                 |

### Pin Version Input

When "Pin" mode is selected, the input field becomes active:
- Cursor blinks in the field.
- Type version string (e.g., `1.22.4`).
- On Enter, version is validated against the tool's version API.
- Valid: `✓` in `Proof` appears next to field.
- Invalid: field brackets turn `Flame`, error below: `Invalid version: 1.22.99 not found`.
- Validation is async; a spinner `·` appears during lookup.

### Version Resolution Sources (shown in status)

| Tool      | API Source                                          |
|-----------|-----------------------------------------------------|
| Go        | `go.dev/dl/?mode=json`                              |
| Node.js   | `nodejs.org/dist/index.json`                        |
| Python    | `endoflife.date/api/python.json`                    |
| Rust      | `static.rust-lang.org/dist/channel-rust-stable.toml`|

### Empty State / No Host Detection

If a tool is not detected on the host:

```
   [○] Rust    off      ─                 (not detected)   ─
```

- `(not detected)` in `Faded` italic.
- Tool starts as `off` by default.

---

## Screen 15: Configure Wizard -- AI CLI Tool Setup

### Purpose
Select and configure AI CLI tools to install in the container image. Same layout pattern as
Programming Tools (Screen 14).

### ASCII Mockup (List View)

```
🥃 Configure > AI CLI Tools

 Select AI CLI tools to install in containers. Toggle on/off, select to configure.

 TOOL             STATUS    CONTAINER VERSION    HOST VERSION     MODE
 ──────────────────────────────────────────────────────────────────────────────
 ▶ [●] Claude Code  on     1.0.5             1.0.5            latest
   [○] Copilot CLI  off    ─                 0.4.2            ─
   [●] Codex CLI    on     0.1.2             0.1.2            mirror
   [○] OpenCode     off    ─                 (not detected)   ─

 ┌──────────────────────────────────────────────────────────────────────────┐
 │  Enabled AI tools will have their API provider domains automatically    │
 │  added to the proxy whitelist (e.g., api.anthropic.com for Claude).     │
 │                                                                         │
 │  Additional tools can be added in ~/.cooper/cli/Dockerfile.user.        │
 │  Request new tools at github.com/user/cooper/issues.                    │
 └──────────────────────────────────────────────────────────────────────────┘

 [Space Toggle]  [Enter Configure]  [↑↓ Nav]  [Esc Back]                  🥃
```

### AI Tool Detail View

Same layout as Programming Tool detail (Screen 14) with tool-specific info:

```
🥃 Configure > AI CLI Tools > Claude Code

 ┌─ Claude Code Configuration ─────────────────────────────────────────────┐
 │                                                                         │
 │  Status:  [●] Enabled                                                   │
 │                                                                         │
 │  Version Mode:                                                          │
 │                                                                         │
 │    ▶ ● Latest    Install latest from npm: 1.0.5                         │
 │      ○ Mirror    Install same version as host: 1.0.5                    │
 │      ○ Pin       Specify exact version: [             ]                 │
 │                                                                         │
 │  ── Tool Info ────────────────────────────────────────────────────────  │
 │                                                                         │
 │  Package:      @anthropic-ai/claude-code (npm)                          │
 │  Auto-approve: claude --dangerously-skip-permissions                    │
 │  API domain:   api.anthropic.com (auto-whitelisted)                     │
 │  Auth:         ~/.claude + ~/.claude.json (mounted)                     │
 │                                                                         │
 └─────────────────────────────────────────────────────────────────────────┘
```

### Color Specification

Same as Programming Tools (Screen 14). Additional elements:

| Element                    | Color            | Hex       |
|----------------------------|------------------|-----------|
| Package name               | `Verdigris`      | #48a89c   |
| Auto-approve command       | `Copper`         | #d4783c   |
| API domain                 | `SlateBlue`      | #6888b8   |
| Auth info                  | `Dusty`          | #8a7e70   |

### Key Bindings

Same as Programming Tools (Screen 14).

### Version Resolution

All AI CLI tools use npm registry for version resolution:
- API: `https://registry.npmjs.org/<package>` queried via HTTP from Go.
- No npm required on host.

---

## Screen 16: Configure Wizard -- Proxy Whitelist Setup

### Purpose
Manage domain whitelist and port forwarding rules. Two internal sub-views: Domain Whitelist and
Port Forwarding, navigated with tabs within this screen.

### ASCII Mockup (Domain Whitelist Sub-view)

```
🥃 Configure > Proxy Whitelist

    Domains  Port Forwarding
    ━━━━━━━  ───────────────

 Default whitelisted domains (auto-configured for enabled AI tools):
 ── Default ──────────────────────────────────────────────────────────────
   api.anthropic.com              *.anthropic.com          Claude Code
   api.openai.com                 *.openai.com             Codex CLI
   raw.githubusercontent.com      exact                    Read-only

 Your whitelisted domains:
 ── Custom ───────────────────────────────────────────────────────────────
 ▶  sentry.mycompany.com          *.mycompany.com          with subdomains
    grafana.internal.io            exact                    exact only


 ┌──────────────────────────────────────────────────────────────────────────┐
 │  Be strict: only whitelist domains you trust completely.                │
 │  Package registries (npm, pypi, crates.io, gopkg) are blocked by       │
 │  default to prevent supply-chain attacks. AI tool dependencies are      │
 │  installed at  cooper build  time, not runtime.                         │
 │                                                                         │
 │  For ad-hoc access, use the Monitor tab in  cooper up  to approve      │
 │  individual requests in real-time.                                      │
 └──────────────────────────────────────────────────────────────────────────┘

 [n New]  [e Edit]  [x Delete]  [Tab Ports]  [↑↓ Nav]  [Esc Back]        🥃
```

### Color Specification

| Element                       | Color            | Hex       |
|-------------------------------|------------------|-----------|
| Sub-tab active                | `Verdigris` bold | #48a89c   |
| Sub-tab inactive              | `Dusty`          | #8a7e70   |
| Section `── Default ──`       | `Dusty`          | #8a7e70   |
| Section `── Custom ──`        | `Linen`          | #c4b8a8   |
| Default domain                | `Linen`          | #c4b8a8   |
| Default wildcard pattern      | `Mist`           | #889cb8   |
| Default tool source           | `Dusty` italic   | #8a7e70   |
| Custom domain                 | `Parchment`      | #e8e0d4   |
| `with subdomains`             | `Verdigris`      | #48a89c   |
| `exact only`                  | `SlateBlue`      | #6888b8   |
| Info box warning emphasis     | `Copper` bold    | #d4783c   |

### Add/Edit Domain Modal

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🛡️ Add Whitelisted Domain          ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   Domain:                            ║
              ║   [api.example.com              ]    ║
              ║                                      ║
              ║   [●] Include subdomains             ║
              ║       (matches *.example.com)        ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Save]    [Esc Cancel]     ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

- `Tab` cycles between domain field and subdomain toggle.
- `Space` toggles subdomain checkbox.

### Key Bindings (Domain Sub-view)

| Key          | Action                         |
|--------------|--------------------------------|
| `↑` / `k`    | Move selection up             |
| `↓` / `j`    | Move selection down           |
| `n`          | Add new domain                 |
| `e`          | Edit selected domain           |
| `Enter`      | Same as `e`                    |
| `x`          | Delete selected domain (confirm)|
| `Tab`        | Switch to Port Forwarding      |
| `Esc`        | Back to welcome menu           |

### Port Forwarding Sub-view

```
🥃 Configure > Proxy Whitelist

    Domains  Port Forwarding
    ───────  ━━━━━━━━━━━━━━━

 Port forwarding routes traffic from CLI container to host services.

 CLI PORT      HOST PORT    DESCRIPTION
 ──────────────────────────────────────────────────────────────────────────────
 ▶ 5432         5432        PostgreSQL
   6379         6379        Redis
   8000-8100    8000-8100   Dev server range


 ┌──────────────────────────────────────────────────────────────────────────┐
 │  ⚠ Host services must bind to 0.0.0.0 or the Docker gateway IP to     │
 │  be reachable from containers. Services bound to 127.0.0.1 only will   │
 │  NOT be accessible through port forwarding.                             │
 │                                                                         │
 │  Forwarding uses a two-hop relay:                                       │
 │  CLI container → cooper-proxy → host machine                            │
 └──────────────────────────────────────────────────────────────────────────┘

 [n New]  [e Edit]  [x Delete]  [Tab Domains]  [↑↓ Nav]  [Esc Back]      🥃
```

### Add/Edit Port Modal

```
              ╔══════════════════════════════════════╗
              ║                                      ║
              ║   🔌 Add Port Forward                ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   CLI Port:                          ║
              ║   [5432                         ]    ║
              ║                                      ║
              ║   Host Port:                         ║
              ║   [5432                         ]    ║
              ║                                      ║
              ║   Description (optional):            ║
              ║   [PostgreSQL                   ]    ║
              ║                                      ║
              ║   [●] Range mode (e.g. 8000-8100)   ║
              ║                                      ║
              ║   ──────────────────────────────────  ║
              ║                                      ║
              ║   [Enter ✓ Save]    [Esc Cancel]     ║
              ║                                      ║
              ╚══════════════════════════════════════╝
```

- Range mode: when toggled on, CLI Port and Host Port accept ranges like `8000-8100`.
- Validation: ports must be 1-65535, ranges must not overlap with proxy/bridge ports.

---

## Screen 17: Configure Wizard -- Port Forwarding Setup

This is accessed via Screen 16 (Proxy Whitelist Setup) through the Port Forwarding sub-tab.
See Screen 16 for the Port Forwarding sub-view specification. It is not a separate top-level
screen; it shares the Proxy Whitelist screen using internal tab navigation.

---

## Screen 18: Configure Wizard -- Proxy Setup

### Purpose
Configure proxy and bridge port numbers. Simple form.

### ASCII Mockup

```
🥃 Configure > Proxy Settings

 Configure the ports used by Cooper's proxy and execution bridge.

 ┌─ Port Configuration ────────────────────────────────────────────────────┐
 │                                                                         │
 │  Squid Proxy Port:                                                      │
 │  ▶ [3128                         ]                                      │
 │    Standard Squid port. Must not conflict with host services.           │
 │                                                                         │
 │  Execution Bridge Port:                                                 │
 │    [4343                         ]                                      │
 │    HTTP API for AI-to-host script execution.                            │
 │                                                                         │
 └─────────────────────────────────────────────────────────────────────────┘


 ┌──────────────────────────────────────────────────────────────────────────┐
 │  The execution bridge gives AI CLI tools a way to trigger host          │
 │  scripts without direct machine access. For example:                    │
 │    /deploy-staging  →  ~/scripts/deploy-staging.sh                      │
 │    /go-mod-tidy     →  ~/scripts/go-mod-tidy.sh                         │
 │                                                                         │
 │  Scripts should take NO input. The stdout/stderr is returned to the AI. │
 │  Configure routes in  cooper up  > Routes tab.                          │
 └──────────────────────────────────────────────────────────────────────────┘

 [Tab Next Field]  [Enter Save]  [Esc Back]                               🥃
```

### Color Specification

| Element                  | Color            | Hex       |
|--------------------------|------------------|-----------|
| Field label              | `Linen` bold     | #c4b8a8   |
| Field hint               | `Dusty`          | #8a7e70   |
| Active field brackets    | `Amber`          | #f0a830   |
| Inactive field brackets  | `OakLight`       | #283040   |
| Port number              | `Parchment`      | #e8e0d4   |
| Example routes           | `Verdigris`      | #48a89c   |
| Example scripts          | `Amber`          | #f0a830   |

### Key Bindings

| Key        | Action                        |
|------------|-------------------------------|
| `Tab`      | Move to next field            |
| `Shift+Tab`| Move to previous field        |
| `Enter`    | Save and return to menu       |
| `Esc`      | Back to welcome (discard)     |

### Validation

- Both ports must be valid (1-65535).
- Ports must not be equal.
- Ports must not conflict with forwarded ports.
- Conflict detected: field brackets turn `Flame`, error message appears:
  `Port 3128 conflicts with Squid proxy port`.

---

## Screen 19: Configure Wizard -- Save and Build

### Purpose
Final step of configuration. Shows summary and offers to build immediately.

### ASCII Mockup

```
🥃 Configure > Save & Build

 ── Configuration Summary ────────────────────────────────────────────────

 Programming Tools:  Go 1.22.4 (mirror), Node.js 20.11.1 (latest), Python 3.12.2 (pin)
 AI CLI Tools:       Claude Code 1.0.5 (latest), Codex CLI 0.1.2 (mirror)
 Whitelisted:        5 domains (3 default + 2 custom)
 Port Forwarding:    3 rules
 Proxy Port:         3128
 Bridge Port:        4343

 ── Files to Write ───────────────────────────────────────────────────────

   ~/.cooper/config.json                         configuration
   ~/.cooper/proxy/Dockerfile                    proxy image
   ~/.cooper/proxy/squid.conf                    proxy config
   ~/.cooper/cli/Dockerfile                      CLI container base image
   ~/.cooper/cli/entrypoint.sh                   CLI container entrypoint

 ┌──────────────────────────────────────────────────────────────────────────┐
 │                                                                         │
 │              Save configuration and build images?                       │
 │                                                                         │
 │   [Enter ✓ Save & Build]   [s Save Only]   [Esc Cancel]                │
 │                                                                         │
 └──────────────────────────────────────────────────────────────────────────┘


 [Enter Build]  [s Save]  [Esc Cancel]                                    🥃
```

### Color Specification

| Element                     | Color            | Hex       |
|-----------------------------|------------------|-----------|
| Section headers             | `Dusty`          | #8a7e70   |
| Summary labels              | `Linen`          | #c4b8a8   |
| Summary values              | `Parchment`      | #e8e0d4   |
| Version numbers             | `Amber`          | #f0a830   |
| Mode tags `(mirror)`        | `SlateBlue`      | #6888b8   |
| Mode tags `(latest)`        | `Verdigris`      | #48a89c   |
| Mode tags `(pin)`           | `Amber`          | #f0a830   |
| File paths                  | `Verdigris`      | #48a89c   |
| File descriptions           | `Dusty`          | #8a7e70   |
| Save & Build button         | `Proof` bold     | #58c878   |
| Save Only button            | `Amber`          | #f0a830   |
| Cancel button               | `Dusty`          | #8a7e70   |

### Key Bindings

| Key       | Action                                  |
|-----------|-----------------------------------------|
| `Enter`   | Save config files and start build       |
| `s`       | Save config files only (no build)       |
| `Esc`     | Cancel, return to welcome               |

### State Transitions

| From          | Trigger    | To                              |
|---------------|------------|---------------------------------|
| Save & Build  | `Enter`    | Build progress (inline output)  |
| Save & Build  | `s`        | Save success message            |
| Save & Build  | `Esc`      | Welcome menu                    |

### Build Progress (After Save)

If user selects "Save & Build", the screen transitions to build output:

```
🥃 Configure > Building

 ── Building Cooper Images ───────────────────────────────────────────────

 ✓ Configuration saved to ~/.cooper/config.json

 Building proxy image (cooper-proxy)...
 ━━━━━━━━━━━━━━━━━━╸──────────────── 56%

   Step 3/8: Installing Squid with SSL support...

 Building CLI container base image (cooper-barrel-base)...
 ────────────────────────────────────── waiting


 [q Cancel]                                                               🥃
```

Progress uses the same progress bar component (Section 3.7). Two progress bars: one for proxy
image, one for CLI container base image. They build sequentially (proxy first, then CLI container base).

After completion:

```
 ✓ Configuration saved
 ✓ Proxy image built (cooper-proxy)          1m 24s
 ✓ CLI container base image built (cooper-barrel-base)  2m 08s

 Images are ready. Start Cooper with:  cooper up

 [Enter Done]                                                             🥃
```

---

## Animation Reference Appendix

### A.1 Barrel Roll (Loading/Shutdown)

Used on loading and shutdown screens flanking the `🥃` emoji.

| Frame | Display           | Timing  |
|-------|-------------------|---------|
| 0     | `· 🥃 ·`          | 150ms   |
| 1     | `· 🥃 · ·`        | 150ms   |
| 2     | `· 🥃 · · ·`      | 150ms   |
| 3     | `· 🥃 · ·`        | 150ms   |

Total cycle: 600ms. Dots in `Dusty` (#8a7e70).

### A.2 Status Pulse (Container Starting/Stopping)

Used for the `●` status dot when containers are transitioning.

| Frame | Color      | Hex       | Timing  |
|-------|------------|-----------|---------|
| 0     | `Amber`    | #f0a830   | 300ms   |
| 1     | `Copper`   | #d4783c   | 300ms   |
| 2     | `Amber`    | #f0a830   | 300ms   |

Total cycle: 900ms.

### A.3 Pending Request Badge Pulse

Used for the `⏱ N pending` counter in the header when requests are waiting.

| Frame | Color      | Hex       | Timing  |
|-------|------------|-----------|---------|
| 0     | `Copper`   | #d4783c   | 500ms   |
| 1     | `Amber`    | #f0a830   | 500ms   |

Total cycle: 1000ms. Only active when pending count > 0.

### A.4 Approval Flash (Monitor Tab)

On approve: selected row background flashes then slides out right.

| Frame | Background  | Offset | Timing  |
|-------|-------------|--------|---------|
| 0     | `Proof`     | 0      | 200ms   |
| 1     | `Proof`     | +4     | 100ms   |
| 2     | `Proof`     | +8     | 100ms   |
| 3     | (removed)   | --     | --      |

### A.5 Deny Flash (Monitor Tab)

On deny: selected row background flashes then slides out left.

| Frame | Background  | Offset | Timing  |
|-------|-------------|--------|---------|
| 0     | `Flame`     | 0      | 200ms   |
| 1     | `Flame`     | -4     | 100ms   |
| 2     | `Flame`     | -8     | 100ms   |
| 3     | (removed)   | --     | --      |

### A.6 Timeout Expiry Flash (Monitor Tab)

When countdown reaches 0, the row flashes before removal.

| Frame | Background  | Timing  |
|-------|-------------|---------|
| 0     | `Flame`     | 150ms   |
| 1     | `OakDark`   | 150ms   |
| 2     | (removed)   | --      |

### A.7 Progress Bar Stagger

Display progress chases target progress.

- Increment: 15% per tick.
- Tick interval: 80ms.
- Tip character `╸` in lighter shade appears at leading edge during movement.
- On reaching target: tip replaces with `━`, holds 200ms.
- On reaching 100%: holds 800ms, then triggers completion callback.

### A.8 New Row Insertion (History Tabs)

When new entry appears in Blocked/Allowed/Bridge Logs.

| Frame | Background   | Timing  |
|-------|-------------|---------|
| 0     | `OakMid`    | 80ms    |
| 1     | `OakDark`   | 80ms    |

If user is not scrolled to top, `▲ N new` indicator appears at top of list.

### A.9 Cursor Blink (Text Input)

Used in text input fields across configure screens.

| Frame | Cursor Color | Timing  |
|-------|-------------|---------|
| 0     | `Amber`     | 530ms   |
| 1     | `OakDark`   | 530ms   |

Total cycle: 1060ms.

### A.10 Value Change Flash (Configure Tab)

When a runtime setting value changes.

| Frame | Value Color  | Timing  |
|-------|-------------|---------|
| 0     | `Wheat`     | 150ms   |
| 1     | `Amber`     | (stays) |

For out-of-range clamp:

| Frame | Bracket Color | Timing  |
|-------|-------------|---------|
| 0     | `Flame`     | 200ms   |
| 1     | `Amber`     | (stays) |

---

## Go Implementation Notes

### Model Architecture

Following pgflock patterns: root model routes messages to sub-models, each tab is its own
BubbleTea model with Init/Update/View.

```go
// Root model in internal/tui/model.go
type Model struct {
    // Core state
    activeTab     TabID
    width         int
    height        int

    // Sub-models (one per tab)
    containers    containers.Model
    monitor       proxymon.Model
    blocked       history.Model    // Reused component, configured for blocked
    allowed       history.Model    // Reused component, configured for allowed
    bridgeLogs    bridgeui.Model
    bridgeRoutes  bridgeui.RoutesModel
    settings      settings.Model
    about         about.Model

    // Loading/shutdown screen
    loading       loading.Model
    showLoading   bool

    // Modal state
    modalActive   bool
    modalType     ModalType

    // Channels for external events
    aclEventChan     <-chan proxy.ACLEvent     // From ACL socket listener
    bridgeEventChan  <-chan bridge.ExecEvent   // From bridge HTTP server
    statsEventChan   <-chan docker.StatsEvent  // From docker stats poller

    // Callbacks
    onShutdown    func() <-chan loading.Progress
    onQuit        func()
}
```

### Tab Identifiers

```go
type TabID int

const (
    TabContainers TabID = iota
    TabMonitor
    TabBlocked
    TabAllowed
    TabBridgeLogs
    TabBridgeRoutes
    TabConfigure
    TabAbout
)
```

### Message Types

```go
// internal/tui/messages.go

// Tab navigation
type switchTabMsg struct{ tab TabID }

// Timer events
type tickMsg time.Time                    // 1s UI refresh
type animTickMsg time.Time                // 100ms animation tick
type countdownTickMsg time.Time           // 100ms countdown timer tick

// Loading events
type loadingTickMsg time.Time             // 150ms barrel roll animation
type loadingProgressTickMsg time.Time     // 80ms stagger tick
type loadingProgressMsg struct {
    progress loading.Progress
}

// Proxy events (from ACL socket)
type aclRequestMsg struct {               // New pending request
    request proxy.PendingRequest
}
type aclTimeoutMsg struct {               // Request countdown expired
    requestID string
}
type aclApprovedMsg struct {              // Request was approved
    requestID string
}
type aclDeniedMsg struct {                // Request was denied
    requestID string
    reason    string
}

// Bridge events
type bridgeExecMsg struct {               // Bridge script executed
    event bridge.ExecEvent
}

// Docker events
type statsUpdateMsg struct {              // Container stats update
    stats []docker.ContainerStats
}
type containerStateMsg struct {           // Container started/stopped
    name  string
    state string
}

// Modal events
type showModalMsg struct{ modalType ModalType }
type hideModalMsg struct{}

// Configure events (for runtime settings)
type settingChangedMsg struct {
    key   string
    value int
}
```

### Style Declarations

```go
// internal/tui/styles.go

var (
    // Base colors
    Charcoal  = lipgloss.Color("#0c0e12")
    OakDark   = lipgloss.Color("#151920")
    OakMid    = lipgloss.Color("#1e2530")
    OakLight  = lipgloss.Color("#283040")
    Stave     = lipgloss.Color("#3a4556")

    Parchment = lipgloss.Color("#e8e0d4")
    Linen     = lipgloss.Color("#c4b8a8")
    Dusty     = lipgloss.Color("#8a7e70")
    Faded     = lipgloss.Color("#5c5248")
    Void      = lipgloss.Color("#0c0e12")

    Amber     = lipgloss.Color("#f0a830")
    Copper    = lipgloss.Color("#d4783c")
    Barrel    = lipgloss.Color("#b85c28")
    Flame     = lipgloss.Color("#e84820")
    Wheat     = lipgloss.Color("#e8d8a0")

    Proof     = lipgloss.Color("#58c878")
    Verdigris = lipgloss.Color("#48a89c")
    SlateBlue = lipgloss.Color("#6888b8")
    Mist      = lipgloss.Color("#889cb8")

    // Pre-computed styles
    TitleStyle = lipgloss.NewStyle().
        Foreground(Amber).
        Bold(true)

    BrandStyle = lipgloss.NewStyle().
        Foreground(Amber).
        Bold(true)

    TaglineStyle = lipgloss.NewStyle().
        Foreground(Dusty).
        Italic(true)

    HeaderDividerStyle = lipgloss.NewStyle().
        Foreground(OakLight)

    TabActiveStyle = lipgloss.NewStyle().
        Foreground(Amber).
        Bold(true)

    TabInactiveStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    TabUnderlineActiveStyle = lipgloss.NewStyle().
        Foreground(Amber)

    TabUnderlineInactiveStyle = lipgloss.NewStyle().
        Foreground(OakLight)

    RowSelectedStyle = lipgloss.NewStyle().
        Background(OakMid).
        Foreground(Parchment).
        Bold(true)

    RowNormalStyle = lipgloss.NewStyle().
        Foreground(Linen)

    SelectionArrowStyle = lipgloss.NewStyle().
        Foreground(Amber).
        Bold(true)

    ColumnHeaderStyle = lipgloss.NewStyle().
        Foreground(Dusty).
        Bold(true)

    DividerStyle = lipgloss.NewStyle().
        Foreground(OakLight)

    StatusRunningStyle = lipgloss.NewStyle().
        Foreground(Proof)

    StatusStoppedStyle = lipgloss.NewStyle().
        Foreground(Flame)

    StatusTransitionStyle = lipgloss.NewStyle().
        Foreground(Amber)

    TimestampStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    ContainerNameStyle = lipgloss.NewStyle().
        Foreground(Parchment)

    SourceStyle = lipgloss.NewStyle().
        Foreground(Mist)

    DomainStyle = lipgloss.NewStyle().
        Foreground(Parchment)

    ProofStyle = lipgloss.NewStyle().
        Foreground(Proof)

    FlameStyle = lipgloss.NewStyle().
        Foreground(Flame)

    CopperStyle = lipgloss.NewStyle().
        Foreground(Copper)

    InfoBoxStyle = lipgloss.NewStyle().
        Border(lipgloss.NormalBorder()).
        BorderForeground(OakLight).
        Padding(0, 1)

    InfoTextStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    InfoEmphasisStyle = lipgloss.NewStyle().
        Foreground(Copper).
        Bold(true)

    DetailPaneStyle = lipgloss.NewStyle().
        Border(lipgloss.NormalBorder()).
        BorderForeground(OakLight).
        Padding(0, 1)

    DetailLabelStyle = lipgloss.NewStyle().
        Foreground(Dusty).
        Width(10).
        Align(lipgloss.Right)

    DetailValueStyle = lipgloss.NewStyle().
        Foreground(Parchment)

    DetailSectionStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    ModalBorderStyle = lipgloss.NewStyle().
        Border(lipgloss.DoubleBorder()).
        BorderForeground(Amber).
        Padding(1, 3).
        Width(50)

    ModalTitleStyle = lipgloss.NewStyle().
        Foreground(Parchment).
        Bold(true).
        Align(lipgloss.Center).
        Width(44)

    ModalDividerStyle = lipgloss.NewStyle().
        Foreground(OakLight).
        Align(lipgloss.Center).
        Width(44)

    ModalBodyStyle = lipgloss.NewStyle().
        Foreground(Linen).
        Align(lipgloss.Center).
        Width(44)

    ModalConfirmStyle = lipgloss.NewStyle().
        Foreground(Proof).
        Bold(true)

    ModalCancelStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    HelpKeyStyle = lipgloss.NewStyle().
        Foreground(Amber)

    HelpDescStyle = lipgloss.NewStyle().
        Foreground(Dusty)

    EmptyStateStyle = lipgloss.NewStyle().
        Foreground(Dusty).
        Italic(true).
        Align(lipgloss.Center)

    ErrorStyle = lipgloss.NewStyle().
        Foreground(Flame).
        Bold(true)

    InputActiveStyle = lipgloss.NewStyle().
        Foreground(Amber)

    InputInactiveStyle = lipgloss.NewStyle().
        Foreground(OakLight)

    InputTextStyle = lipgloss.NewStyle().
        Foreground(Parchment)

    InputPlaceholderStyle = lipgloss.NewStyle().
        Foreground(Faded).
        Italic(true)

    DimBackdropStyle = lipgloss.NewStyle().
        Foreground(Faded)

    // Method badge styles
    MethodGETStyle = lipgloss.NewStyle().
        Background(SlateBlue).
        Foreground(Void).
        Padding(0, 1)

    MethodPOSTStyle = lipgloss.NewStyle().
        Background(Amber).
        Foreground(Void).
        Padding(0, 1)

    MethodPUTStyle = lipgloss.NewStyle().
        Background(Copper).
        Foreground(Void).
        Padding(0, 1)

    MethodDELETEStyle = lipgloss.NewStyle().
        Background(Flame).
        Foreground(Parchment).
        Padding(0, 1)

    MethodPATCHStyle = lipgloss.NewStyle().
        Background(Verdigris).
        Foreground(Void).
        Padding(0, 1)

    // HTTP status code styles
    Status2xxStyle = lipgloss.NewStyle().Foreground(Proof)
    Status3xxStyle = lipgloss.NewStyle().Foreground(SlateBlue)
    Status4xxStyle = lipgloss.NewStyle().Foreground(Copper)
    Status5xxStyle = lipgloss.NewStyle().Foreground(Flame)
)
```

### Animator Types

```go
// internal/tui/components/animator.go

// StatusPulse animates the ● dot for transitioning containers.
type StatusPulse struct {
    frame  int
    colors []lipgloss.Color  // [Amber, Copper, Amber]
}

// PendingBadgePulse animates the ⏱ pending count in header.
type PendingBadgePulse struct {
    frame  int
    colors []lipgloss.Color  // [Copper, Amber]
}

// BarrelRoll animates the dot pattern around 🥃.
type BarrelRoll struct {
    frame  int
    frames []string  // ["· 🥃 ·", "· 🥃 · ·", "· 🥃 · · ·", "· 🥃 · ·"]
}

// ApprovalFlash handles the flash+slide for approve/deny.
type ApprovalFlash struct {
    active    bool
    frame     int
    approved  bool   // true=approve (green, slide right), false=deny (red, slide left)
    requestID string
}
```

### Countdown Timer

```go
// internal/tui/components/timer.go

type CountdownTimer struct {
    width    int                  // Bar character width (default 20)
    total    time.Duration        // Total countdown duration
    start    time.Time            // When countdown started
}

func (t *CountdownTimer) Remaining() time.Duration
func (t *CountdownTimer) Progress() float64    // 1.0=full, 0.0=expired
func (t *CountdownTimer) Color() lipgloss.Color // Based on timer gradient
func (t *CountdownTimer) Render() string       // Renders [━━━━━━───] 2.3s
func (t *CountdownTimer) IsExpired() bool
```

### Timer Color Selection

```go
func timerColor(progress float64) lipgloss.Color {
    switch {
    case progress > 0.80:
        return Proof     // #58c878
    case progress > 0.60:
        return Amber     // #f0a830
    case progress > 0.40:
        return Copper    // #d4783c
    case progress > 0.20:
        return Barrel    // #b85c28
    default:
        return Flame     // #e84820
    }
}
```

### Sub-Model Interface

Each tab sub-model implements this interface for consistent routing:

```go
// internal/tui/tabmodel.go

type TabModel interface {
    Init() tea.Cmd
    Update(msg tea.Msg) (TabModel, tea.Cmd)
    View() string
    SetSize(width, height int)
    ShortHelp() []HelpBinding  // For footer help bar
}

type HelpBinding struct {
    Key  string
    Desc string
}
```

### Root Update Routing

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        // Modal keys take priority
        if m.modalActive {
            return m.handleModalKey(msg)
        }
        // Tab switching (1-8, Tab, Shift-Tab)
        if tab, ok := m.handleTabKey(msg); ok {
            m.activeTab = tab
            return m, nil
        }
        // Quit
        if msg.String() == "q" || msg.String() == "ctrl+c" {
            m.modalActive = true
            m.modalType = ModalExit
            return m, nil
        }
        // Delegate to active tab
        return m.delegateToTab(msg)

    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
        contentHeight := msg.Height - 4 // header + divider + tab bar + footer
        m.delegateResize(msg.Width, contentHeight)
        return m, nil

    case aclRequestMsg:
        // Always route to monitor regardless of active tab
        m.monitor.Update(msg)
        return m, m.waitForACLEvent()

    case bridgeExecMsg:
        m.bridgeLogs.Update(msg)
        return m, m.waitForBridgeEvent()

    case statsUpdateMsg:
        m.containers.Update(msg)
        return m, m.waitForStatsEvent()

    // ... timer and animation ticks
    }
}
```

### Configure Wizard Model

The configure wizard is a separate TUI program with its own model:

```go
// internal/configure/model.go

type WizardModel struct {
    screen       WizardScreen
    width        int
    height       int

    // Sub-screens
    welcome      WelcomeModel
    progTools    ProgToolsModel
    aiTools      AIToolsModel
    whitelist    WhitelistModel
    proxySetup   ProxySetupModel
    saveAndBuild SaveBuildModel

    // Config state
    config       *config.Config
    dirty        bool            // Unsaved changes
}

type WizardScreen int

const (
    ScreenWelcome WizardScreen = iota
    ScreenProgTools
    ScreenProgToolDetail
    ScreenAITools
    ScreenAIToolDetail
    ScreenWhitelist
    ScreenWhitelistDomains
    ScreenWhitelistPorts
    ScreenProxySetup
    ScreenSaveBuild
    ScreenBuilding
)
```

### File Structure

```
internal/tui/
    model.go            // Root Model
    app.go              // Root Init/Update with message routing
    view.go             // Root View (header + tabs + active content + footer)
    styles.go           // All color/style constants
    constants.go        // Unicode chars, timing, icons
    messages.go         // All message types

    containers/
        model.go        // Container list model
        view.go         // Container list + detail rendering

    proxymon/
        model.go        // Pending queue, selected index, timers
        view.go         // Two-pane layout rendering

    history/
        model.go        // Shared scrollable history with detail toggle
        view.go         // List + detail pane rendering
                        // Reused for both Blocked and Allowed tabs

    bridgeui/
        model.go        // Bridge logs model
        routes.go       // Bridge routes management model
        view.go         // Logs view + routes view

    settings/
        model.go        // Runtime settings with edit mode
        view.go         // Settings form rendering

    about/
        model.go        // Version comparison model
        view.go         // Version table rendering

    loading/
        model.go        // Reusable loading screen (startup/shutdown/restart)
        view.go         // Centered progress rendering

    components/
        modal.go        // Modal dialog (exit, delete confirm, add/edit forms)
        tabs.go         // Tab bar rendering and navigation
        timer.go        // Countdown timer bar
        table.go        // Scrollable list with selection
        input.go        // Text input field with cursor
        toggle.go       // Checkbox/toggle component
        radio.go        // Radio button group
        progress.go     // Progress bar
        animator.go     // Animation state machines
```

### Channel Architecture

```go
// main.go or internal/app/run.go

func runUp(cfg *config.Config) error {
    // Create channels
    aclEvents := make(chan proxy.ACLEvent, 100)
    bridgeEvents := make(chan bridge.ExecEvent, 100)
    statsEvents := make(chan docker.StatsEvent, 10)
    loadingProgress := make(chan loading.Progress, 20)

    // Start background services
    aclListener := proxy.NewACLListener(cfg, aclEvents)
    bridgeServer := bridge.NewServer(cfg, bridgeEvents)
    statsPoller := docker.NewStatsPoller(cfg, statsEvents)

    // Create TUI model
    model := tui.NewModel(cfg, tui.ModelOptions{
        ACLEvents:     aclEvents,
        BridgeEvents:  bridgeEvents,
        StatsEvents:   statsEvents,
        LoadingChan:   loadingProgress,
    })

    // Set callbacks
    model.SetOnShutdown(func() <-chan loading.Progress {
        ch := make(chan loading.Progress, 20)
        go func() {
            shutdownAll(cfg, ch)
            close(ch)
        }()
        return ch
    })

    // Run TUI
    p := tea.NewProgram(model, tea.WithAltScreen())
    _, err := p.Run()
    return err
}
```

### Key Patterns from pgflock

- **Pre-computed styles**: all lipgloss styles are package-level vars, never allocated in render loops.
- **Staggered progress**: display progress trails actual progress for visual smoothness.
- **Modal overlay**: main view is rendered, then dimmed, then modal overlaid by replacing lines.
- **Loading reuse**: single loading screen model handles startup, shutdown, and restart modes.
- **Channel-based events**: background goroutines push events onto channels, TUI polls with `waitFor*` commands.
- **Callback architecture**: `onRestart`, `onShutdown`, `onQuit` set by main.go, invoked by TUI.
- **Width/height tracking**: all rendering uses stored terminal dimensions, defaults to 80x24.
- **Line truncation**: every output line is truncated to terminal width to prevent wrapping.
- **Scroll offset management**: adjusted in Update(), never in View(), keeping View() pure.
