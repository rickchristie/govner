# Cooper TUI Design -- Design B: "The Rickhouse"

> A rickhouse is the warehouse where bourbon barrels are stored during aging.
> The heat of the upper floors drives whiskey into the charred oak; the cool of the lower
> floors draws it back out. This cycle -- years of patient expansion and contraction --
> is what transforms raw spirit into something worth drinking.
>
> This design treats the Cooper TUI as a rickhouse control room: warm, oak-toned,
> purposeful. Every screen tells you the state of your barrels at a glance.

---

## Color Palette: "The Distillery"

A warm, dark palette that evokes a dimly lit barrel warehouse -- charred oak, amber
whiskey, copper stills, and the faint green of oxidized brass fittings.

### Base Colors

| Name             | Hex       | Usage                                      |
|------------------|-----------|---------------------------------------------|
| `CharredOak`     | `#1A1410` | Primary background -- deep charred wood     |
| `DarkStave`      | `#241E18` | Elevated surfaces, modal backgrounds        |
| `WornOak`        | `#2E2620` | Selection highlight background              |
| `StaveGrain`     | `#3D332A` | Borders, dividers, inactive tab bg          |
| `CooperageFloor` | `#4A3D32` | Subtle separators, scrollbar track          |

### Text Colors

| Name            | Hex       | Usage                                       |
|-----------------|-----------|----------------------------------------------|
| `ParchmentBright` | `#F0E6D6` | Primary text -- aged parchment              |
| `ParchmentDim`    | `#A89880` | Secondary text, help descriptions           |
| `ParchmentFaint`  | `#6B5D4F` | Faint text, dimmed modal backdrop           |
| `ParchmentMuted`  | `#C4B8A8` | Modal body text                             |

### Accent Colors

| Name            | Hex       | Usage                                       |
|-----------------|-----------|----------------------------------------------|
| `Amber`         | `#D4A04F` | Primary accent -- the color of aged whiskey |
| `DeepAmber`     | `#B8862D` | Active tab underline, pressed states        |
| `Copper`        | `#C87533` | Secondary accent -- still copper tones      |
| `DarkCopper`    | `#A0592A` | Hover states, secondary indicators          |
| `BrassGreen`    | `#7A9B6D` | Healthy/approved -- oxidized brass          |
| `BrassGreenBright` | `#9ACD8B` | Active healthy status, checkmarks        |
| `BarrelChar`    | `#CD5C5C` | Error, blocked, denied -- char heat         |
| `BarrelCharBright` | `#E07070` | Active error states                      |
| `OakSmoke`      | `#8B7355` | Warning, timeout -- aged smoke              |
| `WheatGold`     | `#E8D44D` | Countdown urgency, waiting states           |
| `SpringWater`   | `#7EC8C8` | Info, tab labels, key hints                 |

### Animation Colors (Pending Request Pulse)

| Name           | Hex       | Frame position                               |
|----------------|-----------|-----------------------------------------------|
| `EmberDim`     | `#B8862D` | Frame 0, 4 (base)                            |
| `EmberWarm`    | `#D4A04F` | Frame 1, 3                                    |
| `EmberBright`  | `#F0C060` | Frame 2 (peak)                               |

### Approval Shimmer Colors

| Name           | Hex       | Frame position                               |
|----------------|-----------|-----------------------------------------------|
| `BrassBase`    | `#7A9B6D` | Frame 0, 4                                   |
| `BrassSheen`   | `#9ACD8B` | Frame 1, 3                                    |
| `BrassPeak`    | `#C0E8B0` | Frame 2 (peak brightness)                    |

---

## Typography & Symbols

### Brand Characters

| Symbol | Usage                        |
|--------|-------------------------------|
| `🥃`   | Cooper brand icon (whiskey glass) |
| `📦`    | Barrel/container icon         |
| `🔥`   | Active/running processes      |
| `💨`   | Shutdown/smoke clearing       |

### Status Icons

| Symbol | Meaning                       |
|--------|--------------------------------|
| `◉`    | Active/running                 |
| `○`    | Idle/free                      |
| `◈`    | Pending (animates)             |
| `✓`    | Approved/healthy/success       |
| `✗`    | Blocked/denied/error           |
| `⚠`    | Warning/timeout                |
| `▶`    | Selection arrow                |
| `◆`    | Animation peak frame           |
| `━`    | Heavy horizontal rule          |
| `─`    | Light horizontal rule          |
| `│`    | Vertical separator             |
| `╭╮╰╯` | Rounded box corners           |
| `┊`    | Dotted vertical (timer column) |

### Tab Icons

| Tab               | Icon | Display               |
|-------------------|------|-----------------------|
| Containers        | `📦`  | `📦 Containers`       |
| Proxy Monitor     | `🔥`  | `🔥 Tasting Room`    |
| Proxy Blocked     | `✗`  | `✗ Rejected`          |
| Proxy Allowed     | `✓`  | `✓ Approved`          |
| Bridge Logs       | `⇄`  | `⇄ Bridge Logs`      |
| Bridge Routes     | `⚙`  | `⚙ Bridge Routes`    |
| Configure         | `☰`  | `☰ Settings`          |
| About             | `◉`  | `◉ About`            |

---

## Animation Timing Constants

| Constant                    | Value   | Purpose                                    |
|-----------------------------|---------|---------------------------------------------|
| `PendingPulseInterval`      | 100ms   | Ember pulse on pending requests             |
| `CountdownTickInterval`     | 1000ms  | Countdown timer updates (1s granularity)    |
| `ApprovalShimmerInterval`   | 50ms    | Shimmer on successful approval              |
| `ApprovalShimmerDuration`   | 2500ms  | Total shimmer display time                  |
| `LoadingFrameInterval`      | 100ms   | Loading screen barrel animation             |
| `LoadingProgressTickInterval` | 50ms  | Staggered progress bar animation            |
| `UIRefreshInterval`         | 1000ms  | General UI refresh (elapsed times, stats)   |
| `HealthCheckInterval`       | 5000ms  | Container/proxy health check cycle          |
| `HealthCheckMinDisplayTime` | 2000ms  | Minimum time to show health-check status    |
| `HealthStatusHoldTime`      | 1500ms  | How long to show "All healthy" before clear |

---

## Global Key Bindings

These keys work from any screen in the `cooper up` TUI (unless a modal is open):

| Key         | Action                                  |
|-------------|-----------------------------------------|
| `1`-`8`     | Jump to tab by number                   |
| `Tab`       | Next tab                                |
| `Shift+Tab` | Previous tab                            |
| `q`         | Open exit confirmation modal            |
| `Ctrl+C`    | Open exit confirmation modal            |
| `?`         | Toggle help overlay (full keybind list) |

---

## Screen 1: Loading Screen (`cooper up` startup)

**Theme:** "Filling the Barrel" -- the startup process is presented as filling a
barrel with raw spirit, then lighting the char to seal it.

### State Transitions
- **Entry:** `cooper up` command executed.
- **Exit (success):** All containers healthy, transition to Main TUI (Containers tab).
- **Exit (failure):** Error shown with option to quit.
- **Exit (cancel):** User presses `q` during startup.

### ASCII Mockup (startup in progress)

```
                          . 🥃 . .

                      c o o p e r

                  filling the barrel...

                  ████████░░░░░░░░░░░░   40%

              Starting proxy container...

              :3128  ✓ proxy ready
              :4343  waiting...

              [q Cancel]  🥃
```

### ASCII Mockup (startup complete, holding at 100%)

```
                        🔥 🥃 🔥

                      c o o p e r

                    barrel is charred

                  ████████████████████  100%

                      Ready to pour

              :3128  ✓ proxy ready
              :4343  ✓ bridge ready

              [q Cancel]  🥃
```

### ASCII Mockup (startup failed)

```
                        🥃 !

                      c o o p e r

                     startup failed

                  ████████░░░░░░░░░░░░

          Error: proxy container failed to start

              [q Quit]  🥃
```

### Color Specification

