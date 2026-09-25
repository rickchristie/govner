// The VM test seeds one synthetic cookie with the real native app, then
// reads it after normal Cooper launches. It uses no account or remote site.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';

const seed = process.argv[2] === 'seed';
const profile = process.env.CODEX_ELECTRON_USER_DATA_PATH;
const key = 'cooper-synthetic-native-cookie-key';
const environment = { ...process.env };
let keyring, windowManager, child;

function run(command, args, input) {
  const result = spawnSync(command, args, { env: environment, input, encoding: 'utf8', timeout: 10000 });
  assert.equal(result.status, 0, `${command}: ${result.stderr}`);
}

async function until(check, message, timeout = 15000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw Error(message);
}

try {
  if (seed) {
    keyring = await fs.mkdtemp('/tmp/cooper-cookie-keyring-');
    environment.GNOME_KEYRING_CONTROL = keyring;
    run('gnome-keyring-daemon', ['--start', '--components=secrets', '--control-directory=' + keyring]);
    run('gdbus', ['call', '--session', '--dest', 'org.freedesktop.secrets', '--object-path', '/org/freedesktop/secrets',
      '--method', 'org.freedesktop.Secret.Service.SetAlias', 'default', '/org/freedesktop/secrets/collection/session']);
    run('secret-tool', ['store', '--collection=session', '--label=Chromium Safe Storage',
      'application', 'chromium', 'xdg:schema', 'chrome_libsecret_os_crypt_password_v2'], key);
    windowManager = spawn('openbox', [], { env: environment, stdio: 'ignore' });
  }
  await fs.rm(profile + '/DevToolsActivePort', { force: true });
  child = spawn('/usr/bin/chatgpt', ['--user-data-dir=' + profile, '--disable-setuid-sandbox',
    '--password-store=gnome-libsecret', '--proxy-server=' + environment.HTTPS_PROXY, '--remote-debugging-port=0'],
    { env: environment, stdio: 'ignore' });
  const exited = once(child, 'exit');
  const info = await until(async () => {
    assert.equal(child.exitCode, null, 'native ChatGPT exited before its test connection');
    try {
      const port = (await fs.readFile(profile + '/DevToolsActivePort', 'utf8')).split('\n')[0];
      return await (await fetch('http://127.0.0.1:' + port + '/json/version')).json();
    } catch { return null; }
  }, 'native test connection did not start');
  const socket = new WebSocket(info.webSocketDebuggerUrl);
  await once(socket, 'open');
  let id = 0;
  const pending = new Map();
  socket.addEventListener('message', event => { const message = JSON.parse(event.data); pending.get(message.id)?.(message); });
  async function send(method, params = {}) {
    const current = ++id;
    const result = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => { pending.delete(current); reject(Error('native test request timed out: ' + method)); }, 5000);
      pending.set(current, message => { clearTimeout(timer); pending.delete(current); resolve(message); });
      socket.send(JSON.stringify({ id: current, method, params }));
    });
    assert.equal(result.error, undefined, JSON.stringify(result.error));
    return result.result;
  }
  if (seed) {
    await send('Storage.setCookies', { cookies: [{ name: 'cooper_login_fixture', value: 'native-cookie-kept',
      domain: 'chatgpt.example.invalid', path: '/', secure: true, httpOnly: true, expires: Math.floor(Date.now() / 1000) + 3600 }] });
    // Native Chromium batches disk writes. An immediate Quit can precede
    // the commit; wait for the real encrypted database, never a fixed delay.
    await until(async () => {
      try { return (await fs.readFile(profile + '/Default/Cookies')).includes(Buffer.from('v11')); }
      catch { return false; }
    }, 'native encrypted cookie was not committed', 70000);
  } else {
    const result = await send('Storage.getCookies');
    assert.ok(result.cookies.some(cookie => cookie.name === 'cooper_login_fixture' && cookie.value === 'native-cookie-kept'),
      'native encrypted cookie was lost after a Cooper launch');
  }
  await until(() => spawnSync('xdotool', ['search', '--onlyvisible', '--all', '--pid', String(child.pid), '.*'],
    { env: environment, stdio: 'ignore' }).status === 0, 'native app window did not open');
  // Quit uses the normal app lifecycle. X may close the window before the
  // key-up event, so the process exit is the authoritative result. Release
  // the keys with no target window before the next desktop accepts input.
  await until(() => {
    if (child.exitCode !== null || child.signalCode !== null) return true;
    spawnSync('xdotool', ['search', '--onlyvisible', '--all', '--pid', String(child.pid), '.*',
      'windowactivate', '--sync', 'key', '--clearmodifiers', 'ctrl+q'], { env: environment, stdio: 'ignore', timeout: 1000 });
    return false;
  }, 'native app did not quit', 20000);
  await exited;
  run('xdotool', ['keyup', 'q', 'Control_L', 'Control_R']);
  socket.close();
  const lock = profile + '/SingletonLock';
  if (await fs.readlink(lock).catch(() => '') === `${os.hostname()}-${child.pid}`) await fs.unlink(lock);
  console.log(seed ? 'DESKTOP_NATIVE_COOKIE_SEEDED' : 'DESKTOP_NATIVE_COOKIE_RESTORED');
} finally {
  if (child && child.exitCode === null) child.kill('SIGKILL');
  windowManager?.kill();
  if (keyring) await fs.rm(keyring, { recursive: true, force: true });
}
