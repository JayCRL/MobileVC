const wsUrl = "ws://127.0.0.1:8081/ws?token=test";
const cmd = "claude";

if (typeof WebSocket === "undefined") {
  console.error("当前 Node.js 没有内置 WebSocket。");
  process.exit(1);
}

const ws = new WebSocket(wsUrl);
let inputSent = false;
let count = 0;

ws.addEventListener("open", () => {
  console.log("OPEN", wsUrl);
  ws.send(JSON.stringify({ action: "exec", mode: "pty", cmd }));
});

ws.addEventListener("message", (evt) => {
  const raw = typeof evt.data === "string" ? evt.data : String(evt.data);
  console.log("EVT", raw);
  let event;
  try { event = JSON.parse(raw); } catch { return; }
  count += 1;
  if (!inputSent && event.type === "agent_state" && event.state === "WAIT_INPUT") {
    inputSent = true;
    ws.send(JSON.stringify({ action: "input", data: "你好\n" }));
    console.log("SENT_INPUT");
  }
  if (count > 30) {
    ws.close();
    process.exit(0);
  }
});

ws.addEventListener("close", () => {
  console.log("CLOSED");
  process.exit(0);
});

ws.addEventListener("error", (err) => {
  console.error("WS_ERROR", err);
  process.exit(2);
});

setTimeout(() => {
  console.error("TIMEOUT");
  try { ws.close(); } catch {}
  process.exit(3);
}, 30000);
