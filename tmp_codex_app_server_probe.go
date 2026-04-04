package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type rpcMessage struct {
	JSONRPC string      `json:"jsonrpc,omitempty"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
}

func main() {
	cmd := exec.Command("codex", "app-server", "--listen", "stdio://")
	cmd.Env = append(os.Environ(),
		"FORCE_COLOR=1",
		"CLICOLOR_FORCE=1",
		"TERM=xterm-256color",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	go stream("stdout", stdout)
	go stream("stderr", stderr)

	mustWrite(stdin, rpcMessage{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"clientInfo": map[string]any{
				"name":    "MobileVC",
				"version": "dev",
			},
			"capabilities": nil,
		},
	})
	mustWrite(stdin, rpcMessage{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "thread/resume",
		Params: map[string]any{
			"threadId": "019d53d8-f497-79a2-a80e-bd28f2cedca2",
		},
	})
	mustWrite(stdin, rpcMessage{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "turn/start",
		Params: map[string]any{
			"threadId": "019d53d8-f497-79a2-a80e-bd28f2cedca2",
			"input": []map[string]any{{
				"type":          "text",
				"text":          "probe from direct app-server",
				"text_elements": []any{},
			}},
			"approvalPolicy": "on-request",
			"cwd":            "/Users/wust_lh/MobileVC",
		},
	})

	select {
	case err := <-done:
		fmt.Printf("process_exit err=%v\n", err)
	case <-time.After(5 * time.Second):
		fmt.Println("process_alive_after_5s=true")
		_ = cmd.Process.Kill()
		<-done
	}
}

func mustWrite(stdin interface{ Write([]byte) (int, error) }, message rpcMessage) {
	payload, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	payload = append(payload, '\n')
	if _, err := stdin.Write(payload); err != nil {
		panic(err)
	}
}

func stream(name string, file interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err == nil {
			if id, ok := payload["id"]; ok {
				if _, hasError := payload["error"]; hasError {
					fmt.Printf("%s rpc id=%v error=%v\n", name, id, payload["error"])
					continue
				}
				if result, hasResult := payload["result"]; hasResult {
					fmt.Printf("%s rpc id=%v result_type=%T\n", name, id, result)
					continue
				}
			}
			if method, ok := payload["method"].(string); ok {
				fmt.Printf("%s notify method=%s\n", name, method)
				continue
			}
		}
		fmt.Printf("%s %s\n", name, line)
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("%s_err %v\n", name, err)
	}
}
