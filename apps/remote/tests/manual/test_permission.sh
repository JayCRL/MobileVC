#!/bin/bash

# 测试 Claude CLI 权限确认流程

echo "启动 Claude CLI..."
claude --model claude-opus-4-6 --print --verbose --output-format stream-json --input-format stream-json --permission-prompt-tool stdio > /tmp/claude_test_output.log 2>&1 &
CLAUDE_PID=$!

echo "Claude CLI PID: $CLAUDE_PID"
sleep 2

# 发送用户输入
echo "发送用户输入..."
echo '{"type":"user","message":"帮我把 test_permission.txt 文件中的 111666 删掉"}' | nc localhost 8001

# 等待权限请求
echo "等待权限请求..."
sleep 5

# 检查进程是否还在
if ps -p $CLAUDE_PID > /dev/null; then
    echo "✓ Claude CLI 进程还在运行"
else
    echo "✗ Claude CLI 进程已退出"
    exit 1
fi

echo "测试完成"
