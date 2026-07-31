#!/usr/bin/env python3
import http.server
import socketserver
import sys

PORT = 9001

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
        else:
            self.send_response(200)
            self.send_header('Content-Type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'web-demo running\n')

    def do_POST(self):
        self.do_GET()

    def log_message(self, fmt, *args):
        sys.stderr.write(fmt % args + '\n')

socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("0.0.0.0", PORT), Handler) as httpd:
    httpd.serve_forever()
