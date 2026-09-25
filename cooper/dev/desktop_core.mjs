// Exercise the real desktop engine without a remote model or account. The
// model fixture asks for writes outside the workspace. Only Cooper's engine
// wrapper supplies full access; the saved user configuration stays read-only.
import assert from 'node:assert/strict';
import http from 'node:http';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import readline from 'node:readline';

const root = await fs.mkdtemp(path.join(os.tmpdir(), 'cooper-desktop-core-'));
const workspace = path.join(root, 'workspace');
const home = path.join(root, 'home');
const state = path.join(home, '.codex');
await fs.mkdir(workspace);
await fs.mkdir(state, { recursive: true });
const config = `sandbox_mode = "read-only"\napproval_policy = "on-request"\n\n[projects."${workspace}"]\ntrust_level = "trusted"\n`;
await fs.writeFile(path.join(state, 'config.toml'), config);
const marker = 'COOPER_DESKTOP_CORE_OK';
const environment = { PATH: process.env.PATH, HOME: home, CODEX_HOME: state };

async function checkAppServer() {
  const child = spawn('/opt/cooper/desktop/core.sh', ['app-server'], {
    cwd: workspace, stdio: ['pipe', 'pipe', 'pipe'], env: environment,
  });
  const pending = new Map();
  let errors = '';
  child.stderr.on('data', data => errors += data);
  const closed = new Promise(resolve => child.on('close', resolve));
  const lines = readline.createInterface({ input: child.stdout });
  lines.on('line', line => {
    const message = JSON.parse(line);
    pending.get(message.id)?.(message);
  });
  const send = (id, method, params) => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(Error(`App-server ${method} timed out: ${errors}`)), 15000);
    pending.set(id, result => { clearTimeout(timer); pending.delete(id); resolve(result); });
    child.stdin.write(JSON.stringify({ id, method, params }) + '\n');
  });
  try {
    await send(1, 'initialize', { clientInfo: { name: 'cooper-desktop-test', version: '1' }, capabilities: { experimentalApi: true } });
    child.stdin.write('{"method":"initialized"}\n');
    const result = await send(2, 'config/read', { includeLayers: false });
    assert.equal(result.result?.config.sandbox_mode, 'danger-full-access', JSON.stringify(result));
    assert.equal(result.result?.config.approval_policy, 'never', JSON.stringify(result));
  } finally {
    lines.close();
    child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 2000);
    await closed;
    clearTimeout(timer);
  }
}
const command = `printf workspace-ok > '${workspace}/written'; printf state-ok > '${state}/written'`;
let calls = 0;
let toolResult = false;
let toolOutput = '';
const server = http.createServer((request, response) => {
  const chunks = [];
  let size = 0;
  request.on('data', chunk => {
    size += chunk.length;
    if (size > 4 * 1024 * 1024) request.destroy();
    else chunks.push(chunk);
  });
  request.on('end', () => {
    try {
      const body = JSON.parse(Buffer.concat(chunks));
      calls++;
      const tools = body.tools.map(tool => tool.name || tool.function?.name);
      const name = ['exec_command', 'shell_command', 'shell'].find(name => tools.includes(name));
      assert.ok(name, `No shell tool: ${JSON.stringify(tools)}`);
      toolResult ||= body.input.some(item => item.type === 'function_call_output');
      toolOutput = body.input.filter(item => item.type === 'function_call_output').map(item => item.output).join('\n');
      const args = name === 'exec_command' ? { cmd: command } : name === 'shell_command' ? { command } : { command: ['bash', '-c', command] };
      const item = toolResult
        ? { id: 'msg_fixture', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: marker, annotations: [] }] }
        : { id: 'fc_fixture', type: 'function_call', call_id: 'call_fixture', name, arguments: JSON.stringify(args), status: 'completed' };
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      const events = [
        { type: 'response.created', response: { id: `resp_${calls}`, status: 'in_progress', output: [] } },
        { type: 'response.output_item.done', output_index: 0, item },
        { type: 'response.completed', response: { id: `resp_${calls}`, status: 'completed', output: [item], usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } } },
      ];
      for (const event of events) response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
      response.end();
    } catch (error) {
      console.error(error);
      response.writeHead(500);
      response.end();
    }
  });
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
try {
  await checkAppServer();
  const settings = ['model_provider="fixture"', 'model_providers.fixture.name="Fixture"',
    `model_providers.fixture.base_url="http://127.0.0.1:${server.address().port}/v1"`,
    'model_providers.fixture.wire_api="responses"', 'model_providers.fixture.requires_openai_auth=false',
    'model_providers.fixture.supports_websockets=false', 'model="gpt-5"'];
  const child = spawn('timeout', ['--kill-after=2s', '45s', '/opt/cooper/desktop/core.sh', 'exec',
    '--skip-git-repo-check', '--json', ...settings.flatMap(setting => ['-c', setting]),
    'Run the requested file writes, then reply with ' + marker], {
    cwd: workspace, stdio: ['ignore', 'pipe', 'pipe'],
    env: environment,
  });
  let output = '', errors = '';
  child.stdout.on('data', data => output += data);
  child.stderr.on('data', data => errors += data);
  const status = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
  assert.equal(status, 0, errors + output);
  assert.ok(output.includes(marker), output);
  assert.ok(toolResult && calls >= 2, 'The engine did not return a tool result');
  assert.ok(toolOutput.includes('0'), toolOutput);
  await fs.access(path.join(workspace, 'written')).catch(() => { throw Error(toolOutput + '\n' + output + '\n' + errors); });
  assert.equal(await fs.readFile(path.join(workspace, 'written'), 'utf8'), 'workspace-ok');
  assert.equal(await fs.readFile(path.join(state, 'written'), 'utf8'), 'state-ok');
  assert.equal(await fs.readFile(path.join(state, 'config.toml'), 'utf8'), config);
  console.log(marker);
} finally {
  server.close();
  await fs.rm(root, { recursive: true, force: true });
}
