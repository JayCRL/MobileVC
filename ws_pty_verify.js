const wsUrl = "ws://127.0.0.1:8080/ws?token=test";
const cmd = "printf \"Enter your name: \"; read name; printf \"Hello, %s\\n\" \"$name\"";

if (typeof WebSocket === "undefined") {
  console.error("当前 Node.js 没有内置 WebSocket。请使用较新的 Node.js 版本运行此脚本。");
  process.exit(1);
}

const ws = new WebSocket(wsUrl);
let inputSent = false;
let sawHello = false;

function sendExec() {
  const payload = { action: "exec", mode: "pty", cmd };
  console.log(">>> SEND", JSON.stringify(payload));
  ws.send(JSON.stringify(payload));
}

function sendInput() {
  if (inputSent) return;
  inputSent = true;
  const payload = { action: "input", data: "VibeCoder\n" };
  console.log(">>> SEND", JSON.stringify(payload));
  ws.send(JSON.stringify(payload));
}

function shouldAutoInput(event) {
  if (!event || typeof event !== "object") return false;
  if (event.type === "prompt_request") return true;
  if (event.type === "log" && typeof event.msg === "string") {
    const text = event.msg.toLowerCase();
    return text.includes("enter your name") || text.includes("name:") || text.endsWith(":") || text.endsWith(": ");
  }
  return false;
}

ws.addEventListener("open", () => {
  console.log("WebSocket 已连接:", wsUrl);
  sendExec();
});

ws.addEventListener("message", (evt) => {
  const raw = typeof evt.data === "string" ? evt.data : String(evt.data);
  console.log("<<< RECV", raw);

  let event;
  try {
    event = JSON.parse(raw);
  } catch {
    return;
  }

  if (shouldAutoInput(event)) {
    sendInput();
  }

  if (event.type === "log" && typeof event.msg === "string" && event.msg.includes("Hello, VibeCoder")) {
    sawHello = true;
    console.log("✅ 已收到目标输出: Hello, VibeCoder");
  }

  if (event.type === "error") {
    console.error("❌ 收到错误事件:", event.msg || event);
  }

  if (event.type === "session_state" && event.state === "closed") {
    console.log("会话结束:", event.msg || "");
    process.exit(sawHello ? 0 : 2);
  }
});

ws.addEventListener("error", (err) => {
  console.error("WebSocket 错误:", err);
  process.exit(4);
});

setTimeout(() => {
  if (!sawHello) {
    console.error("❌ 超时：未在预期时间内收到 Hello, VibeCoder");
    try {
      ws.close();
    } catch {}
    process.exit(3);
  }
}, 30000);
