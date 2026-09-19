// Local model fixture. The child receives only a disposable home and no host
// credentials. A prior assistant answer proves that native resume read state.
import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import { spawn } from "node:child_process";

const [binary, home, thread] = process.argv.slice(2);
const marker = "COOPER_PROFILE_OK";
const requests = [];
const server = http.createServer((request, response) => {
  const chunks = [];
  let size = 0;
  request.on("data", chunk => {
    size += chunk.length;
    if (size > 4 * 1024 * 1024) request.destroy();
    else chunks.push(chunk);
  });
  request.on("end", () => {
    requests.push(JSON.parse(Buffer.concat(chunks).toString()));
    const item = { id: "msg_fixture", type: "message", role: "assistant", status: "completed",
      content: [{ type: "output_text", text: marker, annotations: [] }] };
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    const events = [
      { type: "response.created", response: { id: "resp_fixture", status: "in_progress", output: [] } },
      { type: "response.output_item.done", output_index: 0, item },
      { type: "response.completed", response: { id: "resp_fixture", status: "completed", output: [item],
        usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 } } },
    ];
    for (const event of events) response.write(`event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
    response.end();
  });
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
try {
  const settings = ["model_provider=\"fixture\"", "model_providers.fixture.name=\"Fixture\"",
    `model_providers.fixture.base_url="http://127.0.0.1:${server.address().port}/v1"`,
    "model_providers.fixture.wire_api=\"responses\"", "model_providers.fixture.requires_openai_auth=false",
    "model_providers.fixture.supports_websockets=false", "model=\"gpt-5\""];
  const args = ["--foreground", "--kill-after=2s", "30s", binary, "exec",
    ...(thread ? ["resume", thread] : []), "--skip-git-repo-check",
    "--dangerously-bypass-approvals-and-sandbox", "--json", ...settings.flatMap(value => ["-c", value]),
    "Reply with " + marker];
  const child = spawn("timeout", args, { cwd: home, stdio: ["ignore", "pipe", "pipe"],
    env: { PATH: process.env.PATH, HOME: home, CODEX_HOME: path.join(home, ".codex") } });
  let output = "", errors = "";
  child.stdout.on("data", data => output += data);
  child.stderr.on("data", data => errors += data);
  const status = await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("close", resolve);
  });
  assert.equal(status, 0, errors);
  assert.ok(output.includes(marker), output);
  const events = output.trim().split("\n").map(line => JSON.parse(line));
  const id = events.find(event => event.type === "thread.started").thread_id;
  const priorAnswer = requests.some(request => request.input?.some(item => item.role === "assistant"));
  if (thread) {
    assert.equal(id, thread);
    assert.ok(priorAnswer, "native resume lost the prior answer");
  }
  process.stdout.write(JSON.stringify({ thread: id, priorAnswer }) + "\n");
} finally {
  server.close();
}
