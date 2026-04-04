package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	host := flag.String("host", "127.0.0.1:8001", "ws host:port")
	token := flag.String("token", "123", "auth token")
	sessionID := flag.String("session", "codex-thread:019d53d8-f497-79a2-a80e-bd28f2cedca2", "session id")
	cwd := flag.String("cwd", "/Users/wust_lh/MobileVC", "cwd")
	input := flag.String("input", "你好", "input text")
	timeout := flag.Duration("timeout", 20*time.Second, "read timeout")
	flag.Parse()

	u := url.URL{
		Scheme:   "ws",
		Host:     *host,
		Path:     "/ws",
		RawQuery: "token=" + url.QueryEscape(*token),
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial websocket: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	deadline := time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			fmt.Fprintf(os.Stderr, "bootstrap read: %v\n", err)
			os.Exit(1)
		}
		printEvent("bootstrap", event)
		if strings.TrimSpace(asString(event["type"])) == "session_list_result" {
			break
		}
	}

	if err := conn.WriteJSON(map[string]any{
		"action":    "session_load",
		"sessionId": *sessionID,
		"cwd":       *cwd,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "write session_load: %v\n", err)
		os.Exit(1)
	}

	deadline = time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	loaded := false
	for !loaded {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			fmt.Fprintf(os.Stderr, "load read: %v\n", err)
			os.Exit(1)
		}
		printEvent("load", event)
		loaded = strings.TrimSpace(asString(event["type"])) == "session_history"
	}

	if err := conn.WriteJSON(map[string]any{
		"action":         "input",
		"data":           *input,
		"permissionMode": "default",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "write input: %v\n", err)
		os.Exit(1)
	}

	deadline = time.Now().Add(*timeout)
	_ = conn.SetReadDeadline(deadline)
	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			fmt.Fprintf(os.Stderr, "input read: %v\n", err)
			os.Exit(1)
		}
		printEvent("input", event)
	}
}

func printEvent(stage string, event map[string]any) {
	encoded, _ := json.Marshal(event)
	fmt.Printf("[%s] %s\n", stage, string(encoded))
}

func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
