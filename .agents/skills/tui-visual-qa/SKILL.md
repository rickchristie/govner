---
name: tui-visual-qa
description: Capture and inspect deterministic Govner TUI screenshots after layout, theme, resize, or interaction changes. Use for terminal visual QA and design refinement, but not as a replacement for behavior tests.
---

# TUI Visual QA

Use a deterministic storybook fixture and an isolated capture environment. Do
not capture the active desktop.

## Workflow

1. Read `AGENTS.TUI.md` and the project README before you change TUI code.
2. Use a deterministic `tui-test` or storybook mode. Use fake data and fixed
   application state.
3. Run the required behavior tests. Build a static Linux capture binary under
   `/tmp`. For Gowt, run:

   ```bash
   GOCACHE=/tmp/gowt-tui-capture-go-cache CGO_ENABLED=0 \
     go build -C ./gowt -o /tmp/gowt-tui-capture . \
     > /tmp/gowt-tui-capture-build.txt 2>&1
   ```

4. Prepare the pinned capture images when they are not present. This step uses
   the network and can require authorization:

   ```bash
   ./scripts/capture-tui.sh prepare all \
     > /tmp/tui-capture-prepare.txt 2>&1
   ```

5. Use VHS for the normal capture. Set a screen condition when stable text is
   available:

   ```bash
   ./scripts/capture-tui.sh \
     --output /tmp/gowt-tree.png \
     --wait-regex GOWT \
     -- /tmp/gowt-tui-capture --storybook \
     > /tmp/gowt-tree-capture.txt 2>&1
   ```

6. Repeat `--key` and `--type` options to capture a specific interaction
   state. The actions run in the order in which they occur on the command
   line.
7. Use the isolated Xvfb backend when native XTerm rendering is important or
   VHS cannot render the program:

   ```bash
   ./scripts/capture-tui.sh \
     --backend xvfb \
     --output /tmp/gowt-log.png \
     --key enter \
     -- /tmp/gowt-tui-capture --storybook \
     > /tmp/gowt-log-capture.txt 2>&1
   ```

8. Open every PNG with an image inspection tool. Check alignment, clipping,
   hierarchy, contrast, focus, disabled states, errors, help text, Unicode,
   and the smallest supported terminal size.
9. Refine the design and repeat the same fixture, dimensions, font, theme, and
   locale. Use a new output name, or use `--force` only for the exact image
   that you own.
10. Report the backend, dimensions, image paths, and visual findings. Treat a
    PNG as review evidence, not as a golden behavior test.

## Boundaries

- Run the wrapper from the repository root.
- Keep generated images and logs under `/tmp`.
- Use fake data. Do not capture tokens, credentials, or production content.
- Do not install display packages on the host. The Xvfb image contains its own
  display tools.
- The capture containers have no network and receive only the static
  executable and a private temporary output directory.
