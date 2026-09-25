// The VM fixture supplies a private viewer URL and disposable workspace.
// Run through test-vm-dev.sh desktop chatgpt after prepare-agent chatgpt.
const { chromium, expect } = require('playwright/test');
const fs = require('node:fs/promises');
const path = require('node:path');

async function main() {
  const [url, artifacts, workspace] = process.argv.slice(2);
  const errors = [];
  const browser = await chromium.launch({ headless: true, executablePath: process.env.COOPER_DESKTOP_BROWSER_PATH });
  try {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
    await page.goto(url);
    await expect(page.getByRole('status')).toHaveText('Connected', { timeout: 15000 });
    expect(new URL(page.url()).hash).toBe('');
    expect(await context.cookies()).toEqual([]);
    const canvas = page.locator('canvas');
    await expect(canvas).toBeVisible();
    await expect.poll(() => canvas.evaluate(element => element.width)).toBeGreaterThan(500);
    await page.screenshot({ path: path.join(artifacts, 'desktop-app.png') });
    await page.getByRole('button', { name: 'Terminal', exact: true }).click();
    await expect.poll(async () => {
      try { return await fs.readFile(path.join(workspace, 'desktop-terminal-ready'), 'utf8'); }
      catch { return ''; }
    }, { timeout: 15000 }).toBe('ready');
    await page.keyboard.type("printf 'cooper-gui-ok\\n' > cooper-desktop-input", { delay: 5 });
    await page.keyboard.press('Enter');
    await expect.poll(async () => {
      try { return await fs.readFile(path.join(workspace, 'cooper-desktop-input'), 'utf8'); }
      catch { return ''; }
    }, { timeout: 15000 }).toBe('cooper-gui-ok\n');
    await page.getByRole('button', { name: 'Clipboard', exact: true }).click();
    await page.getByRole('textbox', { name: 'Clipboard text' }).fill('cooper-clipboard-ok');
    await page.getByRole('button', { name: 'Send to desktop', exact: true }).click();
    await page.keyboard.type('xclip -selection clipboard -o > cooper-desktop-clipboard', { delay: 5 });
    await page.keyboard.press('Enter');
    await expect.poll(async () => {
      try { return await fs.readFile(path.join(workspace, 'cooper-desktop-clipboard'), 'utf8'); }
      catch { return ''; }
    }, { timeout: 15000 }).toBe('cooper-clipboard-ok');
    await page.keyboard.type("printf cooper-from-guest | xclip -selection clipboard", { delay: 5 });
    await page.keyboard.press('Enter');
    await page.getByRole('button', { name: 'Clipboard', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Clipboard text' })).toHaveValue('cooper-from-guest', { timeout: 10000 });
    await page.screenshot({ path: path.join(artifacts, 'desktop-clipboard.png') });
    await page.getByRole('button', { name: 'Use host image', exact: true }).click();
    await page.keyboard.type('xclip -selection clipboard -t image/png -o > cooper-desktop-image', { delay: 5 });
    await page.keyboard.press('Enter');
    await expect.poll(async () => {
      try { return (await fs.readFile(path.join(workspace, 'cooper-desktop-image'))).subarray(0, 8).toString('hex'); }
      catch { return ''; }
    }, { timeout: 15000 }).toBe('89504e470d0a1a0a');
    await page.screenshot({ path: path.join(artifacts, 'desktop-terminal.png') });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await expect.poll(() => canvas.evaluate(element => element.width)).toBe(1440);
    await page.reload();
    await expect(page.getByRole('status')).toHaveText('Connected', { timeout: 15000 });
    expect(await context.cookies()).toEqual([]);
    await page.screenshot({ path: path.join(artifacts, 'desktop-reconnect.png') });
    await canvas.evaluate(element => {
      element.cooperPreviousFrame = element.getContext('2d').getImageData(0, 0, element.width, element.height);
    });
    await fs.writeFile(path.join(workspace, 'desktop-request-app'), 'ready');
    await page.getByRole('button', { name: 'ChatGPT', exact: true }).click();
    await expect.poll(async () => {
      try { return await fs.readFile(path.join(workspace, 'desktop-app-ready'), 'utf8'); }
      catch { return ''; }
    }, { timeout: 15000 }).toContain('HEIGHT=');
    const geometry = Object.fromEntries((await fs.readFile(path.join(workspace, 'desktop-app-ready'), 'utf8'))
      .trim().split('\n').map(line => { const [key, value] = line.split('='); return [key, Number(value)]; }));
    // Focus can precede both native rendering and the next RFB frame. Wait
    // for contrasting app content in the window's center that replaces the
    // terminal. Borders, a cursor, or a blank loading frame are insufficient.
    await expect.poll(() => canvas.evaluate((element, geometry) => {
      const x = Math.max(0, Math.floor(geometry.X + geometry.WIDTH / 4));
      const y = Math.max(0, Math.floor(geometry.Y + geometry.HEIGHT / 4));
      const width = Math.min(element.width - x, Math.floor(geometry.WIDTH / 2));
      const height = Math.min(element.height - y, Math.floor(geometry.HEIGHT / 2));
      const pixels = element.getContext('2d').getImageData(x, y, width, height).data;
      const previous = element.cooperPreviousFrame.data;
      let dark = 0, light = 0, changed = 0;
      for (let row = 0; row < height; row++) {
        for (let column = 0; column < width; column++) {
          const index = (row * width + column) * 4;
          const old = ((row + y) * element.width + column + x) * 4;
          const colors = [pixels[index], pixels[index + 1], pixels[index + 2]];
          if (Math.max(...colors) < 100) dark++;
          if (Math.min(...colors) > 200) light++;
          if (colors.reduce((sum, value, channel) => sum + Math.abs(value - previous[old + channel]), 0) > 60) changed++;
        }
      }
      return dark > 400 && light > 400 && changed > 1000;
    }, geometry), { timeout: 30000 }).toBe(true);
    await page.screenshot({ path: path.join(artifacts, 'desktop-app-restored.png') });
    expect(errors).toEqual([]);
    console.log('DESKTOP_BROWSER_OK');
  } catch (error) {
    const page = browser.contexts()[0]?.pages()[0];
    if (page) await page.screenshot({ path: path.join(artifacts, 'desktop-failure.png') });
    throw error;
  } finally {
    await browser.close();
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