| Element                | Color                                   |
|------------------------|-----------------------------------------|
| Background             | `CharredOak` (#1A1410)                  |
| `c o o p e r` title   | `Amber` (#D4A04F) bold                  |
| Subtitle text          | `ParchmentDim` (#A89880)                |
| Progress bar filled    | `Amber` (#D4A04F)                       |
| Progress bar empty     | `StaveGrain` (#3D332A)                  |
| Progress percentage    | `ParchmentDim` (#A89880)                |
| Status message         | `ParchmentDim` (#A89880)                |
| Instance ready icon    | `BrassGreenBright` (#9ACD8B)            |
| Instance ready text    | `BrassGreen` (#7A9B6D)                  |
| Instance waiting       | `ParchmentDim` (#A89880)                |
| Error text             | `BarrelCharBright` (#E07070) bold       |
| Help key `q`           | `SpringWater` (#7EC8C8)                 |
| Help desc              | `ParchmentDim` (#A89880)                |

### Animation Details

**Whiskey glass dots animation** (loading in progress):
- Interval: `LoadingFrameInterval` (100ms)
- Frames cycle: `". 🥃 ."` -> `". 🥃 . ."` -> `". 🥃 . . ."` -> `". 🥃 . ."`
- On completion: switches to `"🔥 🥃 🔥"` (charred)
- On failure: switches to `"🥃 !"`
- On shutdown mode: final frame is `"💨 🥃 💨"` (smoke clearing)

**Progress bar staggered animation:**
- Interval: `LoadingProgressTickInterval` (50ms)
- Display progress animates toward target in 20% increments.
- Holds at 100% for 20 ticks (1s) before marking done.
- Character: `█` for filled, `░` for empty.
- Width: 20 characters.

### Key Bindings

| Key      | Action                      |
|----------|-----------------------------|
| `q`      | Cancel startup and quit     |
| `Ctrl+C` | Cancel startup and quit     |

(No other keys during loading. Shutdown mode has no keys at all.)

### Subtitles by Mode

| Mode     | In-progress subtitle      | Complete subtitle       | Failed subtitle       |
|----------|---------------------------|-------------------------|-----------------------|
| Startup  | `filling the barrel...`   | `barrel is charred`     | `startup failed`      |
| Restart  | `re-charring the barrel...` | `barrel is charred`   | `restart failed`      |
| Shutdown | `sealing the barrel...`   | `barrel sealed`         | `shutdown failed`     |

### Status Messages by Step

| Step                 | Message                              |
|----------------------|--------------------------------------|
| StepInit             | `Preparing the cooperage...`         |
| StepStartingProxy    | `Starting proxy container...`        |
| StepWaitingProxy     | `Waiting for Squid to respond...`    |
| StepStartingBridge   | `Lighting the bridge lantern...`     |
| StepReady            | `Ready to pour`                      |
| StepStoppingContainers | `Draining the containers...`       |

### Empty/Idle States
Not applicable -- the loading screen always has content.

### Error States
- Error message displayed below the progress bar in `BarrelCharBright`.
- `[q Quit]` replaces `[q Cancel]`.
- Whiskey glass shows `"🥃 !"`.
- Progress bar freezes at last position.

---

## Screen 2: Main Tab Bar

The tab bar sits at the top of every `cooper up` screen. It is always visible.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
─────────────────────────────────────────────────────────────────────────────────
```

### Layout

**Line 1: Header bar** (fixed, always visible)
- Left: `🥃 cooper` brand (Amber, bold) + `[proxy:3128]` (ParchmentDim) + `[bridge:4343]` (ParchmentDim)
- Right: `📦 N containers` (BrassGreen if running, ParchmentDim if zero) + `◉ proxy up` / `✗ proxy down` (BrassGreenBright / BarrelCharBright)

**Line 2: Heavy separator** (`━` in StaveGrain)

**Line 3: Tab bar**
- Each tab: icon + label.
- Active tab: `Amber` text, bold, with `DeepAmber` underline on line 4.
- Inactive tab: `ParchmentDim` text.
- Tabs separated by 3 spaces.

**Line 4: Light separator / active tab underline**
- `─` in `StaveGrain` across full width.
- Under the active tab, the `─` characters are replaced with `━` in `DeepAmber`.

### Color Specification

| Element               | Color                                 |
|-----------------------|---------------------------------------|
| `🥃 cooper`          | `Amber` (#D4A04F) bold               |
| Port labels           | `ParchmentDim` (#A89880)             |
| Container count (>0)  | `BrassGreen` (#7A9B6D)               |
| Container count (0)   | `ParchmentDim` (#A89880)             |
| Proxy status (up)     | `BrassGreenBright` (#9ACD8B)         |
| Proxy status (down)   | `BarrelCharBright` (#E07070)         |
| Active tab text       | `Amber` (#D4A04F) bold               |
| Active tab underline  | `DeepAmber` (#B8862D)                |
| Inactive tab text     | `ParchmentDim` (#A89880)             |
| Heavy separator       | `StaveGrain` (#3D332A)               |
| Light separator       | `StaveGrain` (#3D332A)               |
| Background            | `CharredOak` (#1A1410)               |

### Key Bindings

| Key         | Action                                |
|-------------|---------------------------------------|
| `1`         | Jump to Containers tab                |
| `2`         | Jump to Tasting Room tab              |
| `3`         | Jump to Rejected tab                  |
| `4`         | Jump to Approved tab                  |
| `5`         | Jump to Bridge Logs tab               |
| `6`         | Jump to Bridge Routes tab             |
| `7`         | Jump to Settings tab                  |
| `8`         | Jump to About tab                     |
| `Tab`       | Next tab (wraps around)               |
| `Shift+Tab` | Previous tab (wraps around)           |

### State Transitions
- Tab bar is always present. Switching tabs replaces the content area below the separator.
- Header counters update in real time as containers start/stop.

---

## Screen 3: Containers Tab

**Theme:** The rickhouse holds the containers. The proxy is the "still."

### ASCII Mockup (normal state with containers)

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  The Still                                                             STATUS
─────────────────────────────────────────────────────────────────────────────────
▶ cooper-proxy        🔥 running    2m 34s    CPU 0.3%    MEM 48MB    ◉ healthy
─────────────────────────────────────────────────────────────────────────────────

  The Rickhouse                                                    2 containers
─────────────────────────────────────────────────────────────────────────────────
  barrel-myproject    🔥 running    14m 22s   CPU 1.2%    MEM 210MB   ◉ healthy
  barrel-webapp       🔥 running    8m 05s    CPU 0.8%    MEM 156MB   ◉ healthy

─────────────────────────────────────────────────────────────────────────────────
[s Stop] [r Restart] [↑↓ Nav]                              Checking... · 🥃 ·
```

### ASCII Mockup (empty state -- no CLI containers)

```
  The Still                                                             STATUS
─────────────────────────────────────────────────────────────────────────────────
▶ cooper-proxy        🔥 running    2m 34s    CPU 0.3%    MEM 48MB    ◉ healthy
─────────────────────────────────────────────────────────────────────────────────

  The Rickhouse                                                    0 containers
─────────────────────────────────────────────────────────────────────────────────

                           🥃 💨

             The rickhouse is empty -- no containers running

                  Run cooper cli to start a container

─────────────────────────────────────────────────────────────────────────────────
[↑↓ Nav]                                                  All healthy ✓  🥃
```

### Layout

**Section 1: "The Still"** (proxy container, always exactly 1 row)
- Section header: `The Still` (Amber, bold) + right-aligned `STATUS` (ParchmentDim)
- Light separator (`─` in StaveGrain)
- Row: selection arrow, container name, status icon+text, elapsed time, CPU%, MEM, health

**Section 2: "The Rickhouse"** (CLI containers, 0 or more rows)
- Section header: `The Rickhouse` (Amber, bold) + right-aligned `N containers` (ParchmentDim)
- Light separator
- Container rows (same format as proxy row)
- If empty: centered empty state message

**Footer:**
- Left: context-sensitive key hints
- Right: health status + animated whiskey glass

### Color Specification

| Element                     | Color                                |
|-----------------------------|--------------------------------------|
| Section headers             | `Amber` (#D4A04F) bold              |
| Container name (selected)   | `CharredOak` bg, `Amber` fg, bold   |
| Container name (normal)     | `ParchmentBright` (#F0E6D6)         |
| `🔥 running`               | `BrassGreen` (#7A9B6D)               |
| `○ stopped`                 | `ParchmentDim` (#A89880)             |
| Elapsed time                | `ParchmentDim` (#A89880)             |
| CPU / MEM values            | `ParchmentBright` (#F0E6D6)          |
| CPU / MEM (high >80%)       | `BarrelChar` (#CD5C5C)               |
| `◉ healthy`                 | `BrassGreenBright` (#9ACD8B)         |
| `✗ unhealthy`               | `BarrelCharBright` (#E07070)         |
| Selection row background    | `WornOak` (#2E2620)                  |
| Selection arrow `▶`         | `Amber` (#D4A04F) bold               |
| Empty state emoji            | `Amber` (#D4A04F)                   |
| Empty state text            | `ParchmentDim` (#A89880) italic      |
| Section separator           | `StaveGrain` (#3D332A)               |

### Key Bindings

| Key      | Action                                            |
|----------|---------------------------------------------------|
| `↑`/`k`  | Move selection up                                |
| `↓`/`j`  | Move selection down                              |
| `s`      | Stop selected container                           |
| `r`      | Restart selected container                        |
| `Enter`  | Start selected container (if stopped)             |
| `l`      | View logs for selected container (opens log tail) |

### Health Status Footer Animation

The footer right side shows a health check cycle:

1. **Idle:** `All healthy ✓  🥃` (BrassGreenBright for text, then whiskey glass)
2. **Checking:** `Checking... · 🥃 ·` (ParchmentDim for text, dots animate around glass)
3. **Warning:** `Timeout: cooper-proxy  ⚡🥃` (OakSmoke for text, lightning + glass)
4. **Error:** `Proxy unreachable  🥃💦` (BarrelCharBright for text, sweating glass)

Glass animation frames (checking state):
- `· 🥃 ·` -> `· 🥃 · ·` -> `· 🥃 · · ·` -> `· 🥃 · ·`
- Interval: 100ms

Glass animation frames (error/distressed state):
- `🥃💦` -> ` 🥃💦` -> `🥃 💦` -> ` 🥃`
- Interval: 100ms

### State Transitions
- **Entry:** Default tab when TUI starts after loading screen.
- **Exit:** Switch to another tab via Tab/number keys.
- Container list updates in real time as CLI containers are created/stopped.

### Error States
- If Docker API fails to return stats: show `--` for CPU/MEM, `⚠ unknown` for health.
- If proxy container is down: row shows `✗ stopped` in BarrelCharBright. Stop key is disabled, Restart key becomes the primary action.

---

## Screen 4: Proxy Monitor Tab ("The Tasting Room")

**Theme:** The tasting room is where the master distiller samples the output and decides
what meets the standard. Each pending request is a "sample" to be tasted (inspected) and
approved or rejected.

This is the most important screen -- the two-pane approval UI.

### ASCII Mockup (requests pending)

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Samples Pending  3                        │  Sample Detail
──────────────────────────────────────────  │  ──────────────────────────────────
  ┊ 2s ┊ ▶ api.stripe.com                  │  Domain    api.stripe.com
  ┊ 4s ┊   cdn.example.com                 │  URL       https://api.stripe.com/v
  ┊ 5s ┊   tracking.mixpanel.com           │            1/charges
                                            │  Method    POST
                                            │  Source    barrel-myproject
                                            │  Time     14:32:05
                                            │
                                            │  Request Headers
                                            │  ──────────────────────────────────
                                            │  Authorization  Bearer sk-...4f2a
                                            │  Content-Type   application/json
                                            │  User-Agent     node-fetch/2.6.1
                                            │
                                            │
                                            │
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
[a Approve] [d Deny] [↑↓ Nav]              │  ◈ 3 samples pending     🥃
```

### ASCII Mockup (empty state -- nothing pending)

```
  Samples Pending  0                        │  Sample Detail
──────────────────────────────────────────  │  ──────────────────────────────────
                                            │
                                            │
                                            │
              🥃 ✓                           │
                                            │
       All clear -- nothing to taste        │      No sample selected
                                            │
    Whitelisted traffic flows undisturbed   │
                                            │
                                            │
                                            │
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
[↑↓ Nav]                                    │  All quiet in the tasting room  🥃
```

### Layout

**Two-pane split:**
- Left pane: ~50% width. Pending request list with countdown timers.
- Right pane: ~50% width. Detail view of selected request.
- Panes separated by `│` vertical line in `StaveGrain`.

**Left pane header:** `Samples Pending` (Amber, bold) + count (ParchmentBright)
**Right pane header:** `Sample Detail` (Amber, bold)

**Left pane rows:**
```
  ┊ Ns ┊ [▶] domain.com
```
- `┊` timer column border in StaveGrain
- `Ns` countdown in seconds. Color shifts as time runs out:
  - 4-5s: `BrassGreen` (plenty of time)
  - 2-3s: `WheatGold` (getting urgent)
  - 1s: `BarrelCharBright` (about to expire)
- `▶` selection arrow in Amber (only on selected row)
- Domain name in ParchmentBright (selected) or ParchmentDim (unselected)
- Selected row gets WornOak background

**Right pane detail fields:**
- Label in ParchmentDim, value in ParchmentBright
- URL may wrap to next line (indented to align with value column)
- Headers shown as label-value pairs

**Sorted by time remaining** -- most urgent (lowest countdown) at top.

### Color Specification

| Element                         | Color                               |
|---------------------------------|-------------------------------------|
| Section headers                 | `Amber` (#D4A04F) bold             |
| Pending count                   | `ParchmentBright` (#F0E6D6)        |
| Timer (4-5s remaining)          | `BrassGreen` (#7A9B6D)             |
| Timer (2-3s remaining)          | `WheatGold` (#E8D44D) bold         |
| Timer (1s remaining)            | `BarrelCharBright` (#E07070) bold  |
| Timer column border `┊`         | `StaveGrain` (#3D332A)             |
| Domain (selected)               | `ParchmentBright` (#F0E6D6) bold   |
| Domain (unselected)             | `ParchmentDim` (#A89880)           |
| Selected row background         | `WornOak` (#2E2620)                |
| Selection arrow                 | `Amber` (#D4A04F)                  |
| Detail label                    | `ParchmentDim` (#A89880)           |
| Detail value                    | `ParchmentBright` (#F0E6D6)        |
| Detail URL                      | `SpringWater` (#7EC8C8)            |
| Detail method POST/PUT/DELETE   | `BarrelChar` (#CD5C5C)             |
| Detail method GET               | `BrassGreen` (#7A9B6D)             |
| Pane separator `│`              | `StaveGrain` (#3D332A)             |
| Footer pending count pulse      | Animates through EmberDim/EmberWarm/EmberBright |
| Empty state icon                | `BrassGreenBright` (#9ACD8B)       |
| Empty state text                | `ParchmentDim` (#A89880) italic    |

### Animation Details

**Pending count pulse** (footer, when requests are pending):
- Icon `◈` cycles through colors: `EmberDim` -> `EmberWarm` -> `EmberBright` -> `EmberWarm` -> `EmberDim`
- Interval: `PendingPulseInterval` (100ms), 5 frames per cycle = 500ms full cycle
- When no requests pending: solid `○` in ParchmentDim, no animation

**Countdown timer:**
- Updates every `CountdownTickInterval` (1000ms)
- When a request expires (timer hits 0), the row briefly flashes `BarrelCharBright` bg for 200ms, then is removed from the list.
- The flash uses a 2-frame sequence: `BarrelChar` bg -> removed

**Approval shimmer** (when user approves a request):
- The approved row text gets a shimmer effect moving left to right
- Colors cycle: `BrassBase` -> `BrassSheen` -> `BrassPeak` -> `BrassSheen` -> `BrassBase`
- Duration: `ApprovalShimmerDuration` (2500ms)
- Row is then removed from pending list

### Key Bindings

| Key      | Action                                      |
|----------|---------------------------------------------|
| `↑`/`k`  | Move selection up in pending list           |
| `↓`/`j`  | Move selection down in pending list         |
| `a`      | Approve selected request (allow through)     |
| `Enter`  | Approve selected request (allow through)     |
| `d`      | Deny selected request (block immediately)    |
| `x`      | Deny selected request (block immediately)    |

### State Transitions
- **Entry:** User switches to Tasting Room tab.
- Pending list updates in real-time as new requests arrive from the proxy ACL helper.
- Approved requests move to the Approved history.
- Denied/expired requests move to the Rejected history.

### Error States
- If the ACL socket is disconnected: banner at top of left pane:
  `⚠ Proxy connection lost -- all requests auto-denied`
  Banner text in BarrelCharBright on CharredOak bg.
- If the request detail data is malformed: right pane shows
  `⚠ Could not parse request details` in BarrelChar.

---

## Screen 5: Proxy Blocked Tab ("Rejected Batches")

**Theme:** These are the batches that didn't make the cut -- rejected during tasting.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Rejected Batches  12                      │  Batch Detail
──────────────────────────────────────────  │  ──────────────────────────────────
  14:33:01  ▶ npmjs.org            timeout  │  Domain    npmjs.org
  14:32:58    pypi.org             timeout  │  URL       https://npmjs.org/
  14:32:44    tracking.ad.com      denied   │            package/lodash
  14:32:30    cdn.evil.com         denied   │  Method    GET
  14:32:15    registry.npmjs.org   timeout  │  Source    barrel-myproject
  14:31:50    api.segment.io       timeout  │  Time      14:33:01
  14:31:22    cdn.example.com      denied   │  Reason    timeout (5s)
  14:30:55    tracking.hubspot.com timeout  │
                                            │  Request Headers
                                            │  ──────────────────────────────────
                                            │  User-Agent     npm/9.6.7
                                            │  Accept         application/json
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
[↑↓ Nav]                     12/500 shown   │  12 rejected total     🥃
```

### ASCII Mockup (empty state)

```
  Rejected Batches  0                       │  Batch Detail
──────────────────────────────────────────  │  ──────────────────────────────────
                                            │
                                            │
                                            │
              🥃 ✓                           │
                                            │
     No rejected batches -- clean run       │      No batch selected
                                            │
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
                                            │  Nothing to report     🥃
```

### Layout

Same two-pane layout as the Tasting Room, but for historical (not live pending) data.

**Left pane rows:**
```
  HH:MM:SS  [▶] domain.com         reason
```
- Timestamp in ParchmentDim
- Domain in ParchmentBright (selected) or ParchmentDim (unselected)
- Reason: `timeout` in OakSmoke, `denied` in BarrelChar

**Right pane:** Same field layout as Tasting Room detail, plus `Reason` field.

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header               | `Amber` (#D4A04F) bold              |
| Count                        | `BarrelChar` (#CD5C5C)               |
| Timestamp                    | `ParchmentDim` (#A89880)             |
| Domain (selected)            | `ParchmentBright` (#F0E6D6)          |
| Domain (unselected)          | `ParchmentDim` (#A89880)             |
| Reason `timeout`             | `OakSmoke` (#8B7355)                 |
| Reason `denied`              | `BarrelChar` (#CD5C5C)               |
| Detail Reason value          | `BarrelCharBright` (#E07070)         |
| Line count indicator         | `ParchmentDim` (#A89880)             |
| Selected row bg              | `WornOak` (#2E2620)                  |

### Key Bindings

| Key      | Action                           |
|----------|----------------------------------|
| `↑`/`k`  | Move selection up               |
| `↓`/`j`  | Move selection down             |
| `g`      | Jump to top of list              |
| `G`      | Jump to bottom of list           |
| `PgUp`   | Scroll up one page               |
| `PgDn`   | Scroll down one page             |

### State Transitions
- List grows as new requests are blocked.
- Capped at N entries (configurable in Settings tab, default 500).
- Newest entries appear at the top.

### Error States
- None specific to this view. If data is malformed, the detail pane shows a parse error.

---

## Screen 6: Proxy Allowed Tab ("Approved Casks")

**Theme:** These casks passed inspection and were approved for aging.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Approved Casks  47                        │  Cask Detail
──────────────────────────────────────────  │  ──────────────────────────────────
  14:33:05  ▶ api.anthropic.com   200  wl   │  Domain    api.anthropic.com
  14:33:02    api.openai.com      200  wl   │  URL       https://api.anthropic.co
  14:32:50    api.stripe.com      201  ap   │            m/v1/messages
  14:32:45    api.anthropic.com   200  wl   │  Method    POST
  14:32:30    raw.github.com      200  wl   │  Source    barrel-myproject
  14:32:15    api.anthropic.com   200  wl   │  Time      14:33:05
  14:32:00    api.anthropic.com   200  wl   │  Via       whitelist
                                            │  Status    200 OK
                                            │
                                            │  Response Headers
                                            │  ──────────────────────────────────
                                            │  Content-Type   application/json
                                            │  X-Request-Id   req_abc123
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
[↑↓ Nav]                     47/500 shown   │  47 approved total     🥃
```

### Layout

Same two-pane layout. Left pane rows:
```
  HH:MM:SS  [▶] domain.com      status  via
```
- `via` column: `wl` = whitelist (ParchmentDim), `ap` = manual approval (Amber)
- Status code: `2xx` in BrassGreen, `3xx` in SpringWater, `4xx`/`5xx` in BarrelChar

**Right pane:** Request details plus response data (status code, response headers).

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header               | `Amber` (#D4A04F) bold              |
| Count                        | `BrassGreen` (#7A9B6D)               |
| Status 2xx                   | `BrassGreen` (#7A9B6D)               |
| Status 3xx                   | `SpringWater` (#7EC8C8)              |
| Status 4xx/5xx               | `BarrelChar` (#CD5C5C)               |
| Via `wl` (whitelist)         | `ParchmentDim` (#A89880)             |
| Via `ap` (approved)          | `Amber` (#D4A04F)                    |
| Detail `Via` value whitelist | `ParchmentDim` (#A89880)             |
| Detail `Via` value approved  | `Amber` (#D4A04F) bold               |
| Response header labels       | `ParchmentDim` (#A89880)             |
| Response header values       | `ParchmentBright` (#F0E6D6)          |

### Key Bindings

Same as Rejected tab: `↑`/`↓`/`k`/`j`/`g`/`G`/`PgUp`/`PgDn`.

### Empty State

```
              🥃 ✓

     No traffic yet -- the still is warming up

         Requests will appear once containers
              start making calls
```

### State Transitions
- List grows as requests are completed (both whitelisted and manually approved).
- Capped at N entries (configurable, default 500).
- Newest at top.

---

## Screen 7: Execution Bridge Logs Tab

**Theme:** The bridge between the rickhouse and the cooperage -- where containers
send requests to the master cooper on the host side.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Bridge Logs                               │  Log Detail
  localhost:4343                             │
──────────────────────────────────────────  │  ──────────────────────────────────
  14:33:10  ▶ POST /deploy-staging    200   │  Path       /deploy-staging
  14:32:55    POST /go-mod-tidy       200   │  Method     POST
  14:32:40    POST /restart-dev       500   │  Source     barrel-myproject
  14:32:20    POST /deploy-staging    200   │  Time       14:33:10
                                            │  Status     200 OK
                                            │  Duration   4.2s
                                            │
                                            │  Script     ~/scripts/deploy.sh
                                            │
                                            │  stdout
                                            │  ──────────────────────────────────
                                            │  Deploying to staging...
                                            │  Build successful
                                            │  Deployed v2.3.1 to staging
                                            │
                                            │  stderr
                                            │  ──────────────────────────────────
                                            │  (empty)
                                            │
──────────────────────────────────────────  │  ──────────────────────────────────
[↑↓ Nav]                      4/500 shown   │  4 bridge calls total     🥃
```

### Layout

Two-pane like history tabs. Left shows log entries, right shows detail of selected entry.

**Left pane rows:**
```
  HH:MM:SS  [▶] METHOD /path         status
```

**Right pane:** path, method, source, time, status, duration, script path, stdout, stderr.

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header               | `Amber` (#D4A04F) bold              |
| Port label                   | `ParchmentDim` (#A89880)             |
| Method                       | `SpringWater` (#7EC8C8)              |
| Path                         | `ParchmentBright` (#F0E6D6)          |
| Status 2xx                   | `BrassGreen` (#7A9B6D)               |
| Status 5xx                   | `BarrelChar` (#CD5C5C)               |
| Script path                  | `Copper` (#C87533)                   |
| stdout content               | `ParchmentBright` (#F0E6D6)          |
| stderr content               | `BarrelCharBright` (#E07070)         |
| `(empty)` placeholder        | `ParchmentDim` (#A89880) italic      |
| Duration value               | `ParchmentBright` (#F0E6D6)          |

### Key Bindings

Same as history tabs: `↑`/`↓`/`k`/`j`/`g`/`G`/`PgUp`/`PgDn`.

### Empty State

```
              🥃

     No bridge calls yet

     Configure routes in the Routes tab,
     then containers can call them via
     localhost:4343
```

Text in ParchmentDim, italic.

### Error States
- Failed bridge calls (5xx) have their rows tinted: status in BarrelChar.
- If bridge server is down, banner: `⚠ Bridge server not running` in BarrelCharBright.

---

## Screen 8: Execution Bridge Routes Tab

**Theme:** The cooperage workshop map -- which tools are available on the workbench.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Bridge Routes                                                     3 routes
  Containers call localhost:4343/{route} to execute host scripts
──────────────────────────────────────────────────────────────────────────────────

  API Route                    Script Path                          Status
  ─────────────────────────    ─────────────────────────────────    ──────
▶ /deploy-staging              ~/scripts/deploy-staging.sh          ✓ found
  /go-mod-tidy                 ~/scripts/go-mod-tidy.sh             ✓ found
  /restart-dev                 ~/scripts/restart-dev.sh             ✗ missing

──────────────────────────────────────────────────────────────────────────────────
  Scripts should take no input and handle their own concurrency.
  Full logs: ~/.cooper/logs/
──────────────────────────────────────────────────────────────────────────────────
[n New] [e Edit] [Delete] [↑↓ Nav]                                        🥃
```

### Layout

Single-pane table layout (no detail pane needed -- routes are simple key-value).

**Table columns:**
- API Route (left-aligned)
- Script Path (left-aligned)
- Status: `✓ found` / `✗ missing` (whether script file exists on disk)

**Helper text** below table with best-practice reminder.

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header               | `Amber` (#D4A04F) bold              |
| Route count                  | `ParchmentBright` (#F0E6D6)          |
| Description text             | `ParchmentDim` (#A89880) italic      |
| Column headers               | `ParchmentDim` (#A89880) underline   |
| Route path (selected)        | `ParchmentBright` (#F0E6D6) bold     |
| Route path (normal)          | `ParchmentBright` (#F0E6D6)          |
| Script path                  | `Copper` (#C87533)                   |
| `✓ found`                    | `BrassGreenBright` (#9ACD8B)         |
| `✗ missing`                  | `BarrelCharBright` (#E07070)         |
| Selected row bg              | `WornOak` (#2E2620)                  |
| Helper text                  | `ParchmentDim` (#A89880) italic      |

### Key Bindings

| Key      | Action                              |
|----------|-------------------------------------|
| `↑`/`k`  | Move selection up                  |
| `↓`/`j`  | Move selection down                |
| `n`      | Add new route (opens inline editor) |
| `e`      | Edit selected route                 |
| `Delete` | Delete selected route               |
| `Enter`  | Edit selected route                 |

### Inline Editor

When adding/editing a route, the selected row becomes editable:

```
▶ /[_______________]           ~/[_________________________________]
  ^cursor here
```

- Two text fields: API path and script path.
- `Tab` moves between fields.
- `Enter` saves the route.
- `Esc` cancels editing.
- Field borders in StaveGrain, text input in ParchmentBright, cursor indicator in Amber.

### Empty State

```
              🥃

     No bridge routes configured

     Press n to add your first route.
     Routes let containers execute host scripts
     via localhost:4343/{route}
```

### Error States
- If a script path is `✗ missing`, the row's script path is shown in BarrelCharBright.
- Attempting to save a route with an invalid path shows an inline error below the editor field in BarrelCharBright.

---

## Screen 9: Configure Tab ("Settings")

**Theme:** The master cooper's notebook -- parameters that govern the operation.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Runtime Settings
  Changes take effect immediately
──────────────────────────────────────────────────────────────────────────────────

  Tasting Room
  ─────────────────────────────────────────────────────────────────
▶ Approval timeout              [  5 ] seconds
  Before auto-deny. Range: 1-30.

  History Limits
  ─────────────────────────────────────────────────────────────────
  Rejected batch history         [ 500 ] entries
  Approved cask history          [ 500 ] entries
  Bridge log history             [ 500 ] entries

  These limits apply to the TUI view only.
  Full logs are written to ~/.cooper/logs/

──────────────────────────────────────────────────────────────────────────────────
[Enter Edit] [↑↓ Nav]                                                     🥃
```

### Layout

Single-pane form layout. Settings organized by category with section headers.

Each setting row:
```
  Setting label                [value] unit
  Helper text for this setting.
```

The selected setting has `▶` and `WornOak` background. Pressing Enter opens inline
editing: the `[value]` field becomes editable with a cursor. Left/Right/digit keys to change.

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header (top)         | `Amber` (#D4A04F) bold              |
| Section description          | `ParchmentDim` (#A89880) italic      |
| Category headers             | `Copper` (#C87533) bold              |
| Setting label (selected)     | `ParchmentBright` (#F0E6D6) bold     |
| Setting label (normal)       | `ParchmentBright` (#F0E6D6)          |
| Value brackets `[]`          | `StaveGrain` (#3D332A)               |
| Value text (display)         | `Amber` (#D4A04F)                    |
| Value text (editing)         | `Amber` (#D4A04F) bold, underline    |
| Unit text                    | `ParchmentDim` (#A89880)             |
| Helper text                  | `ParchmentDim` (#A89880) italic      |
| Selected row bg              | `WornOak` (#2E2620)                  |

### Key Bindings

| Key      | Action                              |
|----------|-------------------------------------|
| `↑`/`k`  | Move selection up                  |
| `↓`/`j`  | Move selection down                |
| `Enter`  | Edit selected setting              |
| `Esc`    | Cancel editing / unfocus           |

In edit mode:
| Key        | Action                            |
|------------|-----------------------------------|
| `0`-`9`    | Type digit                        |
| `Backspace`| Delete last digit                 |
| `Enter`    | Save value                        |
| `Esc`      | Cancel, restore previous value    |

### State Transitions
- Values are saved immediately on Enter. No separate "save" action needed.
- On value change, a brief confirmation appears next to the value: `✓ saved` in BrassGreenBright, fading after 1.5s.

### Error States
- If value is out of range: inline error below the setting in BarrelCharBright, e.g., `⚠ Must be between 1 and 30`.
- Value reverts to previous valid value if edit is cancelled or invalid.

---

## Screen 10: About Tab

**Theme:** The barrel stamp -- what's in this barrel, where it came from, how long it's been aging.

### ASCII Mockup

```
 🥃 cooper  [proxy:3128]  [bridge:4343]    📦 2 containers  ◉ proxy up
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  📦 Containers   🔥 Tasting Room   ✗ Rejected   ✓ Approved   ⇄ Logs   ⚙ Routes   ☰ Settings   ◉ About
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  🥃 Cooper v0.1.0                        Barrel-proof container tool
                                           for undiluted AI assistants
──────────────────────────────────────────────────────────────────────────────────

  Programming Tools                        Host        Container    Status
  ─────────────────────────────────────────────────────────────────────────
  Go                                       1.22.4      1.22.4       ✓ match
  Node.js                                  20.11.1     20.11.1      ✓ match
  Python                                   3.12.3      3.12.1       ⚠ mismatch
  Rust                                     1.78.0      1.78.0       ✓ match

  AI CLI Tools                             Host        Container    Status
  ─────────────────────────────────────────────────────────────────────────
  Claude Code                              1.0.12      1.0.12       ✓ match
  GitHub Copilot CLI                       --          0.6.2        ○ n/a
  OpenAI Codex CLI                         --          --           ○ off

  Platform
  ─────────────────────────────────────────────────────────────────────────
  Docker Engine                            27.0.3
  Squid Proxy                              6.9
  Config                                   ~/.cooper/config.json
  Logs                                     ~/.cooper/logs/

──────────────────────────────────────────────────────────────────────────────────
                                     Run cooper update to sync versions     🥃
```

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Version + brand              | `Amber` (#D4A04F) bold              |
| Tagline                      | `ParchmentDim` (#A89880) italic      |
| Category headers             | `Copper` (#C87533) bold              |
| Column headers               | `ParchmentDim` (#A89880) underline   |
| Tool names                   | `ParchmentBright` (#F0E6D6)          |
| Version numbers              | `ParchmentBright` (#F0E6D6)          |
| `✓ match`                    | `BrassGreenBright` (#9ACD8B)         |
| `⚠ mismatch`                | `WheatGold` (#E8D44D)                |
| `○ n/a` / `○ off`           | `ParchmentDim` (#A89880)             |
| `--` (not installed)         | `ParchmentFaint` (#6B5D4F)           |
| File paths                   | `Copper` (#C87533)                   |
| Footer hint                  | `ParchmentDim` (#A89880) italic      |

### Key Bindings

No special key bindings for this tab (it's read-only). Global keys (tab navigation, quit) still work.

### State Transitions
- Data is loaded once when entering the tab. Version comparison is computed at `cooper up` startup.
- If a mismatch is detected, the footer hint `Run cooper update to sync versions` is shown in WheatGold.
- If all versions match, footer shows `All tools in sync ✓` in BrassGreenBright.

---

## Screen 11: Exit Confirmation Modal

**Theme:** "Sealing the cask" -- you're about to close up shop. Are you sure?

### ASCII Mockup

```
            ╭──────────────────────────────────────────────────╮
            │                                                  │
            │           🥃 Seal the containers?                 │
            │                                                  │
            │   ──────────────────────────────────────────     │
            │                                                  │
            │   This will stop the proxy and all containers.   │
            │   2 active containers will lose network access.  │
            │                                                  │
            │   ──────────────────────────────────────────     │
            │                                                  │
            │   [Enter ✓ Seal & Exit]      [Esc Cancel]        │
            │                                                  │
            ╰──────────────────────────────────────────────────╯
```

### Layout

Modal is centered on screen. Background is dimmed (all text becomes ParchmentFaint).

- Width: 54 characters (including border).
- Border: rounded box (`╭╮╰╯─│`) in Amber.
- Interior padding: 1 line top/bottom, 3 chars left/right.
- Title: centered, whiskey glass + text, ParchmentBright bold.
- Dividers: `─` in StaveGrain.
- Body: ParchmentMuted, centered.
- Buttons: confirm in BrassGreenBright bold, cancel in ParchmentDim.

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Modal border                 | `Amber` (#D4A04F)                    |
| Modal background             | `DarkStave` (#241E18)                |
| Dimmed backdrop text         | `ParchmentFaint` (#6B5D4F)           |
| Title text                   | `ParchmentBright` (#F0E6D6) bold     |
| Divider                      | `StaveGrain` (#3D332A)               |
| Body text                    | `ParchmentMuted` (#C4B8A8)           |
| Container count (in body)    | `Amber` (#D4A04F) bold               |
| Confirm button               | `BrassGreenBright` (#9ACD8B) bold    |
| Cancel button                | `ParchmentDim` (#A89880)             |

### Key Bindings

| Key      | Action                         |
|----------|--------------------------------|
| `Enter`  | Confirm -- begin shutdown      |
| `y`      | Confirm -- begin shutdown      |
| `Esc`    | Cancel -- return to TUI        |
| `n`      | Cancel -- return to TUI        |

### State Transitions
- **Entry:** User presses `q` or `Ctrl+C` from any screen.
- **Confirm:** Transitions to Shutdown Screen (Screen 12).
- **Cancel:** Returns to previous screen, modal dismissed.

### Variants

**No active containers:**
```
            │   This will stop the proxy container.            │
```

**With active containers:**
```
            │   This will stop the proxy and all containers.   │
            │   N active containers will lose network access.  │
```

---

## Screen 12: Shutdown Screen

**Theme:** "Sealing the Barrel" -- the spirit is stored, the char cools, smoke clears.

### ASCII Mockup (shutdown in progress)

```
                          . 🥃 . .

                      c o o p e r

                   sealing the barrel...

                  ████████████░░░░░░░░   60%

              Stopping containers...

              [shutting down]  🥃
```

### ASCII Mockup (shutdown complete)

```
                        💨 🥃 💨

                      c o o p e r

                     barrel sealed

                  ████████████████████  100%

                Containers stored. Until next time.

                           🥃
```

### Color Specification

Same as Loading Screen (Screen 1), but with these differences:

| Element                      | Color                               |
|------------------------------|-------------------------------------|
| Complete subtitle            | `ParchmentDim` (#A89880)            |
| Complete message             | `ParchmentDim` (#A89880) italic     |
| Smoke emoji `💨`              | (emoji native color)               |

### Animation Details
- Same dot animation as loading screen.
- On completion: `"💨 🥃 💨"` (smoke clearing).
- After holding at 100% for 1s, the application exits.

### Key Bindings
No keys accepted during shutdown. The process completes automatically and exits.

### Status Messages by Step (Shutdown)

| Step                      | Message                              |
|---------------------------|--------------------------------------|
| StepStoppingContainers    | `Stopping containers...`             |
| StepStoppingProxy         | `Stopping proxy container...`        |
| StepCleaningNetworks      | `Clearing the cooperage floor...`    |
| StepReady (complete)      | `Containers stored. Until next time.`|

---

## Screen 13: `cooper configure` -- Welcome / Main Menu

**Theme:** The master cooper's workshop -- where you set up the cooperage before any
barrels can be filled.

### ASCII Mockup (first run)

```
                        🥃 Cooper

            Barrel-proof container tool
            for undiluted AI assistants
──────────────────────────────────────────────────────────────────────────────────

  Welcome to the cooperage. Let's set up your workshop.

  Setup Checklist
  ───────────────────────────────────────────────────────────────────────
▶ Programming Tools        Set up language environments           ○ not set
  AI CLI Tools             Install AI coding assistants           ○ not set
  Domain Whitelist         Configure allowed domains              ○ not set
  Port Forwarding          Forward host ports to containers       ○ not set
  Proxy & Bridge Ports     Set proxy and bridge ports             ○ not set

  ───────────────────────────────────────────────────────────────────────

  Docker Engine  ✓ 27.0.3      Cooper CA  ✓ valid (expires 2027-03-27)

──────────────────────────────────────────────────────────────────────────────────
[Enter Select] [↑↓ Nav] [s Save & Build] [q Quit]
```

### ASCII Mockup (returning user -- already configured)

```
  Setup Checklist
  ───────────────────────────────────────────────────────────────────────
▶ Programming Tools        Go 1.22, Node 20, Python 3.12         ✓ set
  AI CLI Tools             Claude Code, Copilot                   ✓ set
  Domain Whitelist         8 domains                              ✓ set
  Port Forwarding          3 rules                                ✓ set
  Proxy & Bridge Ports     3128 / 4343                            ✓ set

  ───────────────────────────────────────────────────────────────────────

  Docker Engine  ✓ 27.0.3      Cooper CA  ✓ valid (expires 2027-03-27)
```

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Brand `🥃 Cooper`            | `Amber` (#D4A04F) bold              |
| Tagline                      | `ParchmentDim` (#A89880) italic      |
| Welcome text                 | `ParchmentMuted` (#C4B8A8)           |
| Checklist header             | `Copper` (#C87533) bold              |
| Item name (selected)         | `ParchmentBright` (#F0E6D6) bold     |
| Item name (normal)           | `ParchmentBright` (#F0E6D6)          |
| Item description             | `ParchmentDim` (#A89880)             |
| `✓ set`                      | `BrassGreenBright` (#9ACD8B)         |
| `○ not set`                  | `ParchmentDim` (#A89880)             |
| Summary text (configured)    | `ParchmentDim` (#A89880)             |
| Docker version               | `BrassGreenBright` (#9ACD8B)         |
| CA status valid              | `BrassGreenBright` (#9ACD8B)         |
| CA status missing/expired    | `BarrelCharBright` (#E07070)         |
| Selected row bg              | `WornOak` (#2E2620)                  |

### Key Bindings

| Key      | Action                                     |
|----------|--------------------------------------------|
| `↑`/`k`  | Move selection up                         |
| `↓`/`j`  | Move selection down                       |
| `Enter`  | Enter selected setup flow                  |
| `s`      | Save configuration and prompt to build     |
| `q`      | Quit configure (warns if unsaved changes)  |
| `Esc`    | Same as `q`                                |

### State Transitions
- **Entry:** `cooper configure` command.
- **Enter flow:** Navigates to the selected sub-screen (14-19).
- **Save & Build:** Writes config.json, generates Dockerfiles, then asks "Would you like to build?"
- **Quit:** If changes are unsaved, shows a confirmation modal.

### Error States
- Docker not installed: `Docker Engine  ✗ not found` in BarrelCharBright, with message:
  `Docker Engine 20.10+ is required. Install from https://docs.docker.com/engine/install/`
- CA expired: `Cooper CA  ⚠ expired` in WheatGold, with hint to run `cooper configure --regenerate-ca`.

---

## Screen 14: Programming Tool Setup

### ASCII Mockup (tool list)

```
                        🥃 Cooper Configure

  Programming Tools
  Set up language environments for your containers
──────────────────────────────────────────────────────────────────────────────────

  Tool             Version             Mode        Host Version    Status
  ───────────────────────────────────────────────────────────────────────────
▶ Go               1.22.4              mirror      1.22.4          ✓ on
  Node.js          20.11.1             mirror      20.11.1         ✓ on
  Python           3.12.3              latest      3.12.1          ✓ on
  Rust             --                  --          1.78.0          ○ off

  ───────────────────────────────────────────────────────────────────────────

  Tools not listed? Add them manually in ~/.cooper/cli/Dockerfile.user
  Cooper will never overwrite that file.

──────────────────────────────────────────────────────────────────────────────────
[Enter Configure] [↑↓ Nav] [Esc Back]
```

### ASCII Mockup (tool detail -- e.g., Go selected)

```
                        🥃 Cooper Configure

  Go Configuration
──────────────────────────────────────────────────────────────────────────────────

  Status          [✓ Enabled ]

  Version Mode
  ──────────────────────────────────────────────────────
▶ ◉ Mirror host     1.22.4 (from host)
    ○ Latest         (fetches latest from go.dev)
    ○ Pin version    [___________]

  ──────────────────────────────────────────────────────

  Run cooper update after changing versions to rebuild
  the container image with the new version.

──────────────────────────────────────────────────────────────────────────────────
[Enter Select] [Space Toggle] [↑↓ Nav] [Esc Back]
```

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Tool name (selected)         | `ParchmentBright` (#F0E6D6) bold     |
| Tool name (normal)           | `ParchmentBright` (#F0E6D6)          |
| Version number               | `Amber` (#D4A04F)                    |
| Mode text                    | `ParchmentDim` (#A89880)             |
| Host version                 | `ParchmentDim` (#A89880)             |
| `✓ on`                       | `BrassGreenBright` (#9ACD8B)         |
| `○ off`                      | `ParchmentDim` (#A89880)             |
| Radio selected `◉`           | `Amber` (#D4A04F)                    |
| Radio unselected `○`         | `ParchmentDim` (#A89880)             |
| Toggle `[✓ Enabled ]`        | `BrassGreenBright` (#9ACD8B) bold    |
| Toggle `[  Disabled]`        | `ParchmentDim` (#A89880)             |
| Pin version input            | `Amber` (#D4A04F), brackets in StaveGrain |
| Helper text                  | `ParchmentDim` (#A89880) italic      |
| `cooper update` mention      | `Copper` (#C87533)                   |

### Key Bindings (Tool List)

| Key      | Action                               |
|----------|--------------------------------------|
| `↑`/`k`  | Move selection up                   |
| `↓`/`j`  | Move selection down                 |
| `Enter`  | Open tool detail                     |
| `Esc`    | Back to main configure menu          |

### Key Bindings (Tool Detail)

| Key      | Action                               |
|----------|--------------------------------------|
| `↑`/`k`  | Move selection up                   |
| `↓`/`j`  | Move selection down                 |
| `Enter`  | Select version mode / confirm pin   |
| `Space`  | Toggle enabled/disabled              |
| `Esc`    | Back to tool list                    |

In pin version text input mode:
| Key        | Action                            |
|------------|-----------------------------------|
| Characters | Type version string               |
| `Backspace`| Delete character                  |
| `Enter`    | Validate and save version         |
| `Esc`      | Cancel, restore previous value    |

### Error States
- Invalid version string: inline error `⚠ Invalid version. Example: 1.22.4` in BarrelCharBright.
- Host tool not found: Host Version column shows `--` in ParchmentFaint, Mirror option shows `(not detected on host)`.
- Network error resolving latest: `⚠ Could not fetch latest version` in BarrelCharBright.

---

## Screen 15: AI CLI Tool Setup

Identical layout to Programming Tool Setup (Screen 14), but for AI CLI tools.

### Tool List Mockup

```
  AI CLI Tools
  Install AI coding assistants in your containers
──────────────────────────────────────────────────────────────────────────────────

  Tool                  Version        Mode        Host Version    Status
  ───────────────────────────────────────────────────────────────────────────
▶ Claude Code           1.0.12         mirror      1.0.12          ✓ on
  GitHub Copilot CLI    0.6.2          latest      --              ✓ on
  OpenAI Codex CLI      --             --          --              ○ off
  OpenCode              --             --          --              ○ off

  ───────────────────────────────────────────────────────────────────────────

  Tools not listed? Add them in ~/.cooper/cli/Dockerfile.user
  Request new tool support at github.com/rickchristie/govner/issues
```

### Tool Detail

Same as Screen 14 detail view, adapted for the specific AI CLI tool.

### Key Bindings, Color Specification, Error States
All identical to Screen 14.

---

## Screen 16: Proxy Whitelist Setup

### ASCII Mockup (Domain Whitelist tab active)

```
                        🥃 Cooper Configure

  Proxy Whitelist
──────────────────────────────────────────────────────────────────────────────────
  Domain Whitelist    Port Forwarding
━━━━━━━━━━━━━━━━━━━━──────────────────────────────────────────────────────────────

  Default Domains (auto-configured from enabled AI tools)
  ─────────────────────────────────────────────────────────
  .anthropic.com                       ✓ (Claude Code)
  .openai.com                          ✓ (Codex CLI)
  .githubcopilot.com                   ✓ (Copilot)
  raw.githubusercontent.com            ✓ (read-only, safe)

  Your Domains
  ─────────────────────────────────────────────────────────
▶ api.mycompany.com                    ✓ subdomains: no
  .sentry.io                           ✓ subdomains: yes
  staging.myapp.dev                    ✓ subdomains: no

  ─────────────────────────────────────────────────────────

  Be strict. The Tasting Room lets you approve requests on-the-fly.
  Package registries (npm, pypi, etc.) are blocked by design to
  prevent supply-chain attacks. Module caches are mounted read-only.

──────────────────────────────────────────────────────────────────────────────────
[n New] [e Edit] [Delete] [Tab Switch] [↑↓ Nav] [Esc Back]
```

### ASCII Mockup (Port Forwarding tab active)

```
  Domain Whitelist    Port Forwarding
──────────────────────━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Port Forwarding Rules
  Container localhost:{port} is forwarded to host:{port} via two-hop socat relay.
  ─────────────────────────────────────────────────────────────────────────────

  Container Port      Host Port        Description
  ────────────        ────────         ─────────────────────────
▶ 5432                5432             PostgreSQL
  6379                6379             Redis
  8000-8100           8000-8100        Dev server range

  ─────────────────────────────────────────────────────────────────────────────

  Host services must bind to 0.0.0.0 or the Docker gateway IP.
  Services bound to 127.0.0.1 are NOT reachable from containers.

──────────────────────────────────────────────────────────────────────────────────
[n New] [e Edit] [Delete] [Tab Switch] [↑↓ Nav] [Esc Back]
```

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Active sub-tab text          | `Amber` (#D4A04F) bold              |
| Active sub-tab underline     | `DeepAmber` (#B8862D)                |
| Inactive sub-tab text        | `ParchmentDim` (#A89880)             |
| Default domain names         | `ParchmentDim` (#A89880)             |
| Default domain note          | `ParchmentFaint` (#6B5D4F) italic    |
| User domain names (selected) | `ParchmentBright` (#F0E6D6) bold     |
| User domain names (normal)   | `ParchmentBright` (#F0E6D6)          |
| Subdomain setting            | `ParchmentDim` (#A89880)             |
| Port numbers                 | `Amber` (#D4A04F)                    |
| Port range                   | `Amber` (#D4A04F)                    |
| Description                  | `ParchmentDim` (#A89880)             |
| Warning text                 | `OakSmoke` (#8B7355)                 |
| Security advice              | `ParchmentDim` (#A89880) italic      |

### Key Bindings

| Key      | Action                                     |
|----------|--------------------------------------------|
| `↑`/`k`  | Move selection up                         |
| `↓`/`j`  | Move selection down                       |
| `Tab`    | Switch between Domain Whitelist / Port Forwarding sub-tabs |
| `n`      | Add new domain or port rule                |
| `e`      | Edit selected item                         |
| `Delete` | Delete selected item                       |
| `Enter`  | Edit selected item                         |
| `Esc`    | Back to main configure menu                |

### Domain Editor (inline)

```
  Domain:      [api.example.com__________]
  Subdomains:  [○ No / ◉ Yes]
```

- Text input for domain name, radio toggle for subdomains.
- `Tab` between fields, `Enter` to save, `Esc` to cancel.

### Port Forwarding Editor (inline)

```
  Container Port:  [5432_____]
  Host Port:     [5432_____]
  Description:   [PostgreSQL_________________]
  Range:         [○ Single / ◉ Range]
```

For range mode, port fields accept `NNNN-NNNN` format.

### Error States
- Duplicate domain: `⚠ Domain already in whitelist` in BarrelCharBright.
- Invalid port: `⚠ Port must be between 1 and 65535` in BarrelCharBright.
- Port collision with proxy/bridge: `⚠ Port 3128 is reserved for the proxy` in BarrelCharBright.
- Overlapping port ranges: `⚠ Range overlaps with existing rule` in BarrelCharBright.

---

## Screen 17: Proxy Setup

### ASCII Mockup

```
                        🥃 Cooper Configure

  Proxy & Bridge Ports
──────────────────────────────────────────────────────────────────────────────────

  Proxy Port
  ──────────────────────────────────────────────────────
▶ Squid proxy port          [ 3128 ]
  The proxy intercepts and monitors all outbound traffic.
  Default: 3128 (Squid standard).

  Execution Bridge Port
  ──────────────────────────────────────────────────────
  Bridge API port           [ 4343 ]
  Containers call localhost:{port}/{route} to run host scripts.
  The bridge gives AI tools controlled access to host actions
  (deploy, restart-dev, go-mod-tidy) without direct machine access.
  Scripts should take no input and handle concurrency.
  Default: 4343.

  These two ports must not collide with each other or with
  user-configured port forwarding rules.

──────────────────────────────────────────────────────────────────────────────────
[Enter Edit] [↑↓ Nav] [Esc Back]
```

### Color Specification

Same as Settings tab (Screen 9). Port values in Amber, descriptions in ParchmentDim italic.

### Key Bindings

Same as Settings tab -- navigate, edit inline, save on Enter.

### Error States
- Port collision: `⚠ Ports must not collide. 3128 is already used by the proxy.` in BarrelCharBright.
- Invalid port: `⚠ Port must be between 1024 and 65535` in BarrelCharBright.

---

## Screen 18: Save & Build Prompt

This appears after the user presses `s` (Save & Build) from the main configure menu.

### ASCII Mockup (save complete, build prompt)

```
                        🥃 Cooper Configure

  Configuration saved
──────────────────────────────────────────────────────────────────────────────────

  ✓  config.json written to ~/.cooper/config.json
  ✓  CLI Dockerfile generated
  ✓  Proxy Dockerfile generated
  ✓  Squid configuration generated
  ✓  Entrypoint template generated

──────────────────────────────────────────────────────────────────────────────────

  Would you like to build the container images now?

  This will build:
    cooper-proxy         Proxy container with Squid SSL bump
    cooper-barrel-base   Base image with languages and AI tools
    cooper-barrel        Final image (with your Dockerfile.user, if any)

  Build typically takes 2-5 minutes on first run.

──────────────────────────────────────────────────────────────────────────────────
[y Build Now] [n Skip]                        Run cooper build later to build
```

### Color Specification

| Element                      | Color                                |
|------------------------------|--------------------------------------|
| Section header               | `Amber` (#D4A04F) bold              |
| `✓` check marks              | `BrassGreenBright` (#9ACD8B)        |
| File paths                   | `Copper` (#C87533)                   |
| Question text                | `ParchmentBright` (#F0E6D6) bold     |
| Image names                  | `Amber` (#D4A04F)                    |
| Image descriptions           | `ParchmentDim` (#A89880)             |
| Time estimate                | `ParchmentDim` (#A89880)             |
| `y` key                      | `SpringWater` (#7EC8C8) bold         |
| `n` key                      | `SpringWater` (#7EC8C8) bold         |
| Footer hint                  | `ParchmentDim` (#A89880) italic      |

### Key Bindings

| Key   | Action                                   |
|-------|------------------------------------------|
| `y`   | Start build (shows build progress)       |
| `n`   | Skip build, exit configure               |
| `Esc` | Same as `n`                              |

### Build Progress (after pressing `y`)

```
  Building container images...
──────────────────────────────────────────────────────────────────────────────────

  cooper-proxy          ████████████████████  ✓ built (42s)
  cooper-barrel-base    ██████████░░░░░░░░░░  building...
  cooper-barrel         ░░░░░░░░░░░░░░░░░░░░  waiting

──────────────────────────────────────────────────────────────────────────────────
  Overall              ██████████████░░░░░░░  65%

  Current: Installing Node.js 20.11.1...
```

Build complete:

```
  Build complete
──────────────────────────────────────────────────────────────────────────────────

  cooper-proxy          ████████████████████  ✓ built (42s)
  cooper-barrel-base    ████████████████████  ✓ built (3m 12s)
  cooper-barrel         ████████████████████  ✓ built (8s)

──────────────────────────────────────────────────────────────────────────────────

  All images ready. Run cooper up to start.

──────────────────────────────────────────────────────────────────────────────────
[Enter Done]
```

### State Transitions
- **Entry:** User presses `s` from main menu. Config is saved first, then this screen shows.
- **Build:** Shows build progress for each image in sequence.
- **Done:** User presses Enter to exit configure.
- **Skip:** User presses `n` to exit without building.

### Error States
- Build failure: progress bar stops, status shows `✗ failed`, error message displayed:
  ```
  cooper-barrel-base    ██████░░░░░░░░░░░░░░  ✗ failed

  Error: go 1.99.0 is not a valid Go version
  ```
- User can press `q` to exit or `r` to retry.

---

## Implementation Notes for Bubbletea

### Model Structure

The root model contains:
```go
type Model struct {
    // Tab state
    activeTab    Tab
    tabs         []Tab

    // Sub-models (one per tab)
    containers   containers.Model
    proxyMonitor proxymon.Model
    blocked      history.Model
    allowed      history.Model
    bridgeLogs   bridgeui.Model
    bridgeRoutes bridgeui.RoutesModel
    settings     settings.Model
    about        about.Model

    // Shared state
    config       *config.Config
    width        int
    height       int

    // Modal state
    confirm      ConfirmAction

    // Loading screen
    loadingScreen *LoadingScreen
    showLoading   bool

    // Channels
    proxyEventChan  <-chan proxy.Event
    bridgeEventChan <-chan bridge.Event
    // ...callbacks
}
```

### Message Routing

The root Update function routes messages to the active tab's sub-model:
```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Global key handling (tab switching, quit)
    // Modal handling (if active)
    // Route to active tab's Update
}
```

### Shared Components

- **TabBar:** Renders the tab bar, handles Tab/Shift+Tab/number keys.
- **TwoPaneView:** Reusable two-pane layout (used by Monitor, Blocked, Allowed, Bridge Logs).
- **HistoryList:** Scrollable list with selection, used by Blocked/Allowed/Logs.
- **InlineEditor:** Text input field for routes, domains, ports, versions.
- **Modal:** Centered overlay with dimmed background, confirm/cancel buttons.
- **ProgressBar:** Animated progress bar (reused from loading screen).

### Style Definitions (Go)

```go
// Pre-computed styles for the Distillery palette
var (
    // Base
    ColorCharredOak     = lipgloss.Color("#1A1410")
    ColorDarkStave      = lipgloss.Color("#241E18")
    ColorWornOak        = lipgloss.Color("#2E2620")
    ColorStaveGrain     = lipgloss.Color("#3D332A")
    ColorCooperageFloor = lipgloss.Color("#4A3D32")

    // Text
    ColorParchmentBright = lipgloss.Color("#F0E6D6")
    ColorParchmentDim    = lipgloss.Color("#A89880")
    ColorParchmentFaint  = lipgloss.Color("#6B5D4F")
    ColorParchmentMuted  = lipgloss.Color("#C4B8A8")

    // Accents
    ColorAmber             = lipgloss.Color("#D4A04F")
    ColorDeepAmber         = lipgloss.Color("#B8862D")
    ColorCopper            = lipgloss.Color("#C87533")
    ColorDarkCopper        = lipgloss.Color("#A0592A")
    ColorBrassGreen        = lipgloss.Color("#7A9B6D")
    ColorBrassGreenBright  = lipgloss.Color("#9ACD8B")
    ColorBarrelChar        = lipgloss.Color("#CD5C5C")
    ColorBarrelCharBright  = lipgloss.Color("#E07070")
    ColorOakSmoke          = lipgloss.Color("#8B7355")
    ColorWheatGold         = lipgloss.Color("#E8D44D")
    ColorSpringWater       = lipgloss.Color("#7EC8C8")

    // Animation
    ColorEmberDim    = lipgloss.Color("#B8862D")
    ColorEmberWarm   = lipgloss.Color("#D4A04F")
    ColorEmberBright = lipgloss.Color("#F0C060")

    // Styles
    TitleStyle = lipgloss.NewStyle().
        Foreground(ColorAmber).
        Bold(true)

    DimStyle = lipgloss.NewStyle().
        Foreground(ColorParchmentDim)

    RowSelectedStyle = lipgloss.NewStyle().
        Foreground(ColorCharredOak).
        Background(ColorWornOak).
        Bold(true).
        PaddingLeft(1).
        PaddingRight(1)

    // ... etc, matching all elements in the color specification tables
)
```

### Animation Implementation

The pending request ember pulse follows the same pattern as pgflock's `LockedAnimator`:

```go
type EmberPulse struct {
    frame       int
    frameStyles []lipgloss.Style
    icons       []string
}

func NewEmberPulse() *EmberPulse {
    icons := []string{"◈", "◈", "◆", "◈", "◈"}
    colors := []lipgloss.Color{
        ColorEmberDim, ColorEmberWarm, ColorEmberBright,
        ColorEmberWarm, ColorEmberDim,
    }
    styles := make([]lipgloss.Style, len(icons))
    for i := range icons {
        styles[i] = lipgloss.NewStyle().
            Foreground(colors[i]).
            Bold(true)
    }
    return &EmberPulse{
        frame:       0,
        frameStyles: styles,
        icons:       icons,
    }
}

func (p *EmberPulse) Tick() {
    p.frame = (p.frame + 1) % len(p.icons)
}

func (p *EmberPulse) Render() string {
    return p.frameStyles[p.frame].Render(p.icons[p.frame])
}
```

The approval shimmer follows the same pattern as pgflock's `CopyShimmer`.

### Countdown Timer Color Function

```go
func TimerColor(secondsRemaining int) lipgloss.Color {
    switch {
    case secondsRemaining >= 4:
        return ColorBrassGreen
    case secondsRemaining >= 2:
        return ColorWheatGold
    default:
        return ColorBarrelCharBright
    }
}
```

---

## Summary of Themed UI Copy

This section collects all the cooper-themed labels, status messages, and empty states
used throughout the design for quick reference.

### Screen Titles & Section Headers

| Element                    | Text                                                    |
|----------------------------|---------------------------------------------------------|
| Brand                      | `🥃 cooper`                                             |
| Loading title              | `c o o p e r`                                           |
| Containers section (proxy) | `The Still`                                             |
| Containers section (CLI)   | `The Rickhouse`                                         |
| Proxy Monitor              | `Tasting Room` / `Samples Pending` / `Sample Detail`   |
| Blocked History            | `Rejected Batches` / `Batch Detail`                     |
| Allowed History            | `Approved Casks` / `Cask Detail`                        |
| Bridge Logs                | `Bridge Logs` / `Log Detail`                            |
| Bridge Routes              | `Bridge Routes`                                         |
| Settings                   | `Runtime Settings`                                      |
| About                      | `🥃 Cooper v{X}` / `Barrel-proof container tool`       |
| Configure welcome          | `Welcome to the cooperage. Let's set up your workshop.` |
| Configure checklist        | `Setup Checklist`                                       |

### Loading / Shutdown Messages

| Context                 | Message                                      |
|-------------------------|----------------------------------------------|
| Startup in-progress     | `filling the barrel...`                      |
| Startup step: init      | `Preparing the cooperage...`                 |
| Startup step: proxy     | `Starting proxy container...`                |
| Startup step: squid     | `Waiting for Squid to respond...`            |
| Startup step: bridge    | `Lighting the bridge lantern...`             |
| Startup complete        | `barrel is charred` / `Ready to pour`        |
| Restart in-progress     | `re-charring the barrel...`                  |
| Restart complete        | `barrel is charred`                          |
| Shutdown in-progress    | `sealing the barrel...`                      |
| Shutdown step: containers | `Stopping containers...`                   |
| Shutdown step: proxy    | `Stopping proxy container...`                |
| Shutdown step: cleanup  | `Clearing the cooperage floor...`            |
| Shutdown complete       | `barrel sealed` / `Containers stored. Until next time.` |

### Empty States

| Screen           | Message                                                        |
|------------------|----------------------------------------------------------------|
| Containers       | `The rickhouse is empty -- no containers running`              |
|                  | `Run cooper cli to start a container`                          |
| Proxy Monitor    | `All clear -- nothing to taste`                                |
|                  | `Whitelisted traffic flows undisturbed`                        |
| Blocked          | `No rejected batches -- clean run`                             |
| Allowed          | `No traffic yet -- the still is warming up`                    |
|                  | `Requests will appear once containers start making calls`      |
| Bridge Logs      | `No bridge calls yet`                                          |
| Bridge Routes    | `No bridge routes configured`                                  |
|                  | `Press n to add your first route.`                             |

### Exit Modal

| Variant                | Title                  | Body                                           |
|------------------------|------------------------|-------------------------------------------------|
| With containers        | `Seal the containers?` | `This will stop the proxy and all containers.`  |
|                        |                        | `N active containers will lose network access.` |
| No containers          | `Seal the containers?` | `This will stop the proxy container.`           |
| Confirm button         |                        | `Seal & Exit`                                   |

### Footer Status Messages

| State                  | Message                           |
|------------------------|-----------------------------------|
| Health check idle      | `All healthy ✓`                   |
| Health checking        | `Checking...`                     |
| Health warning         | `Timeout: {container}`            |
| Health error           | `{component} unreachable`         |
| Proxy disconnected     | `Proxy connection lost -- all requests auto-denied` |
| About: versions match  | `All tools in sync ✓`             |
| About: version mismatch| `Run cooper update to sync versions`|
