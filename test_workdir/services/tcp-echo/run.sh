#!/bin/bash
# TCP echo server on port 9002 using bash + /dev/tcp
# 备用方案：使用 socat 或 nc，但 bash 内置 /dev/tcp 更通用

PORT=9002

# 使用 coproc 启动一个 nc 监听器，如果 nc 不可用，使用 python 兜底
if command -v nc &>/dev/null && nc -h 2>&1 | grep -q "listen"; then
    # nc 支持 -l (listen) 和 -k (keep-alive，多连接)
    # 注意：不同 nc 实现参数不同，busybox nc 使用 -ll -p
    if nc -l -p "$PORT" -k 2>/dev/null; then
        exit 0
    fi
fi

# Python 兜底实现 TCP echo
python3 -c "
import socketserver
import sys

PORT = $PORT

class Handler(socketserver.BaseRequestHandler):
    def handle(self):
        while True:
            try:
                data = self.request.recv(4096)
                if not data:
                    break
                self.request.sendall(data)
            except Exception:
                break

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

with Server(('0.0.0.0', PORT), Handler) as srv:
    srv.serve_forever()
"
