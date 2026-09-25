import RFB from './novnc/core/rfb.js';

const screen = document.getElementById('screen');
const status = document.getElementById('status');
const message = document.getElementById('message');
const dialog = document.getElementById('clipboard-dialog');
const text = document.getElementById('clipboard-text');
const controls = ['app', 'terminal', 'clipboard', 'clipboard-image'].map(id => document.getElementById(id));
let rfb;
let token = location.hash.slice(1);
history.replaceState(null, '', '/');
// Storage is private to this tab and origin, including its port. Cookies
// would expose the capability to other apps on the same loopback address.
try { token ||= sessionStorage.getItem('cooper-desktop-token') || ''; }
catch { /* The private link still works if browser storage is disabled. */ }

function showError(error) {
  status.textContent = 'Disconnected';
  message.textContent = error.message;
  message.hidden = false;
  controls.forEach(control => control.disabled = true);
}

async function connect() {
  if (rfb) rfb.disconnect();
  const health = await fetch('/health', { headers: { Authorization: `Bearer ${token}` }, credentials: 'omit' });
  if (!health.ok) throw Error('Open the private desktop link printed by Cooper.');
  try { sessionStorage.setItem('cooper-desktop-token', token); }
  catch { /* Reconnect can use the token kept in this page. */ }
  status.textContent = 'Connecting…';
  message.textContent = 'Opening your desktop…';
  message.hidden = false;
  const connection = new RFB(screen, `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/websockify`, {
    wsProtocols: ['binary', `cooper-auth-${token}`],
  });
  rfb = connection;
  connection.resizeSession = true;
  connection.scaleViewport = true;
  connection.showDotCursor = true;
  connection.addEventListener('connect', () => {
    if (rfb !== connection) return;
    status.textContent = 'Connected';
    message.hidden = true;
    controls.forEach(control => control.disabled = false);
    connection.focus();
  });
  connection.addEventListener('disconnect', () => {
    if (rfb === connection) showError(Error('The display connection closed. Reconnect, or run the Cooper launch command again.'));
  });
  connection.addEventListener('securityfailure', () => showError(Error('The desktop refused the display connection. Run the Cooper launch command again.')));
  connection.addEventListener('clipboard', event => { text.value = event.detail.text; });
}

function shortcut(letter) {
  rfb.sendKey(0xffe3, 'ControlLeft', true);
  rfb.sendKey(0xffe9, 'AltLeft', true);
  rfb.sendKey(letter.charCodeAt(0), `Key${letter.toUpperCase()}`, true);
  rfb.sendKey(letter.charCodeAt(0), `Key${letter.toUpperCase()}`, false);
  rfb.sendKey(0xffe9, 'AltLeft', false);
  rfb.sendKey(0xffe3, 'ControlLeft', false);
  rfb.focus();
}

document.getElementById('terminal').addEventListener('click', () => shortcut('t'));
document.getElementById('app').addEventListener('click', () => shortcut('c'));
document.getElementById('reconnect').addEventListener('click', () => connect().catch(showError));
document.getElementById('fullscreen').addEventListener('click', async () => {
  try {
    if (document.fullscreenElement) await document.exitFullscreen();
    else await document.documentElement.requestFullscreen();
  } catch { /* The browser can refuse full screen. The normal viewer stays usable. */ }
});
document.getElementById('clipboard').addEventListener('click', () => { dialog.showModal(); text.focus(); });
document.getElementById('clipboard-close').addEventListener('click', () => { dialog.close(); rfb.focus(); });
document.getElementById('clipboard-send').addEventListener('click', () => {
  rfb.clipboardPasteFrom(text.value);
  dialog.close();
  rfb.focus();
});
document.getElementById('clipboard-image').addEventListener('click', () => { dialog.close(); shortcut('i'); });
document.getElementById('clipboard-copy').addEventListener('click', async event => {
  try { await navigator.clipboard.writeText(text.value); event.target.textContent = 'Copied'; }
  catch { text.focus(); text.select(); event.target.textContent = 'Use Ctrl+C to copy'; }
});
connect().catch(showError);
