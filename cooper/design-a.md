# Cooper TUI Design Document

Implementation-ready design specification for all Cooper TUI screens.
Framework: Go + BubbleTea (matching pgflock). Alt-screen mode.

---

## Table of Contents

1. [Color Palette](#1-color-palette)
2. [Typography & Spacing Conventions](#2-typography--spacing-conventions)
3. [Shared Components](#3-shared-components)
4. [cooper up -- Loading Screen](#4-cooper-up--loading-screen)
5. [cooper up -- Main Tab Bar](#5-cooper-up--main-tab-bar)
6. [cooper up -- Containers Tab](#6-cooper-up--containers-tab)
7. [cooper up -- Proxy Monitor Tab](#7-cooper-up--proxy-monitor-tab)
8. [cooper up -- Proxy Blocked Tab](#8-cooper-up--proxy-blocked-tab)
9. [cooper up -- Proxy Allowed Tab](#9-cooper-up--proxy-allowed-tab)
10. [cooper up -- Execution Bridge Logs Tab](#10-cooper-up--execution-bridge-logs-tab)
11. [cooper up -- Execution Bridge Routes Tab](#11-cooper-up--execution-bridge-routes-tab)
12. [cooper up -- Configure Tab](#12-cooper-up--configure-tab)
13. [cooper up -- About Tab](#13-cooper-up--about-tab)
14. [cooper up -- Exit Confirmation Modal](#14-cooper-up--exit-confirmation-modal)
15. [cooper up -- Shutdown Screen](#15-cooper-up--shutdown-screen)
16. [cooper configure -- Welcome / Main Menu](#16-cooper-configure--welcome--main-menu)
17. [cooper configure -- Programming Tool Setup](#17-cooper-configure--programming-tool-setup)
18. [cooper configure -- AI CLI Tool Setup](#18-cooper-configure--ai-cli-tool-setup)
19. [cooper configure -- Proxy Whitelist Setup](#19-cooper-configure--proxy-whitelist-setup)
20. [cooper configure -- Port Forwarding Setup](#20-cooper-configure--port-forwarding-setup)
21. [cooper configure -- Proxy Setup](#21-cooper-configure--proxy-setup)
22. [cooper configure -- Save & Build Prompt](#22-cooper-configure--save--build-prompt)

---

## 1. Color Palette

The "Cooper Distillery" palette. Built on pgflock's dark void/surface base, with pgflock's cyan
retained for headers and interactive elements. The pgflock lime green status color is replaced with
warm amber/copper tones that evoke oak barrels, whiskey aging, and the cooper trade.

### Base Colors (inherited from pgflock)

| Name           | Hex       | Usage                                      |
|----------------|-----------|---------------------------------------------|
| Void           | `#0a0e14` | Terminal background assumption, deepest bg  |
| Surface        | `#131920` | Card/panel backgrounds, elevated surfaces   |
| Selection      | `#1c2836` | Selected row background (subtle moonlit)    |
| Border         | `#2d3748` | Separators, dividers, inactive elements     |

### Text Colors

| Name           | Hex       | Usage                                      |
|----------------|-----------|---------------------------------------------|
| TextBright     | `#e2e8f0` | Primary text, headings                      |
| TextDim        | `#64748b` | Secondary text, labels, help descriptions   |
| TextFaint      | `#334155` | Dimmed backdrop behind modals               |
| TextMuted      | `#94a3b8` | Modal body text, explanatory paragraphs     |

### Cooper Theme Colors (warm accents -- the whiskey palette)

| Name           | Hex       | Usage                                      |
|----------------|-----------|---------------------------------------------|
| Amber          | `#fbbf24` | Primary accent. Countdown timers, selection arrows, port numbers, pending request highlights. The "lantern light" of the distillery. |
| Copper         | `#f59e0b` | Tab underline for active tab, warm glow on active status indicators. Slightly deeper than amber. |
| Oak            | `#92400e` | Subtle background tint for the tab bar separator line, barrel-themed decorative elements. |
| Bourbon        | `#d97706` | Progress bar fill color (replaces pgflock's lime for cooper-specific bars). Warm golden fill. |
| Charcoal       | `#78350f` | Deep warm brown for inactive/exhausted timer backgrounds. |

### Semantic Colors

| Name           | Hex       | Usage                                      |
|----------------|-----------|---------------------------------------------|
| Cyan           | `#22d3ee` | Headers, interactive keys in help bar, tab names, links. Shared with pgflock family. |
| Allowed        | `#4ade80` | Allowed/approved status, healthy containers, whitelist badges, checkmarks. (pgflock's lime) |
| Denied         | `#f87171` | Denied/blocked status, errors, unhealthy, countdown expired. (pgflock's coral) |
| Violet         | `#a78bfa` | Container names, markers, identifiers.      |

### Animation Colors

| Name           | Hex       | Usage                                      |
|----------------|-----------|---------------------------------------------|
| Rose           | `#fb7185` | Denial animation frame 1, 3                |
| Orange         | `#fb923c` | Denial animation frame 2 (peak)            |
| Emerald        | `#34d399` | Approval shimmer frame 0, 4                |
| Mint           | `#a7f3d0` | Approval shimmer frame 2 (peak)            |
| AmberBright    | `#fde68a` | Timer pulse peak brightness                |
| AmberDim       | `#92400e` | Timer pulse trough                         |

### Named lipgloss.Color Declarations (for constants.go)

```go
var (
    // Base
    ColorVoid      = lipgloss.Color("#0a0e14")
    ColorSurface   = lipgloss.Color("#131920")
    ColorSelection = lipgloss.Color("#1c2836")
    ColorBorder    = lipgloss.Color("#2d3748")

    // Text
    ColorTextBright = lipgloss.Color("#e2e8f0")
    ColorTextDim    = lipgloss.Color("#64748b")
    ColorTextFaint  = lipgloss.Color("#334155")
    ColorTextMuted  = lipgloss.Color("#94a3b8")

    // Cooper Theme (warm)
    ColorAmber    = lipgloss.Color("#fbbf24")
    ColorCopper   = lipgloss.Color("#f59e0b")
    ColorOak      = lipgloss.Color("#92400e")
    ColorBourbon  = lipgloss.Color("#d97706")
    ColorCharcoal = lipgloss.Color("#78350f")

    // Semantic
    ColorCyan    = lipgloss.Color("#22d3ee")
    ColorAllowed = lipgloss.Color("#4ade80")
    ColorDenied  = lipgloss.Color("#f87171")
    ColorViolet  = lipgloss.Color("#a78bfa")

    // Animation
    ColorRose        = lipgloss.Color("#fb7185")
    ColorOrange      = lipgloss.Color("#fb923c")
    ColorEmerald     = lipgloss.Color("#34d399")
    ColorMint        = lipgloss.Color("#a7f3d0")
    ColorAmberBright = lipgloss.Color("#fde68a")
    ColorAmberDim    = lipgloss.Color("#92400e")
)
```

---

## 2. Typography & Spacing Conventions

All conventions match pgflock for family consistency.

### Line Structure

- **Header**: 1 line. Brand + status indicators left, view toggle / tab info right.
- **Tab bar**: 1 line (cooper up only). Tabs separated by `  ` (2 spaces). Active tab is Bold+Cyan with Copper underline dot.
- **Separator**: 1 line of `---` characters in `ColorBorder`.
- **Content area**: Fills remaining height minus header, tab bar, 2 footer lines. Scrollable.
- **Footer separator**: 1 line of `---` characters in `ColorBorder`.
- **Help bar**: 1 line. Key bindings left, status indicators right.

### Key Binding Rendering

Matching pgflock's `renderHelpKey` pattern:
```
[q Quit]  [Tab Next]  [a Approve]  [Enter Select]
```
Where `q` is `ColorCyan` and `Quit` is `ColorTextDim`.

### Unicode Characters

```go
const (
    // Brand
    WhiskeyEmoji   = "\U0001F943"  // whiskey glass
    BarrelEmoji    = "\U0001FAD9"  // (fallback: use WhiskeyEmoji everywhere)
    FlameEmoji     = "\U0001F525"  // fire, for bridge execution
    ShieldEmoji    = "\U0001F6E1"  // shield, for proxy/security
    SparklesEmoji  = "\u2728"      // sparkles, completion

    // Status icons (from pgflock)
    IconCheckmark      = "\u2713"  // checkmark
    IconCross          = "\u2717"  // cross
    IconWarning        = "\u26A0"  // warning triangle
    IconSelectionArrow = "\u25B6"  // right-pointing triangle
    IconFree           = "\u25CB"  // circle
    IconDot            = "\u25CF"  // filled circle

    // Timer
    IconHourglass = "\u23F3"       // hourglass
    IconTimer     = "\u23F1"       // stopwatch

    // Navigation
    NavArrows   = "\u2191\u2193"   // up/down arrows
    NavLeftRight = "\u2190\u2192"  // left/right arrows
    BorderLightH = "\u2500"        // light horizontal line
    BorderHeavyH = "\u2501"        // heavy horizontal line

    // Tab indicators
    TabActive   = "\u25CF"         // filled circle (active tab dot)
    TabInactive = "\u25CB"         // open circle (inactive tab dot)

    // Container
    IconContainer = "\U0001F4E6"   // package box
    IconProxy     = "\U0001F6E1"   // shield

    // Proxy
    IconAllowed = "\u2713"         // checkmark (allowed)
    IconBlocked = "\u2717"         // cross (blocked)
    IconPending = "\u23F3"         // hourglass (pending)
)
```

### Animation Timing Constants

```go
const (
    // Timer countdown pulse (amber heartbeat)
    TimerPulseInterval = 100 * time.Millisecond  // 10 fps, 5-frame cycle = 500ms

    // Approval shimmer (green wave on approve)
    ApprovalShimmerInterval = 50 * time.Millisecond
    ApprovalShimmerDuration = 2000 * time.Millisecond

    // Denial flash (red strobe on deny/timeout)
    DenialFlashInterval = 80 * time.Millisecond
    DenialFlashDuration = 800 * time.Millisecond

    // Loading screen barrel animation
    LoadingFrameInterval = 100 * time.Millisecond

    // UI refresh for timers
    TickInterval = 200 * time.Millisecond  // faster than pgflock's 1s for countdown precision

    // Tab switch animation (instant, no delay)
    TabSwitchDelay = 0

    // Health check interval
    HealthCheckInterval = 5 * time.Second

    // Log auto-scroll debounce
    LogScrollDebounce = 50 * time.Millisecond
)
```

---

## 3. Shared Components

### 3.1 Tab Bar Component

Used in: main navigation, proxy sub-tabs, bridge sub-tabs.

```
  Containers    Proxy    Bridge    Configure    About
      *
```

- Each tab name rendered in `ColorCyan` when active (Bold), `ColorTextDim` when inactive.
- Active tab has a `ColorCopper` dot (`*` = `TabActive`) below it.
- Tabs separated by 4 spaces.
- Navigation: `Tab` / `Shift+Tab` to cycle, or `1`-`5` number keys for direct jump.

**ASCII Layout (active tab = Proxy):**
```
  Containers    Proxy    Bridge    Configure    About
                  *
```

### 3.2 Progress Bar Component

Reuses pgflock's ProgressBar pattern with cooper colors.

```go
NewProgressBar(
    WithWidth(20),
    WithColors(ColorBourbon, ColorBorder),  // warm gold fill, dark empty
)
```

Rendering at 60%:
```
  ━━━━━━━━━━━━────────
```
Where filled segments are `ColorBourbon` (#d97706) and empty segments are `ColorBorder` (#2d3748).

### 3.3 Countdown Timer Component

Used in proxy monitor for pending request approval timeouts.

**Visual:** `[04.2s]` -- shows seconds remaining with one decimal.

**Animation:** 5-frame amber heartbeat pulse cycle (matches pgflock's LOCKED animation pattern):
- Frame 0: `ColorAmberDim` (#92400e)
- Frame 1: `ColorAmber` (#fbbf24)
- Frame 2: `ColorAmberBright` (#fde68a) -- peak brightness
- Frame 3: `ColorAmber` (#fbbf24)
- Frame 4: `ColorAmberDim` (#92400e)

Cycle duration: 500ms (100ms per frame). Pulses faster as time runs low:
- >3s remaining: normal 500ms cycle
- 1-3s remaining: 300ms cycle (60ms per frame)
- <1s remaining: 200ms cycle (40ms per frame)

When expired: solid `ColorDenied` (#f87171), no animation.

### 3.4 Modal Dialog

Reuses pgflock's modal pattern: double-border box with `ColorAmber` border, centered overlay on
dimmed main view.

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Modal Title               |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |       Body text line 1                             |
  |       Body text line 2                             |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Confirm]    [Esc Cancel]      |
  |                                                    |
  +==================================================+
```

- Border: `lipgloss.DoubleBorder()`, `ColorAmber`
- Title: `ColorTextBright`, Bold, centered, width 44
- Body: `ColorTextMuted`, centered
- Confirm button: `ColorAllowed`, Bold
- Cancel button: `ColorTextDim`
- Background: main view lines dimmed via `ColorTextFaint`

### 3.5 Scrollable List Component

Used across all list views (containers, proxy histories, bridge logs/routes).

- Selected row: `ColorVoid` text on `ColorCyan` background (matching pgflock's `RowSelectedStyle`)
- Normal row: `ColorTextBright` text
- Arrow indicator: `IconSelectionArrow` on selected row
- Scroll indicator in footer: `"1-20/45 44%"` in `ColorTextDim`
- Navigation: `Up/Down` or `j/k` to move selection, scroll offset auto-adjusts (pgflock pattern)

### 3.6 Two-Pane Layout

Used in proxy monitor, blocked/allowed detail views.

```
  LEFT PANE (50%)              |  RIGHT PANE (50%)
                               |
  Scrollable list              |  Detail view for
  of items                     |  selected item
                               |
                               |
```

- Vertical divider: single `|` character in `ColorBorder`, full height of content area
- Pane width: calculated as `(termWidth - 1) / 2` each (1 char for divider)
- Left pane has scrollable list with selection
- Right pane shows detail of currently selected item, scrollable independently via `Shift+Up/Down`

---

## 4. cooper up -- Loading Screen

Displayed on startup while proxy container starts, networks are created, and the system initializes.

### ASCII Mockup (centered in terminal)

```
                    . whiskey_glass .
                   .  whiskey_glass  . .
                  .  whiskey_glass  . . .


                    c o o p e r


              barrel-proof containers


        ━━━━━━━━━━━━━━━━────────────────


             Creating networks...


           cooper-internal   waiting...
           cooper-external   waiting...
           cooper-proxy      waiting...
           bridge API        waiting...


              [q Cancel]  whiskey_glass
```

### States

**Animating (startup in progress):**
- Whiskey glass emoji animates with surrounding dots (matching pgflock sheep pattern):
  - Frame 0: `. whiskey_glass .`
  - Frame 1: `. whiskey_glass . .`
  - Frame 2: `. whiskey_glass . . .`
  - Frame 3: `. whiskey_glass . .`
- Title `c o o p e r` in `ColorCyan`, Bold (spaced lettering matches pgflock's `p g f l o c k`)
- Subtitle in `ColorTextDim`, italic
- Progress bar: 20 chars wide, `ColorBourbon` fill / `ColorBorder` empty
- Status message in `ColorTextDim`
- Component checklist: each line shows name + status

**Component Checklist Items:**
```
  cooper-internal   checkmark ready          (ColorAllowed when ready)
  cooper-external   checkmark ready          (ColorAllowed when ready)
  cooper-proxy      checkmark ready          (ColorAllowed when ready)
  bridge API        waiting...       (ColorTextDim while waiting)
```

**Completed:**
- Dots become sparkles: `sparkles whiskey_glass sparkles`
- Subtitle changes to `"barrels ready to roll"`
- Progress bar at 100%
- All checklist items show checkmark

**Failed:**
- Whiskey glass with exclamation: `whiskey_glass !`
- Subtitle changes to `"startup failed"`
- Error message in `ColorDenied`, Bold
- Help bar: `[q Quit]`

### Loading Steps and Progress Mapping

| Step                  | Target Progress | Status Message               |
|-----------------------|-----------------|------------------------------|
| Init                  | 0.00            | "Initializing..."            |
| Creating networks     | 0.15            | "Creating Docker networks..."  |
| Starting proxy        | 0.35            | "Starting proxy container..."  |
| Configuring SSL       | 0.50            | "Configuring SSL bump..."      |
| Starting bridge       | 0.70            | "Starting execution bridge..." |
| Version check         | 0.85            | "Checking tool versions..."    |
| Ready                 | 1.00            | "Ready!"                       |

Progress animation uses pgflock's staggered pattern: display progress animates toward target in 20%
increments at 50ms intervals, holds at 100% for 1 second before transitioning to main view.

### Color Specification

| Element            | Color             | Style      |
|--------------------|-------------------|------------|
| Whiskey emoji      | native emoji      | --         |
| Animated dots      | `ColorTextDim`    | --         |
| Title text         | `ColorCyan`       | Bold       |
| Subtitle text      | `ColorTextDim`    | --         |
| Progress filled    | `ColorBourbon`    | --         |
| Progress empty     | `ColorBorder`     | --         |
| Status message     | `ColorTextDim`    | --         |
| Checklist ready    | `ColorAllowed`    | --         |
| Checklist waiting  | `ColorTextDim`    | --         |
| Error message      | `ColorDenied`     | Bold       |
| Help key           | `ColorCyan`       | --         |
| Help description   | `ColorTextDim`    | --         |

### Key Bindings

| Key        | Action                                       |
|------------|----------------------------------------------|
| `q`        | Cancel startup, quit (cleanup started resources) |
| `Ctrl+C`   | Same as `q`                                  |

### State Transitions

| From                | Trigger                    | To                   |
|---------------------|----------------------------|----------------------|
| (app start)         | `cooper up` invoked        | Loading Screen       |
| Loading Screen      | All steps complete         | Main view (Containers tab) |
| Loading Screen      | Step failure               | Loading Screen (error state) |
| Loading Screen (error) | `q` pressed            | App exits            |
| Loading Screen      | `q` pressed                | App exits (cleanup)  |

---

## 5. cooper up -- Main Tab Bar

The persistent navigation bar visible across all `cooper up` tabs. This is not a standalone screen but
a shared component rendered at the top of every main view.

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject  barrel-otherproj
  ──────────────────────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
        *
  ──────────────────────────────────────────────────────────────────────────────────────────────────
```

### Header Line (Line 1)

**Left side:**
- `whiskey_glass cooper` -- brand, `ColorCyan` Bold (matches pgflock's `SheepEmoji + " pgflock"`)
- `shield Proxy: 3128` -- proxy status with port, `ColorTextDim` label, `ColorAmber` port number
- `flame Bridge: 4343` -- bridge status with port, `ColorTextDim` label, `ColorAmber` port number

**Right side:**
- Active barrel containers listed: `barrel-myproject` `barrel-otherproj` in `ColorViolet`
- If no barrels running: `(no barrels)` in `ColorTextDim` italic

### Tab Bar (Line 3)

**Tab Names and Shortcuts:**
| Tab          | Shortcut | Sub-tabs                              |
|--------------|----------|---------------------------------------|
| Containers   | `1`      | --                                    |
| Proxy        | `2`      | Monitor, Blocked, Allowed             |
| Bridge       | `3`      | Logs, Routes                          |
| Configure    | `4`      | --                                    |
| About        | `5`      | --                                    |

**Visual states:**
- Active tab: `ColorCyan`, Bold, with `ColorCopper` dot below
- Inactive tab: `ColorTextDim`
- Tab with notification (e.g., pending proxy requests): tab name gets `ColorAmber` + count badge like `Proxy (3)`

### Color Specification

| Element              | Color             | Style      |
|----------------------|-------------------|------------|
| Brand "cooper"       | `ColorCyan`       | Bold       |
| Whiskey emoji        | native            | --         |
| Shield/Flame emoji   | native            | --         |
| Port numbers         | `ColorAmber`      | --         |
| Service labels       | `ColorTextDim`    | --         |
| Active tab name      | `ColorCyan`       | Bold       |
| Active tab dot       | `ColorCopper`     | --         |
| Inactive tab name    | `ColorTextDim`    | --         |
| Notification badge   | `ColorAmber`      | Bold       |
| Barrel names         | `ColorViolet`     | --         |
| Separator lines      | `ColorBorder`     | --         |

### Key Bindings (Global, always active)

| Key          | Action                                  |
|--------------|-----------------------------------------|
| `Tab`        | Next tab                                |
| `Shift+Tab`  | Previous tab                            |
| `1`-`5`      | Jump to tab by number                   |
| `q`          | Open exit confirmation modal             |
| `Ctrl+C`     | Same as `q`                             |

For tabs with sub-tabs (Proxy, Bridge):
| Key          | Action                                  |
|--------------|-----------------------------------------|
| `[`          | Previous sub-tab                        |
| `]`          | Next sub-tab                            |

### State Transitions

| From           | Trigger              | To                        |
|----------------|----------------------|---------------------------|
| Any tab        | `Tab`                | Next tab (wraps around)   |
| Any tab        | `Shift+Tab`          | Previous tab (wraps)      |
| Any tab        | Number key `1`-`5`   | Specific tab              |
| Proxy sub-tab  | `[` or `]`           | Adjacent Proxy sub-tab    |
| Bridge sub-tab | `[` or `]`           | Adjacent Bridge sub-tab   |
| Any tab        | `q`                  | Exit confirmation modal   |

---

## 6. cooper up -- Containers Tab

Lists all running cooper containers with resource usage. Primary management interface for
starting/stopping containers.

### ASCII Mockup (with data)

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
        *
  ──────────────────────────────────────────────────────────────────────────────────

     CONTAINER              STATUS     CPU     MEM       UPTIME     NETWORK
  => cooper-proxy            running   0.3%    42 MB     2h 14m     ext+int
     barrel-myproject        running   1.2%    128 MB    1h 03m     internal
     barrel-otherproj        running   0.8%    96 MB     45m 12s    internal

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [s Start] [x Stop] [r Restart] [up/down Nav]   proxy checkmark  bridge checkmark  whiskey_glass
```

### Column Layout

| Column      | Width  | Alignment | Content                          |
|-------------|--------|-----------|----------------------------------|
| Selector    | 3      | Left      | `=>` arrow or `  ` blank         |
| CONTAINER   | 24     | Left      | Container name                   |
| STATUS      | 10     | Left      | running/stopped/starting         |
| CPU         | 7      | Right     | CPU percentage                   |
| MEM         | 10     | Right     | Memory in MB                     |
| UPTIME      | 12     | Right     | Duration since start             |
| NETWORK     | 10     | Left      | Network attachment               |

### Column Header Style

- Header labels in `ColorTextDim`, uppercase
- 2-space left margin from terminal edge

### Row Styles

**Selected row:**
- `ColorVoid` text on `ColorCyan` background, Bold (matches pgflock RowSelectedStyle)
- Arrow: `IconSelectionArrow` (`=>`)

**Normal row:**
- `ColorTextBright` text

**Status indicators:**
| Status     | Color           | Icon               |
|------------|-----------------|---------------------|
| running    | `ColorAllowed`  | `IconCheckmark`     |
| stopped    | `ColorDenied`   | `IconCross`         |
| starting   | `ColorAmber`    | animated dots       |
| restarting | `ColorAmber`    | animated dots       |

**CPU coloring (threshold-based):**
- 0-50%: `ColorTextBright`
- 50-80%: `ColorAmber`
- 80%+: `ColorDenied`

**Memory coloring:**
- 0-256 MB: `ColorTextBright`
- 256-512 MB: `ColorAmber`
- 512 MB+: `ColorDenied`

### Empty State (no containers)

```
                whiskey_glass

          No containers running

     Run "cooper up" to start the proxy,
     then "cooper cli" to open a barrel.
```
Centered vertically and horizontally. Whiskey emoji, text in `ColorTextDim` italic.

### Error State

When a container action fails (start/stop/restart), an error message appears below the table:
```
  Error: failed to restart cooper-proxy: container already restarting
```
In `ColorDenied`, Bold. Clears after 5 seconds or on next key press.

### Key Bindings

| Key        | Action                                        |
|------------|-----------------------------------------------|
| `Up/k`     | Move selection up                             |
| `Down/j`   | Move selection down                           |
| `s`        | Start selected container                      |
| `x`        | Stop selected container (opens confirm modal) |
| `r`        | Restart selected container                    |
| `Tab`      | Next main tab                                 |

### State Transitions

| From              | Trigger            | To                          |
|-------------------|--------------------|-----------------------------|
| Containers tab    | `s` on stopped     | Container starts, status animates |
| Containers tab    | `x` on running     | Stop confirmation modal     |
| Stop modal        | `Enter/y`          | Container stops             |
| Stop modal        | `Esc/n`            | Back to Containers tab      |
| Containers tab    | `r` on running     | Restart confirmation modal  |
| Restart modal     | `Enter/y`          | Container restarts          |

---

## 7. cooper up -- Proxy Monitor Tab

The real-time approval/denial UI for non-whitelisted HTTP requests. This is the security-critical
screen where the user makes live decisions about what the AI can access.

### ASCII Mockup (with pending requests)

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                      *
       Monitor   Blocked   Allowed
          *
  ──────────────────────────────────────────────────────────────────────────────────

  PENDING REQUESTS (3)              |  REQUEST DETAILS
                                    |
  => [02.1s] GET stackoverflow.co.. |  URL
     [04.8s] POST api.example.com.. |    https://stackoverflow.com/questions/12345
     [01.3s] GET cdn.jsdelivr.net.. |
                                    |  Method
                                    |    GET
                                    |
                                    |  Container
                                    |    barrel-myproject
                                    |
                                    |  Headers
                                    |    User-Agent: node-fetch/2.6.7
                                    |    Accept: application/json
                                    |    Host: stackoverflow.com
                                    |
                                    |  Timestamp
                                    |    2026-03-27 14:32:05
                                    |

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [a Approve] [d Deny] [up/down Nav]   3 pending  whiskey_glass
```

### Left Pane -- Pending Request List

**Layout per row:**
```
  [02.1s] GET stackoverflow.com/questions/12345
```

- Timer: `[02.1s]` with countdown pulse animation (see Section 3.3)
- Method: `GET`/`POST`/etc in `ColorCyan`, Bold
- URL: truncated to fit pane width, in `ColorTextBright`
- Sorted by time remaining ascending (most urgent at top)

**Selected row:**
- Full row gets `ColorCyan` background with `ColorVoid` text (matching pgflock selection)
- Arrow prefix: `=> `

**Timer expiration behavior:**
- When timer hits 0, the row flashes `ColorDenied` for 800ms (3 frames at 80ms, DenialFlashInterval),
  then the request is removed from the list (auto-denied)
- Denial flash animation frames:
  - Frame 0: `ColorDenied` background, `ColorTextBright` text
  - Frame 1: `ColorSurface` background (normal)
  - Frame 2: `ColorDenied` background, `ColorTextBright` text
  - Then removed

**Pane header:** `PENDING REQUESTS (3)` where count is `ColorAmber`, Bold. Label in `ColorTextDim`.

### Right Pane -- Request Details

Shows details of the currently selected pending request. This only shows request-side data (response
does not exist yet while pending).

**Fields displayed:**
| Field      | Label Color     | Value Color      | Content                     |
|------------|-----------------|------------------|-----------------------------|
| URL        | `ColorTextDim`  | `ColorTextBright`| Full URL (wraps if needed)  |
| Method     | `ColorTextDim`  | `ColorCyan`      | HTTP method                 |
| Container  | `ColorTextDim`  | `ColorViolet`    | barrel-{name}               |
| Headers    | `ColorTextDim`  | `ColorTextBright`| Key: Value, one per line    |
| Timestamp  | `ColorTextDim`  | `ColorTextDim`   | ISO 8601 format             |

**Labels are indented 2 spaces, values indented 4 spaces.**

### Empty State (no pending requests)

Left pane:
```
              whiskey_glass

        All quiet on the wire

      No pending requests to review
```
Centered in left pane. `ColorTextDim` italic.

Right pane: empty (or shows same message centered).

### Approval Animation

When user presses `a` to approve:
1. The approved row gets a green shimmer animation (matching pgflock's CopyShimmer pattern):
   - 5-frame color cycle: `ColorEmerald` -> `ColorAllowed` -> `ColorMint` -> `ColorAllowed` -> `ColorEmerald`
   - Shimmer moves left-to-right across the row text at 50ms per frame
   - Total duration: 1000ms
2. Row text briefly shows `checkmark APPROVED` in `ColorAllowed`
3. Row fades out (removed from list)

### Denial Animation

When user presses `d` to deny (or timer expires):
1. Row flashes red (DenialFlash pattern, 800ms)
2. Row text briefly shows `cross DENIED` in `ColorDenied`
3. Row removed from list

### Key Bindings

| Key        | Action                                         |
|------------|------------------------------------------------|
| `Up/k`     | Select previous pending request                |
| `Down/j`   | Select next pending request                    |
| `a`        | Approve selected request                       |
| `Enter`    | Same as `a` (approve)                          |
| `d`        | Deny selected request immediately              |
| `[`        | Switch to previous sub-tab                     |
| `]`        | Switch to next sub-tab (Blocked)               |
| `Tab`      | Next main tab                                  |
| `Shift+Up`   | Scroll right pane up                         |
| `Shift+Down` | Scroll right pane down                       |

### State Transitions

| From           | Trigger                     | To                        |
|----------------|-----------------------------|---------------------------|
| Monitor tab    | New request arrives (chan)   | Request added to top of list |
| Monitor tab    | `a`/`Enter` on request      | Approval shimmer, then removed |
| Monitor tab    | `d` on request              | Denial flash, then removed |
| Monitor tab    | Timer expires on request    | Auto-deny flash, then removed |
| Monitor tab    | `]`                         | Blocked sub-tab           |

---

## 8. cooper up -- Proxy Blocked Tab

History of denied/blocked requests. Read-only list with detail view.

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                      *
       Monitor   Blocked   Allowed
                    *
  ──────────────────────────────────────────────────────────────────────────────────

  BLOCKED HISTORY (47/500)          |  REQUEST DETAILS
                                    |
  => cross 14:32:05  GET stackover..|  URL
     cross 14:31:42  POST api.examp.|    https://stackoverflow.com/questions/12345
     cross 14:30:18  GET cdn.jsdeliv|
     cross 14:29:55  GET malicious..|  Method      GET
     cross 14:28:01  POST upload.se.|  Status      403 Forbidden
                                    |  Container   barrel-myproject
                                    |  Reason      Timeout (5s)
                                    |
                                    |  Request Headers
                                    |    User-Agent: node-fetch/2.6.7
                                    |    Accept: */*
                                    |
                                    |  Timestamp
                                    |    2026-03-27 14:32:05
                                    |

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [[ ] Sub-tab] [up/down Nav]   47 blocked  whiskey_glass
```

### Left Pane -- Blocked Request List

**Row format:**
```
  cross 14:32:05  GET stackoverflow.com/questions/...
```

- `cross` icon in `ColorDenied`
- Timestamp in `ColorTextDim`
- Method in `ColorCyan`
- URL truncated to fit, in `ColorTextBright`
- Most recent at top

**Pane header:** `BLOCKED HISTORY (47/500)` -- current count / max cap. Count in `ColorDenied`, max in `ColorTextDim`.

### Right Pane -- Request Details

Same layout as Monitor detail pane, plus:
- **Status**: `403 Forbidden` in `ColorDenied`
- **Reason**: "Timeout (5s)" or "Manual deny" in `ColorTextDim`

### Empty State

```
              shield

        No blocked requests yet

   Requests denied by timeout or manual
   action will appear here.
```

### Key Bindings

| Key          | Action                              |
|--------------|-------------------------------------|
| `Up/k`       | Select previous entry               |
| `Down/j`     | Select next entry                   |
| `[`          | Previous sub-tab (Monitor)          |
| `]`          | Next sub-tab (Allowed)              |
| `Tab`        | Next main tab                       |
| `Shift+Up`   | Scroll right pane up                |
| `Shift+Down` | Scroll right pane down              |

### State Transitions

| From         | Trigger                    | To                    |
|--------------|----------------------------|-----------------------|
| Blocked tab  | New blocked event (chan)    | Entry added to top    |
| Blocked tab  | `[`                        | Monitor sub-tab       |
| Blocked tab  | `]`                        | Allowed sub-tab       |

---

## 9. cooper up -- Proxy Allowed Tab

History of approved/whitelisted requests with response data.

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                      *
       Monitor   Blocked   Allowed
                              *
  ──────────────────────────────────────────────────────────────────────────────────

  ALLOWED HISTORY (123/500)         |  REQUEST + RESPONSE
                                    |
  => checkmark 14:32:08  GET api.an.|  URL
     checkmark 14:31:55  POST api.a.|    https://api.anthropic.com/v1/messages
     checkmark 14:31:40  GET raw.gi.|
     checkmark 14:30:02  GET api.an.|  Method      POST
                                    |  Status      200 OK
                                    |  Container   barrel-myproject
                                    |  Source      Whitelist (.anthropic.com)
                                    |
                                    |  Request Headers
                                    |    Authorization: Bearer sk-...
                                    |    Content-Type: application/json
                                    |
                                    |  Response Headers
                                    |    Content-Type: application/json
                                    |    X-Request-Id: req_abc123
                                    |
                                    |  Timestamp
                                    |    2026-03-27 14:32:08
                                    |

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [[ ] Sub-tab] [up/down Nav]   123 allowed  whiskey_glass
```

### Left Pane -- Allowed Request List

**Row format:**
```
  checkmark 14:32:08  GET api.anthropic.com/v1/messages
```

- `checkmark` icon in `ColorAllowed`
- Timestamp in `ColorTextDim`
- Method in `ColorCyan`
- URL truncated to fit, in `ColorTextBright`
- Most recent at top

**Pane header:** `ALLOWED HISTORY (123/500)` -- count in `ColorAllowed`, max in `ColorTextDim`.

### Right Pane -- Request + Response Details

Same as blocked, but includes:
- **Status**: `200 OK` in `ColorAllowed` (color varies by status code range)
  - 2xx: `ColorAllowed`
  - 3xx: `ColorCyan`
  - 4xx: `ColorAmber`
  - 5xx: `ColorDenied`
- **Source**: "Whitelist (.anthropic.com)" or "Manual approval" in `ColorTextDim`
- **Response Headers**: added section below request headers

### Empty State

```
              checkmark

        No allowed requests yet

   Approved and whitelisted requests
   will appear here.
```

### Key Bindings

Same as Blocked tab, with `[` going to Blocked and `]` wrapping to Monitor.

### State Transitions

Same pattern as Blocked tab, with allowed events arriving via channel.

---

## 10. cooper up -- Execution Bridge Logs Tab

Live log viewer for execution bridge API calls.

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                                 *
       Logs   Routes
        *
  ──────────────────────────────────────────────────────────────────────────────────

  BRIDGE LOGS (12/500)              |  EXECUTION DETAILS
                                    |
  => 14:32:05  /deploy-staging  0.. |  Route
     14:31:42  /go-mod-tidy     0.. |    /deploy-staging
     14:30:18  /restart-dev     1.. |
                                    |  Script
                                    |    ~/scripts/deploy-staging.sh
                                    |
                                    |  Container
                                    |    barrel-myproject
                                    |
                                    |  Duration    1.24s
                                    |  Exit Code   0
                                    |
                                    |  stdout
                                    |  ---------------------------------
                                    |    Deploying to staging...
                                    |    Build successful
                                    |    Deployed to staging-abc123
                                    |
                                    |  stderr
                                    |  ---------------------------------
                                    |    (empty)
                                    |
                                    |  Timestamp
                                    |    2026-03-27 14:32:05
                                    |

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [[ ] Sub-tab] [up/down Nav]   flame localhost:4343  whiskey_glass
```

### Left Pane -- Log Entry List

**Row format:**
```
  14:32:05  /deploy-staging  0  1.24s
```

- Timestamp in `ColorTextDim`
- API route in `ColorCyan`
- Exit code: `0` in `ColorAllowed`, non-zero in `ColorDenied`
- Duration in `ColorTextDim`
- Most recent at top

**Pane header:** `BRIDGE LOGS (12/500)` -- count in `ColorCyan`, max in `ColorTextDim`.

### Right Pane -- Execution Details

| Field       | Label Color     | Value Color       |
|-------------|-----------------|-------------------|
| Route       | `ColorTextDim`  | `ColorCyan`       |
| Script      | `ColorTextDim`  | `ColorTextBright` |
| Container   | `ColorTextDim`  | `ColorViolet`     |
| Duration    | `ColorTextDim`  | `ColorTextBright` |
| Exit Code   | `ColorTextDim`  | `ColorAllowed` (0) / `ColorDenied` (non-0) |
| stdout      | `ColorTextDim`  | `ColorTextBright` (in a bordered box) |
| stderr      | `ColorTextDim`  | `ColorDenied` (in a bordered box, if non-empty) |

### Empty State

```
              flame

        No bridge executions yet

   Bridge API calls from CLI containers
   will appear here.
```

### Key Bindings

| Key          | Action                                |
|--------------|---------------------------------------|
| `Up/k`       | Select previous log entry             |
| `Down/j`     | Select next log entry                 |
| `[`          | Previous sub-tab (wraps to Routes)    |
| `]`          | Next sub-tab (Routes)                 |
| `Tab`        | Next main tab                         |
| `Shift+Up`   | Scroll right pane up                  |
| `Shift+Down` | Scroll right pane down                |

---

## 11. cooper up -- Execution Bridge Routes Tab

Management screen for bridge route mappings (API path to script file).

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                                 *
       Logs   Routes
                *
  ──────────────────────────────────────────────────────────────────────────────────

     API PATH                SCRIPT                          CALLS
  => /deploy-staging         ~/scripts/deploy-staging.sh     14
     /go-mod-tidy            ~/scripts/go-mod-tidy.sh        8
     /restart-dev            ~/scripts/restart-dev.sh        3

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [n New] [e Edit] [x Delete] [up/down Nav]   whiskey_glass
```

### Guidance Note

Below the route table, when the list is short (fewer than 5 entries), show guidance text:

```
  -----------------------------------------------
  tip: Bridge routes map API paths to local scripts.
  The AI can call localhost:4343/deploy-staging from inside the container.
  Best practice: scripts should take no input and handle concurrency.
  -----------------------------------------------
```

In `ColorTextDim`, with `ColorBorder` separator lines above/below.

### Column Layout

| Column    | Width  | Alignment | Content                |
|-----------|--------|-----------|------------------------|
| Selector  | 3      | Left      | `=>` or blank          |
| API PATH  | 22     | Left      | Route path             |
| SCRIPT    | 40     | Left      | Script file path       |
| CALLS     | 8      | Right     | Total execution count  |

### Route Entry Modal (for New/Edit)

When `n` (new) or `e` (edit) is pressed, a modal appears:

```
  +==================================================+
  |                                                    |
  |            flame  Bridge Route                     |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |  API Path:                                         |
  |  > /deploy-staging_                                |
  |                                                    |
  |  Script Path:                                      |
  |  > ~/scripts/deploy-staging.sh_                    |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Save]    [Esc Cancel]         |
  |                                                    |
  +==================================================+
```

- Text input fields with `_` cursor indicator
- `ColorCyan` field labels
- `ColorTextBright` input text
- Active input field has `ColorAmber` `>` prefix
- `Tab` switches between fields within the modal

### Delete Confirmation Modal

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Delete Route?             |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |       /deploy-staging                              |
  |       ~/scripts/deploy-staging.sh                  |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Delete]    [Esc Cancel]       |
  |                                                    |
  +==================================================+
```

### Empty State

```
              flame

        No bridge routes configured

   Press [n] to add a route. Routes map
   API paths to local scripts that AI
   tools can execute from the container.
```

### Key Bindings

| Key        | Action                            |
|------------|-----------------------------------|
| `Up/k`     | Select previous route             |
| `Down/j`   | Select next route                 |
| `n`        | New route (opens modal)           |
| `e`        | Edit selected route (opens modal) |
| `Enter`    | Same as `e`                       |
| `x`        | Delete selected route (confirm)   |
| `[`        | Previous sub-tab (Logs)           |
| `]`        | Next sub-tab (wraps to Logs)      |
| `Tab`      | Next main tab                     |

**Inside route entry modal:**
| Key        | Action                            |
|------------|-----------------------------------|
| `Tab`      | Move to next input field          |
| `Enter`    | Save route                        |
| `Esc`      | Cancel                            |
| typing     | Standard text input               |

### State Transitions

| From         | Trigger         | To                           |
|--------------|-----------------|------------------------------|
| Routes tab   | `n`             | New route modal              |
| Routes tab   | `e`/`Enter`     | Edit route modal             |
| Routes tab   | `x`             | Delete confirmation modal    |
| Route modal  | `Enter` (valid) | Route saved, back to list    |
| Route modal  | `Esc`           | Back to list (no changes)    |
| Delete modal | `Enter/y`       | Route deleted, back to list  |
| Delete modal | `Esc/n`         | Back to list (no changes)    |

---

## 12. cooper up -- Configure Tab

Runtime settings that take effect immediately. This is for `cooper up` operational settings, not
container/Dockerfile settings (those are in `cooper configure`).

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                                              *
  ──────────────────────────────────────────────────────────────────────────────────

    RUNTIME SETTINGS
    Changes take effect immediately.

     SETTING                              VALUE
  => Monitor approval timeout             [  5 ] seconds
     Blocked history limit                [ 500 ] entries
     Allowed history limit                [ 500 ] entries
     Bridge log limit                     [ 500 ] entries

    ─────────────────────────────────────────────────
    Full logs are always written to ~/.cooper/logs/
    regardless of these display limits.
    ─────────────────────────────────────────────────

    To change proxy whitelist, port forwarding,
    AI tools, or programming tools, run:
      cooper configure

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next] [Enter Edit] [up/down Nav]   whiskey_glass
```

### Setting Row Layout

```
  => Monitor approval timeout             [  5 ] seconds
```

- Setting name: `ColorTextBright`
- Value box: `[ 5 ]` with `ColorAmber` brackets and `ColorTextBright` value
- Unit label: `ColorTextDim`
- Selected row: `ColorCyan` background (standard selection)

### Edit Mode

When `Enter` is pressed on a selected setting, the value becomes editable:

```
  => Monitor approval timeout             [ 10_ ] seconds
                                           ^^^^^ editable, cursor shown
```

- Value box border changes to `ColorCyan`
- Cursor shown as `_`
- Type numbers to change value
- `Enter` to confirm, `Esc` to cancel

### Validation

- Monitor approval timeout: 1-30 seconds (integer)
- History limits: 100-10000 entries (integer)
- Invalid values show inline error: `"Must be 1-30"` in `ColorDenied` next to the field

### Key Bindings

| Key        | Action                                    |
|------------|-------------------------------------------|
| `Up/k`     | Select previous setting                   |
| `Down/j`   | Select next setting                       |
| `Enter`    | Enter edit mode for selected setting      |
| `Tab`      | Next main tab                             |

**In edit mode:**
| Key        | Action                                    |
|------------|-------------------------------------------|
| `0`-`9`    | Type digit                                |
| `Backspace`| Delete last digit                         |
| `Enter`    | Confirm new value                         |
| `Esc`      | Cancel edit, restore previous value       |

### Guidance Text

Below the settings table:

```
  Full logs are always written to ~/.cooper/logs/
  regardless of these display limits.
```
In `ColorTextDim`, with `ColorBorder` separator lines.

```
  To change proxy whitelist, port forwarding,
  AI tools, or programming tools, run:
    cooper configure
```
Where `cooper configure` is in `ColorCyan`.

---

## 13. cooper up -- About Tab

Shows tool versions, system information, and cooper status.

### ASCII Mockup

```
  whiskey_glass cooper  shield Proxy: 3128  flame Bridge: 4343  barrel-myproject
  ──────────────────────────────────────────────────────────────────────────────────
    Containers      Proxy      Bridge      Configure      About
                                                            *
  ──────────────────────────────────────────────────────────────────────────────────

    whiskey_glass COOPER v0.1.0
    barrel-proof containers for AI assistants

    ─────────────────────────────────────────────────

    SYSTEM STATUS

    Proxy          shield  checkmark running on :3128            Squid 6.10
    Bridge         flame  checkmark running on :4343
    CA Certificate      checkmark valid, expires 2028-03-27
    Docker              checkmark Engine 27.0.3

    ─────────────────────────────────────────────────

    INSTALLED TOOLS                    CONTAINER     HOST        MODE

    Programming
      Go                               1.23.0       1.23.0      mirror
      Node.js                          22.5.1       22.5.1      mirror
      Python                           3.12.4       3.12.4      mirror

    AI CLIs
      Claude Code                      1.0.12       1.0.12      latest
      Codex                            0.9.3        --          latest

    ─────────────────────────────────────────────────

    warning Version mismatch detected:
      Claude Code: container has 1.0.10, latest is 1.0.12
      Run "cooper update" to update.

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Tab Next]   whiskey_glass
```

### Sections

**Header:**
- `whiskey_glass COOPER v0.1.0` in `ColorCyan`, Bold
- Tagline in `ColorTextDim`

**System Status:**
| Component      | Healthy Color    | Unhealthy Color  | Details                |
|----------------|------------------|------------------|------------------------|
| Proxy          | `ColorAllowed`   | `ColorDenied`    | port, Squid version    |
| Bridge         | `ColorAllowed`   | `ColorDenied`    | port                   |
| CA Certificate | `ColorAllowed`   | `ColorAmber`     | expiry date            |
| Docker         | `ColorAllowed`   | `ColorDenied`    | Engine version         |

**Installed Tools Table:**
| Column      | Width  | Color             |
|-------------|--------|-------------------|
| Tool name   | 28     | `ColorTextBright` |
| CONTAINER   | 14     | `ColorAllowed` (match) or `ColorAmber` (mismatch) |
| HOST        | 12     | `ColorTextDim`    |
| MODE        | 10     | `ColorTextDim`    |

Version mismatch rows: container version in `ColorAmber` instead of `ColorAllowed`.

**Version Mismatch Warning:**
When any tool has a version mismatch (mirror mode: container != host; latest mode: container != latest):
```
  warning Version mismatch detected:
    Claude Code: container has 1.0.10, latest is 1.0.12
    Run "cooper update" to update.
```
- `warning` icon and first line in `ColorAmber`, Bold
- Details in `ColorTextMuted`
- `cooper update` in `ColorCyan`

### Key Bindings

| Key        | Action              |
|------------|---------------------|
| `Tab`      | Next main tab       |
| `q`        | Exit confirmation   |

---

## 14. cooper up -- Exit Confirmation Modal

Shown when user presses `q` to quit `cooper up`. Quitting stops all cooper containers.

### ASCII Mockup

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Quit Cooper?              |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |    This will stop the proxy container              |
  |    and all running barrel containers.              |
  |                                                    |
  |    Active barrels: 2                               |
  |      barrel-myproject                              |
  |      barrel-otherproj                              |
  |                                                    |
  |    AI tools will lose network access.              |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Confirm]    [Esc Cancel]      |
  |                                                    |
  +==================================================+
```

### Content

- Title: `"Quit Cooper?"` with `whiskey_glass` emoji
- Body lists consequences:
  - Proxy container will stop
  - All barrel containers will lose network access (they remain running but isolated)
  - Number and names of active barrels
- Warning line: "AI tools will lose network access." in `ColorAmber`

### With No Active Barrels

```
  This will stop the proxy container.
  No barrel containers are currently running.
```

### Color Specification

| Element              | Color             | Style |
|----------------------|-------------------|-------|
| Modal border         | `ColorAmber`      | Double border |
| Title                | `ColorTextBright` | Bold |
| Body text            | `ColorTextMuted`  | -- |
| Barrel count         | `ColorAmber`      | Bold |
| Barrel names         | `ColorViolet`     | -- |
| Warning line         | `ColorAmber`      | -- |
| Confirm button       | `ColorAllowed`    | Bold |
| Cancel button        | `ColorTextDim`    | -- |

### Key Bindings

| Key        | Action                                    |
|------------|-------------------------------------------|
| `Enter/y`  | Confirm quit, transition to shutdown screen |
| `Esc/n`    | Cancel, return to previous view           |

### State Transitions

| From             | Trigger     | To                     |
|------------------|-------------|------------------------|
| Any tab          | `q`         | Exit confirmation modal |
| Exit modal       | `Enter/y`   | Shutdown screen        |
| Exit modal       | `Esc/n`     | Previous tab           |

---

## 15. cooper up -- Shutdown Screen

Graceful shutdown progress screen, shown after exit confirmation.

### ASCII Mockup

```
                    sleeping whiskey_glass sleeping


                        c o o p e r


                    sealing the barrels...


              ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


                  Stopping proxy container...


                 cooper-proxy      checkmark stopped
                 cooper-internal   checkmark removed
                 cooper-external   checkmark removed
                 bridge API        checkmark stopped


                          whiskey_glass
```

### States

**Shutting down (in progress):**
- `sleeping whiskey_glass sleeping` (matching pgflock's shutdown sheep pattern)
- Title: `c o o p e r` in `ColorCyan`, Bold
- Subtitle: `"sealing the barrels..."` in `ColorTextDim`
- Progress bar animating
- Status message updating
- No cancel key (shutdown cannot be cancelled, matching pgflock)

**Completed:**
- Subtitle changes to `"barrels sealed safely"`
- All checklist items show checkmark
- App exits immediately after 1 second hold at 100%

**Failed:**
- Subtitle: `"shutdown failed"`
- Error message in `ColorDenied`
- Help bar: `[q Force Quit]`

### Shutdown Steps and Progress

| Step                     | Target Progress | Status Message                  |
|--------------------------|-----------------|----------------------------------|
| Init                     | 0.00            | "Preparing shutdown..."          |
| Stopping bridge          | 0.15            | "Stopping execution bridge..."   |
| Stopping barrels         | 0.40            | "Stopping barrel containers..."  |
| Stopping proxy           | 0.65            | "Stopping proxy container..."    |
| Removing networks        | 0.85            | "Removing Docker networks..."    |
| Done                     | 1.00            | "Shutdown complete"              |

### Key Bindings

| Key        | Action                                    |
|------------|-------------------------------------------|
| (none)     | No keys during normal shutdown            |
| `q`        | Force quit (only if shutdown failed)      |

---

## 16. cooper configure -- Welcome / Main Menu

The entry screen for the `cooper configure` setup wizard. This is a separate BubbleTea program from
`cooper up`, but shares the same color palette and component library.

### ASCII Mockup (fresh install)

```
                    sparkles whiskey_glass sparkles


                        c o o p e r

                  barrel-proof containers
                    for AI assistants


  ──────────────────────────────────────────────────────────────────────────────────

    Setup Menu                                     Status

 => 1. Programming Tools                           3 tools detected
    2. AI CLI Tools                                 2 tools detected
    3. Proxy Whitelist                              5 domains
    4. Port Forwarding                              0 rules
    5. Proxy & Bridge Ports                         :3128 / :4343

  ──────────────────────────────────────────────────────────────────────────────────

    checkmark Docker Engine 27.0.3 detected
    checkmark CA certificate found at ~/.cooper/ca/cooper-ca.pem

    Tip: Navigate with arrow keys, press Enter to configure each section.
    When ready, press [s] to save or [b] to save and build.

  ──────────────────────────────────────────────────────────────────────────────────
  [q Quit] [Enter Select] [s Save] [b Save & Build] [up/down Nav]   whiskey_glass
```

### ASCII Mockup (reconfiguration -- existing config)

Same layout but with additional status info:

```
 => 1. Programming Tools                           Go 1.23.0, Node 22.5.1, Python 3.12
    2. AI CLI Tools                                 Claude Code, Codex
    3. Proxy Whitelist                              7 domains (2 custom)
    4. Port Forwarding                              3 rules
    5. Proxy & Bridge Ports                         :3128 / :4343
```

### Layout

**Header area:** Same centered cooper branding as loading screen but with sparkles (not animated).

**Menu area:** Two-column layout.
- Left: numbered menu items with selection arrow
- Right: brief status summary for each item

**Pre-flight checks area:**
Below the menu, shows Docker detection and CA status.

| Check                | Pass Color      | Fail Color     |
|----------------------|-----------------|----------------|
| Docker Engine        | `ColorAllowed`  | `ColorDenied`  |
| CA Certificate       | `ColorAllowed`  | `ColorAmber`   |

If Docker is not found:
```
  cross Docker Engine not found. Please install Docker Engine 20.10+.
```
In `ColorDenied`. All menu items become unselectable (dimmed).

### Menu Item Styles

| State    | Name Color       | Status Color     | Background     |
|----------|------------------|------------------|----------------|
| Selected | `ColorCyan`      | `ColorTextBright`| `ColorSelection` |
| Normal   | `ColorTextBright`| `ColorTextDim`   | none           |
| Disabled | `ColorTextDim`   | `ColorTextDim`   | none           |

### Key Bindings

| Key        | Action                                  |
|------------|-----------------------------------------|
| `Up/k`     | Select previous menu item               |
| `Down/j`   | Select next menu item                   |
| `Enter`    | Enter selected configuration section    |
| `1`-`5`    | Jump to section by number               |
| `s`        | Save configuration (no build)           |
| `b`        | Save configuration and run build        |
| `q`        | Quit (prompts if unsaved changes)       |

### State Transitions

| From         | Trigger      | To                              |
|--------------|-------------|----------------------------------|
| Main menu    | `Enter` on 1 | Programming Tool Setup          |
| Main menu    | `Enter` on 2 | AI CLI Tool Setup               |
| Main menu    | `Enter` on 3 | Proxy Whitelist Setup           |
| Main menu    | `Enter` on 4 | Port Forwarding Setup           |
| Main menu    | `Enter` on 5 | Proxy Setup                     |
| Main menu    | `s`          | Save confirmation               |
| Main menu    | `b`          | Save & Build prompt             |
| Main menu    | `q`          | Quit (or unsaved changes modal) |
| Any section  | `Esc`        | Back to Main menu               |

---

## 17. cooper configure -- Programming Tool Setup

Two-level navigation: tool list, then per-tool configuration.

### ASCII Mockup -- Tool List

```
  whiskey_glass cooper configure > Programming Tools

  ──────────────────────────────────────────────────────────────────────────────────

    Select tools to install in the barrel container.
    Detected host tools are pre-selected.

     TOOL              ON/OFF    VERSION        MODE         HOST
  => Go                [ON ]     1.23.0         mirror       1.23.0
     Node.js           [ON ]     22.5.1         mirror       22.5.1
     Python            [ON ]     3.12.4         mirror       3.12.4
     Rust              [OFF]     --             --           not found

  ──────────────────────────────────────────────────────────────────────────────────

    Custom tools can be added to ~/.cooper/cli/Dockerfile.user
    Cooper never modifies that file. See docs for details.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [Enter Configure] [Space Toggle] [up/down Nav]   whiskey_glass
```

### Column Layout -- Tool List

| Column   | Width | Content                                           |
|----------|-------|---------------------------------------------------|
| Selector | 3     | `=>` or blank                                     |
| TOOL     | 18    | Tool name                                         |
| ON/OFF   | 8     | `[ON ]` in `ColorAllowed` or `[OFF]` in `ColorTextDim` |
| VERSION  | 14    | Configured version or `--`                        |
| MODE     | 12    | mirror/latest/pinned or `--`                      |
| HOST     | 12    | Detected host version or `not found`              |

### ASCII Mockup -- Per-Tool Configuration (Go selected)

```
  whiskey_glass cooper configure > Programming Tools > Go

  ──────────────────────────────────────────────────────────────────────────────────

    Go Configuration

     STATUS        [ ON ]               (Space to toggle)

     VERSION MODE
  =>   ( ) Mirror host version          1.23.0
       ( ) Latest available             1.23.2
       ( ) Pin specific version         [         ]

  ──────────────────────────────────────────────────────────────────────────────────

    Mirror: version updates when you run "cooper update", matching host.
    Latest: version updates to newest available on "cooper update".
    Pinned: version stays fixed. You choose when to change it.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [Enter Select] [Space Toggle ON/OFF] [up/down Nav]   whiskey_glass
```

### Radio Button Styles

```
  ( ) Unselected option    -- ColorTextDim
  (*) Selected option      -- ColorCyan, Bold
```

Selected radio button: `(*)` with the label in `ColorCyan`, Bold.
Unselected: `( )` with the label in `ColorTextBright`.

### Version Mode Details

**Mirror mode selected:**
```
  (*) Mirror host version          1.23.0
```
- Shows detected host version in `ColorAmber`
- If host version not found: shows `"(not detected)"` in `ColorDenied`

**Latest mode selected:**
```
  (*) Latest available             1.23.2
```
- Shows latest version fetched from API in `ColorAllowed`
- While fetching: shows `"checking..."` with animated dots in `ColorTextDim`

**Pin mode selected:**
```
  (*) Pin specific version         [ 1.22.0_ ]
```
- Text input field for version number
- Validation on `Enter`: if invalid, shows `"Invalid version"` in `ColorDenied` below the input

### Status Toggle

`Space` toggles the tool ON/OFF:
- `[ON ]` in `ColorAllowed` background -- tool will be included in Dockerfile
- `[OFF]` in `ColorBorder` background with `ColorTextDim` text -- tool excluded

When OFF, version mode options are dimmed and non-interactive.

### Key Bindings -- Tool List

| Key        | Action                                |
|------------|---------------------------------------|
| `Up/k`     | Select previous tool                  |
| `Down/j`   | Select next tool                      |
| `Enter`    | Enter per-tool configuration          |
| `Space`    | Toggle selected tool ON/OFF           |
| `Esc`      | Back to configure main menu           |

### Key Bindings -- Per-Tool Configuration

| Key        | Action                                |
|------------|---------------------------------------|
| `Up/k`     | Select previous option                |
| `Down/j`   | Select next option                    |
| `Enter`    | Confirm radio selection               |
| `Space`    | Toggle tool ON/OFF                    |
| `Esc`      | Back to tool list                     |

**In pin version text input:**
| Key        | Action                                |
|------------|---------------------------------------|
| typing     | Enter version string                  |
| `Enter`    | Validate and confirm version          |
| `Esc`      | Cancel input, restore previous value  |
| `Backspace`| Delete last character                 |

### State Transitions

| From              | Trigger              | To                        |
|-------------------|----------------------|---------------------------|
| Configure menu    | `Enter` on item 1    | Tool List                 |
| Tool List         | `Enter` on tool      | Per-Tool Configuration    |
| Tool List         | `Esc`                | Configure menu            |
| Per-Tool Config   | `Esc`                | Tool List                 |
| Per-Tool Config   | `Enter` on radio     | Radio selected, stay      |

---

## 18. cooper configure -- AI CLI Tool Setup

Identical structure to Programming Tool Setup but for AI CLI tools.

### ASCII Mockup -- Tool List

```
  whiskey_glass cooper configure > AI CLI Tools

  ──────────────────────────────────────────────────────────────────────────────────

    Select AI CLI tools to install in the barrel container.
    Detected host tools are pre-selected.

     TOOL              ON/OFF    VERSION        MODE         HOST
  => Claude Code       [ON ]     1.0.12         latest       1.0.12
     Copilot CLI       [OFF]     --             --           not found
     Codex CLI         [ON ]     0.9.3          latest       0.9.3
     OpenCode          [OFF]     --             --           not found

  ──────────────────────────────────────────────────────────────────────────────────

    Enabling a tool auto-whitelists its API domains in the proxy.
    Custom AI tools can be added to ~/.cooper/cli/Dockerfile.user.
    Request support for new tools at github.com/rickchristie/govner/issues.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [Enter Configure] [Space Toggle] [up/down Nav]   whiskey_glass
```

### Per-Tool Configuration

Same radio button layout as Programming Tools:
- Mirror host version
- Latest available
- Pin specific version

### Auto-Whitelist Note

When toggling a tool ON, show an inline note:

```
  checkmark Enabling Claude Code will auto-whitelist:
    .anthropic.com
    .claude.ai
```
In `ColorAllowed` for checkmark, `ColorTextMuted` for domains.

When toggling OFF:
```
  warning Disabling Codex CLI will remove auto-whitelist for:
    .openai.com
```
In `ColorAmber`.

### Key Bindings

Same as Programming Tool Setup (Section 17).

---

## 19. cooper configure -- Proxy Whitelist Setup

Domain whitelist management with two sub-tabs: Domain Whitelist and Port Forwarding (Port Forwarding
has its own section below, Section 20).

### ASCII Mockup -- Domain Whitelist

```
  whiskey_glass cooper configure > Proxy Whitelist

  ──────────────────────────────────────────────────────────────────────────────────

    Domain Whitelist   Port Forwarding
          *

  ──────────────────────────────────────────────────────────────────────────────────

    DEFAULT DOMAINS (auto-configured)

    checkmark  .anthropic.com              (Claude Code)        includes subdomains
    checkmark  .openai.com                 (Codex CLI)          includes subdomains
    checkmark  raw.githubusercontent.com   (safe, read-only)    exact match

    ─────────────────────────────────────────────────

    CUSTOM DOMAINS

     DOMAIN                          SUBDOMAINS
  => mycompany.com                   [YES]
     api.staging.mycompany.com       [NO ]
     grafana.mycompany.com           [NO ]

  ──────────────────────────────────────────────────────────────────────────────────

    shield Security guidance:
    - Add domains you trust completely (company domains, personal domains).
    - Package registries (npm, pypi, crates.io) are blocked by default.
      This prevents supply-chain attacks. Dependencies are mounted read-only
      from host caches. Install deps on host, not in the container.
    - Be as strict as possible. The proxy monitor lets you approve
      individual requests on-the-fly for one-off research needs.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [n New] [e Edit] [x Delete] [Tab Switch] [up/down Nav]   whiskey_glass
```

### Default Domains Section

Non-editable. Shows auto-configured domains based on enabled AI tools.
- Each has `checkmark` in `ColorAllowed`
- Tool source in `ColorTextDim` parentheses
- Subdomain note in `ColorTextDim`

### Custom Domains Table

| Column      | Width | Content                        |
|-------------|-------|--------------------------------|
| Selector    | 3     | `=>` or blank                  |
| DOMAIN      | 34    | Domain name                    |
| SUBDOMAINS  | 10    | `[YES]` or `[NO ]`            |

### Domain Entry Modal

```
  +==================================================+
  |                                                    |
  |            shield  Add Domain                      |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |  Domain:                                           |
  |  > mycompany.com_                                  |
  |                                                    |
  |  Include subdomains?                               |
  |    (*) Yes  (*.mycompany.com)                      |
  |    ( ) No   (exact match only)                     |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Save]    [Esc Cancel]         |
  |                                                    |
  +==================================================+
```

- Domain input: text field with cursor
- Subdomain toggle: radio buttons
- Preview of what the rule matches shown in `ColorTextDim`

### Empty State (custom domains)

```
    No custom domains added yet.
    Press [n] to add a trusted domain.
```

### Key Bindings

| Key        | Action                                |
|------------|---------------------------------------|
| `Up/k`     | Select previous domain                |
| `Down/j`   | Select next domain                    |
| `n`        | Add new domain (opens modal)          |
| `e`        | Edit selected domain (opens modal)    |
| `Enter`    | Same as `e`                           |
| `x`        | Delete selected domain (confirm)      |
| `Tab`      | Switch to Port Forwarding sub-tab     |
| `Esc`      | Back to configure main menu           |

---

## 20. cooper configure -- Port Forwarding Setup

Sub-tab of Proxy Whitelist Setup. Manages port forwarding rules (two-hop socat relay).

### ASCII Mockup

```
  whiskey_glass cooper configure > Proxy Whitelist

  ──────────────────────────────────────────────────────────────────────────────────

    Domain Whitelist   Port Forwarding
                             *

  ──────────────────────────────────────────────────────────────────────────────────

    PORT FORWARDING RULES

     CLI PORT       HOST PORT      DESCRIPTION
  => 5432           5432           PostgreSQL
     6379           6379           Redis
     8000-8100      8000-8100      Dev server range

  ──────────────────────────────────────────────────────────────────────────────────

    warning Host services must bind to 0.0.0.0 or the Docker gateway IP
    to be reachable. Services bound to 127.0.0.1 only will NOT work.

    How it works: socat in the CLI container forwards localhost:{port}
    to cooper-proxy:{port} on the internal network, then socat in the
    proxy forwards to host.docker.internal:{port} on the external network.

    Guidance:
    - Forward database ports (PostgreSQL, Redis, MySQL) if AI needs them.
    - Forward dev server ports if AI needs to curl/test your app.
    - Use port ranges for apps that use many dynamic ports.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [n New] [e Edit] [x Delete] [Tab Switch] [up/down Nav]   whiskey_glass
```

### Column Layout

| Column       | Width | Content                        |
|--------------|-------|--------------------------------|
| Selector     | 3     | `=>` or blank                  |
| CLI PORT     | 14    | Port or range in container     |
| HOST PORT    | 14    | Port or range on host          |
| DESCRIPTION  | 24    | User-provided label            |

### Port Rule Entry Modal

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Port Forwarding Rule      |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |  Container port:                                   |
  |  > 5432_                                           |
  |                                                    |
  |  Host port:                                        |
  |  > 5432_                                           |
  |                                                    |
  |  Description (optional):                           |
  |  > PostgreSQL_                                     |
  |                                                    |
  |  ( ) Single port                                   |
  |  ( ) Port range (e.g., 8000-8100)                  |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Save]    [Esc Cancel]         |
  |                                                    |
  +==================================================+
```

### Range Mode

When "Port range" is selected:
```
  Start port: > 8000_
  End port:   > 8100_
```
Both container and host use the same range (self-forwarding).

### Validation

- Port must be 1-65535
- Range must have start < end
- Range max span: 1000 ports
- No overlap with existing rules
- No collision with proxy port (3128) or bridge port (4343)
- Invalid input: inline error in `ColorDenied`

### Empty State

```
    No port forwarding rules configured.
    Press [n] to add a rule.

    Port forwarding lets the AI container
    access services running on your host machine.
```

### Key Bindings

Same pattern as Domain Whitelist (Section 19). `Tab` switches back to Domain Whitelist sub-tab.

---

## 21. cooper configure -- Proxy Setup

Proxy and bridge port configuration.

### ASCII Mockup

```
  whiskey_glass cooper configure > Proxy & Bridge Ports

  ──────────────────────────────────────────────────────────────────────────────────

    PROXY CONFIGURATION

     SETTING                          VALUE
  => Squid proxy port                 [ 3128 ]
     Execution bridge port            [ 4343 ]

  ──────────────────────────────────────────────────────────────────────────────────

    Proxy Port (default: 3128)
    The Squid proxy listens on this port inside the proxy container.
    All HTTP/HTTPS traffic from barrel containers routes through here.
    Only change this if port 3128 conflicts with another service.

    Execution Bridge Port (default: 4343)
    The bridge HTTP API runs on this port on the host machine.
    AI tools call localhost:{port}/{route} to execute bridge scripts.
    The bridge binds to 127.0.0.1 and the Docker gateway IP only
    (not 0.0.0.0), so it's not accessible from your LAN.

    These two ports must not be the same.

  ──────────────────────────────────────────────────────────────────────────────────
  [Esc Back] [Enter Edit] [up/down Nav]   whiskey_glass
```

### Setting Row Layout

```
  => Squid proxy port                 [ 3128 ]
```

Same as Configure tab (Section 12) -- `ColorAmber` brackets, `ColorTextBright` value.

### Edit Mode

Same as Configure tab. `Enter` to edit, type digits, `Enter` to confirm, `Esc` to cancel.

### Validation

- Port: 1024-65535
- Two ports must be different
- Port must not conflict with configured port forwarding rules
- Error: inline `ColorDenied` message

### Key Bindings

| Key        | Action                                |
|------------|---------------------------------------|
| `Up/k`     | Select previous setting               |
| `Down/j`   | Select next setting                   |
| `Enter`    | Edit selected setting                 |
| `Esc`      | Back to configure main menu           |

---

## 22. cooper configure -- Save & Build Prompt

Shown when user presses `s` (save only) or `b` (save and build) from the configure main menu.

### ASCII Mockup -- Save Only (`s`)

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Save Configuration?       |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |    Configuration will be written to:               |
  |      ~/.cooper/config.json                         |
  |                                                    |
  |    Dockerfiles will be generated at:               |
  |      ~/.cooper/proxy/Dockerfile                    |
  |      ~/.cooper/cli/Dockerfile                      |
  |      ~/.cooper/squid/squid.conf                    |
  |                                                    |
  |    You can build later with "cooper build".        |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Save]    [Esc Cancel]         |
  |                                                    |
  +==================================================+
```

### ASCII Mockup -- Save & Build (`b`)

```
  +==================================================+
  |                                                    |
  |            whiskey_glass  Save & Build?             |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |    Configuration will be saved and Docker          |
  |    images will be built immediately.               |
  |                                                    |
  |    This will build:                                |
  |      - cooper-proxy image                          |
  |      - cooper-barrel-base image                    |
  |      - cooper-barrel image (if Dockerfile.user     |
  |        exists)                                     |
  |                                                    |
  |    Build may take several minutes.                 |
  |                                                    |
  |  ----------------------------------------          |
  |                                                    |
  |     [Enter checkmark Build]    [Esc Cancel]        |
  |                                                    |
  +==================================================+
```

### Build Progress Screen

If user confirms build, transitions to a build progress screen:

```
                    . whiskey_glass .
                   .  whiskey_glass  . .


                      c o o p e r


                   building barrels...


              ━━━━━━━━━━━━━━━━━━━━━━━━────


                Building cooper-proxy...


              cooper-proxy       checkmark built
              barrel-base        building...
              barrel             waiting...


                  [q Cancel]  whiskey_glass
```

Progress steps:
| Step                | Progress | Message                     |
|---------------------|----------|-----------------------------|
| Save config         | 0.05     | "Saving configuration..."   |
| Generate files      | 0.15     | "Generating Dockerfiles..."  |
| Build proxy image   | 0.45     | "Building cooper-proxy..."   |
| Build barrel-base   | 0.75     | "Building cooper-barrel-base..." |
| Build barrel        | 0.90     | "Building cooper-barrel..."  |
| Done                | 1.00     | "Build complete!"            |

### Build Complete

```
                  sparkles whiskey_glass sparkles


                      c o o p e r


                  barrels ready to roll


              ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


              cooper-proxy       checkmark built
              barrel-base        checkmark built
              barrel             checkmark built


          Start the control panel with "cooper up",
          then open a barrel with "cooper cli".


                  [Enter Done]  whiskey_glass
```

- `cooper up` and `cooper cli` in `ColorCyan`
- Completion text in `ColorTextMuted`

### Key Bindings

**Modal:**
| Key        | Action                    |
|------------|---------------------------|
| `Enter/y`  | Confirm save/build        |
| `Esc/n`    | Cancel, back to menu      |

**Build progress:**
| Key        | Action                    |
|------------|---------------------------|
| `q`        | Cancel build (confirm)    |

**Build complete:**
| Key        | Action                    |
|------------|---------------------------|
| `Enter`    | Exit configure            |
| `q`        | Exit configure            |

### State Transitions

| From           | Trigger        | To                       |
|----------------|----------------|--------------------------|
| Config menu    | `s`            | Save confirmation modal  |
| Config menu    | `b`            | Save & Build modal       |
| Save modal     | `Enter`        | Files written, back to menu with success banner |
| Build modal    | `Enter`        | Build progress screen    |
| Build progress | Build completes| Build complete screen    |
| Build progress | `q`            | Cancel confirmation      |
| Build complete | `Enter`/`q`    | App exits                |

---

## Appendix A: Animation Reference

### Timer Countdown Pulse

5-frame cycle at 100ms per frame (500ms total cycle):

| Frame | Icon  | Color                     | Hex       |
|-------|-------|---------------------------|-----------|
| 0     | --    | AmberDim                  | `#92400e` |
| 1     | --    | Amber                     | `#fbbf24` |
| 2     | --    | AmberBright (peak)        | `#fde68a` |
| 3     | --    | Amber                     | `#fbbf24` |
| 4     | --    | AmberDim                  | `#92400e` |

### Approval Shimmer

5-frame color cycle, sheen moves left-to-right at 50ms per character position:

| Frame | Color                     | Hex       |
|-------|---------------------------|-----------|
| 0     | Emerald                   | `#34d399` |
| 1     | Allowed                   | `#4ade80` |
| 2     | Mint (peak)               | `#a7f3d0` |
| 3     | Allowed                   | `#4ade80` |
| 4     | Emerald                   | `#34d399` |

Matches pgflock's CopyShimmer implementation exactly.

### Denial Flash

3-frame flash at 80ms per frame (240ms visible), then 560ms hold = 800ms total:

| Frame | Background     | Text             |
|-------|----------------|------------------|
| 0     | Denied (#f87171) | TextBright (#e2e8f0) |
| 1     | Surface (#131920) | (normal)        |
| 2     | Denied (#f87171) | TextBright (#e2e8f0) |

### Loading Screen Whiskey Animation

4-frame dot cycle at 100ms per frame:

| Frame | Display                    |
|-------|----------------------------|
| 0     | `. whiskey_glass .`        |
| 1     | `. whiskey_glass . .`      |
| 2     | `. whiskey_glass . . .`    |
| 3     | `. whiskey_glass . .`      |

Matches pgflock sheep loading animation pattern.

### Health Check Whiskey Animation

Same pattern as pgflock sheep states:

| State       | Display                          | When                      |
|-------------|----------------------------------|---------------------------|
| Idle        | `whiskey_glass`                  | Default, all healthy      |
| Checking    | `dot whiskey_glass dot dot`      | During health check       |
| Warning     | `lightning whiskey_glass`        | Partial failure           |
| Error       | `whiskey_glass sweat` (shaking)  | Critical failure          |

---

## Appendix B: Terminal Size Handling

- **Minimum usable size**: 80 columns x 24 rows
- **Responsive behavior**: content area fills available height minus fixed header/footer
- **Narrow terminals (<80 cols)**: tab names abbreviate (Cont., Proxy, Brdg, Conf, Abt)
- **Two-pane layouts**: below 100 columns, panes stack vertically instead of side-by-side
- **All lines truncated** to terminal width to prevent wrapping (matching pgflock's `truncateLine`)
- **WindowSizeMsg** handled in root Update to propagate new dimensions to all sub-models

---

## Appendix C: State Architecture

Root model for `cooper up` holds:
```go
type Model struct {
    // Active tab
    activeTab     Tab     // Containers, Proxy, Bridge, Configure, About
    activeSubTab  SubTab  // For Proxy: Monitor, Blocked, Allowed. For Bridge: Logs, Routes.

    // Sub-models (one per tab)
    containers  *containers.Model
    proxyMon    *proxymon.Model
    proxyBlock  *history.Model
    proxyAllow  *history.Model
    bridgeLogs  *bridgeui.Model
    bridgeRoutes *bridgeui.Model
    settings    *settings.Model
    about       *about.Model

    // Shared state
    config       *config.Config
    proxyStatus  ProxyStatus
    bridgeStatus BridgeStatus
    barrels      []BarrelInfo

    // Loading/shutdown screens
    loadingScreen *loading.Model
    showingLoading bool

    // Modal
    confirm  ConfirmAction

    // Terminal dimensions
    width  int
    height int

    // Channels (state sync from background goroutines)
    proxyEventChan  <-chan proxy.Event     // New requests, approvals, denials
    bridgeEventChan <-chan bridge.Event    // Bridge executions
    containerStatsChan <-chan docker.Stats // CPU/mem updates
    healthCheckChan <-chan HealthResult    // Periodic health

    // Callbacks
    onQuit     func()
    onShutdown func() <-chan loading.Progress
}
```

Message routing in `Update()`:
- Global keys (`Tab`, `q`, number keys) handled by root model
- All other messages forwarded to active sub-model's `Update()`
- Channel messages always processed regardless of active tab (data accumulates in background)

---

## Appendix D: File Mapping to Implementation

This design maps to the module structure specified in REQUIREMENTS.md:

| Design Section | Implementation File(s)                      |
|----------------|---------------------------------------------|
| Color palette  | `internal/tui/constants.go`, `internal/tui/styles.go` |
| Shared components | `internal/tui/components/tabs.go`, `components/modal.go`, `components/timer.go`, `components/table.go` |
| Loading screen | `internal/tui/loading/model.go`, `loading/view.go` |
| Tab bar        | `internal/tui/view.go` (root view)          |
| Containers tab | `internal/tui/containers/model.go`, `containers/view.go` |
| Proxy Monitor  | `internal/tui/proxymon/model.go`, `proxymon/view.go` |
| Proxy Blocked  | `internal/tui/history/model.go`, `history/view.go` (shared) |
| Proxy Allowed  | `internal/tui/history/model.go`, `history/view.go` (shared) |
| Bridge Logs    | `internal/tui/bridgeui/model.go`, `bridgeui/view.go` |
| Bridge Routes  | `internal/tui/bridgeui/model.go`, `bridgeui/view.go` |
| Configure tab  | `internal/tui/settings/model.go`, `settings/view.go` |
| About tab      | `internal/tui/about/model.go`, `about/view.go` |
| Exit modal     | `internal/tui/components/modal.go`          |
| Shutdown screen | `internal/tui/loading/model.go`, `loading/view.go` |
| Configure wizard | `internal/configure/*.go`                 |
