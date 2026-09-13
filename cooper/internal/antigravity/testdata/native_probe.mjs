// Run the installed CLI against a local model with fabricated credentials.
// Node is already a base-image dependency, including minimal agent images.
import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

const marker = "COOPER_ANTIGRAVITY_OK";
const requests = [];
const server = http.createServer((request, response) => {
  const chunks = [];
  let size = 0;
  request.on("data", (chunk) => {
    size += chunk.length;
    if (size > 2 * 1024 * 1024) {
      request.destroy(new Error("fixture request is too large"));
      return;
    }
    chunks.push(chunk);
  });
  request.on("end", () => {
    requests.push(JSON.parse(Buffer.concat(chunks).toString()));
    const result = {
      candidates: [{ content: { role: "model", parts: [{ text: marker }] }, finishReason: "STOP" }],
      usageMetadata: { promptTokenCount: 1, candidatesTokenCount: 1, totalTokenCount: 2 },
    };
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    response.end("data: " + JSON.stringify(result) + "\n\n");
  });
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const runtime = process.argv[2];
assert.ok(runtime === undefined || runtime === "cli" || runtime === "vm");
const shared = runtime !== undefined;
const home = shared ? process.env.HOME : fs.mkdtempSync(path.join(os.tmpdir(), "cooper-antigravity-"));
// Shared mode is only for the private VM fixture. Refuse an ordinary home.
if (shared) {
  assert.equal(fs.readFileSync(path.join(home, ".gemini", ".cooper-vm-state-antigravity"), "utf8").trim(), "antigravity");
}
try {
  const state = path.join(home, ".gemini", "antigravity-cli");
  fs.mkdirSync(state, { recursive: true });
  const record = path.join(state, "cooper-native-probe.json");
  assert.equal(fs.existsSync(record), runtime === "vm", "unexpected prior fixture state");
  const prior = runtime === "vm" ? JSON.parse(fs.readFileSync(record, "utf8")) : undefined;
  let turns = prior?.turns ?? 0;
  fs.writeFileSync(path.join(state, "settings.json"), '{"modelProvider":"gemini"}\n');
  // Keep only reviewed image helper paths. Clear host credentials, proxies,
  // keyring transport, and provider settings before the native process starts.
  const env = {};
  for (const name of ["PATH", "PLAYWRIGHT_DRIVER_PATH", "PLAYWRIGHT_NODEJS_PATH"]) {
    assert.ok(process.env[name], "missing image helper value: " + name);
    env[name] = process.env[name];
  }
  Object.assign(env, {
    HOME: home, AGY_CLI_DISABLE_AUTO_UPDATE: "true", GEMINI_API_KEY: "cooper-fake-key",
    GOOGLE_GEMINI_BASE_URL: "http://127.0.0.1:" + server.address().port,
  });
  async function turn(extra) {
    const command = ["--kill-after=3s", "25s", "/opt/cooper/bin/agy",
      "--print=Reply with only " + marker, "--print-timeout=20s", "--output-format=json",
      "--log-file=" + path.join(home, "native.log"), ...extra];
    const child = spawn("timeout", command, { cwd: home, env, stdio: ["ignore", "pipe", "pipe"] });
    let output = "";
    let errors = "";
    child.stdout.on("data", (data) => { output += data; });
    child.stderr.on("data", (data) => { errors += data; });
    const code = await new Promise((resolve, reject) => {
      child.on("error", reject);
      child.on("close", resolve);
    });
    assert.equal(code, 0, errors);
    const result = JSON.parse(output);
    assert.equal(result.status, "SUCCESS");
    assert.equal(result.response.trim(), marker);
    assert.equal(result.num_turns, ++turns);
    assert.ok(result.usage.output_tokens > 0);
    return result.conversation_id;
  }
  const hasPriorAnswer = (items) => items.some((body) => body.contents?.some((content) =>
    content.role === "model" && content.parts?.some((part) => part.text?.trim() === marker)));
  const conversation = await turn(prior ? ["--conversation=" + prior.conversation] : []);
  if (prior) {
    assert.equal(conversation, prior.conversation);
    assert.ok(hasPriorAnswer(requests), "VM request lost the Docker conversation");
  }
  const count = requests.length;
  assert.ok(conversation);
  assert.equal(await turn(["--conversation=" + conversation]), conversation);
  // The current prompt cannot supply a prior model-role answer. A second
  // native process must read that answer from the durable conversation state.
  assert.ok(hasPriorAnswer(requests.slice(count)),
    "restored request lost the prior model answer");
  if (shared) fs.writeFileSync(record, JSON.stringify({ conversation, turns }));
  // A real pseudo-terminal must reach native login and accept normal exit.
  // This separate home has no model key or saved account, including in the VM.
  const terminalHome = fs.mkdtempSync(path.join(os.tmpdir(), "cooper-antigravity-pty-"));
  try {
    const terminalEnv = { HOME: terminalHome, TERM: "xterm-256color", AGY_CLI_DISABLE_AUTO_UPDATE: "true" };
    for (const name of ["PATH", "PLAYWRIGHT_DRIVER_PATH", "PLAYWRIGHT_NODEJS_PATH"]) terminalEnv[name] = env[name];
    const child = spawn("timeout", ["--kill-after=3s", "15s", "script", "-qec",
      "stty cols 100 rows 30; exec /opt/cooper/bin/agy", "/dev/null"],
      { cwd: terminalHome, env: terminalEnv, stdio: ["pipe", "pipe", "pipe"] });
    let screen = "";
    let first, second;
    child.stdout.on("data", (data) => {
      screen += data;
      if (!first && screen.includes("Google OAuth")) {
        first = setTimeout(() => child.stdin.write("\x03"), 100);
        second = setTimeout(() => child.stdin.write("\x03"), 600);
      }
    });
    child.stderr.resume();
    const code = await new Promise((resolve, reject) => {
      child.on("error", reject);
      child.on("close", resolve);
    });
    clearTimeout(first);
    clearTimeout(second);
    assert.equal(code, 0, "native terminal did not exit normally");
    assert.ok(screen.includes("Google OAuth"), "native terminal did not reach login");
  } finally {
    fs.rmSync(terminalHome, { recursive: true });
  }
  console.log("Native request, saved conversation, and terminal exit passed (fabricated local model).");
} finally {
  await new Promise((resolve) => server.close(resolve));
  if (!shared) fs.rmSync(home, { recursive: true });
}
