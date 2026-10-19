#!/usr/bin/env python3
"""Local-only service test; accesses example.com but never the cloud platform.
Usage: python3 scripts/smoke_test.py /path/to/node /path/to/xray
Requires curl. Starts test listeners on loopback, then stops all test processes.
"""
import http.server
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
import uuid


def wait_port(port, process):
    for _ in range(100):
        if process.poll() is not None:
            raise RuntimeError("test process exited before readiness")
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.2):
                return
        except OSError:
            time.sleep(0.05)
    raise RuntimeError("test port did not become ready")


def curl(target):
    return subprocess.run(["curl", "--silent", "--show-error", "--fail", "--max-time", "15",
                           "--noproxy", "", "--proxy", "socks5h://127.0.0.1:18081", target],
                          capture_output=True, text=True)


def main():
    node, xray = map(lambda p: str(Path(p).resolve()), sys.argv[1:3])
    credential = str(uuid.uuid4())
    path = "/local-smoke-test-1234"
    processes = []
    local = None
    with tempfile.TemporaryDirectory(prefix="muudd-smoke-") as temp:
        with open(Path(temp) / "test.log", "w+") as log:
            def launch(command, env=None):
                proc = subprocess.Popen(command, env=env, stdout=log, stderr=log)
                processes.append(proc)
                return proc
            def client_config(uid):
                return {"log": {"loglevel": "none"},
                        "inbounds": [{"listen": "127.0.0.1", "port": 18081, "protocol": "socks", "settings": {"auth": "noauth", "udp": False}}],
                        "outbounds": [{"protocol": "vless", "settings": {"vnext": [{"address": "127.0.0.1", "port": 18080,
                                       "users": [{"id": uid, "encryption": "none"}]}]},
                                       "streamSettings": {"network": "ws", "security": "none", "wsSettings": {"path": path}}}]}
            def start_client(uid):
                config = Path(temp) / "client.json"
                config.write_text(json.dumps(client_config(uid)))
                p = launch([xray, "run", "-config", str(config)])
                wait_port(18081, p)
                return p
            try:
                # Never expose the plaintext test listener publicly.
                # The launcher itself binds :PORT, so tests should run only in an isolated workspace.
                env = dict(os.environ, VLESS_UUID=credential, WS_PATH=path, PORT="18080", XRAY_BIN=xray)
                server = launch([node], env)
                wait_port(18080, server)
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                assert opener.open("http://127.0.0.1:18080/healthz").read().strip() == b"ok"
                with socket.create_connection(("127.0.0.1", 18080), timeout=3) as ws:
                    ws.sendall((f"GET {path} HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                                "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
                    assert b"101 Switching Protocols" in ws.recv(4096)
                print("PASS health and WebSocket 101 handshake")
                client = start_client(credential)
                result = curl("https://example.com/")
                assert result.returncode == 0 and "Example Domain" in result.stdout, result.stderr
                print("PASS VLESS -> WebSocket -> external HTTPS")
                local = http.server.ThreadingHTTPServer(("127.0.0.1", 18082), http.server.SimpleHTTPRequestHandler)
                threading.Thread(target=local.serve_forever, daemon=True).start()
                assert curl("http://127.0.0.1:18082/").returncode != 0
                print("PASS loopback target blocked")
                client.terminate(); client.wait(timeout=5)
                bad = start_client(str(uuid.uuid4()))
                assert curl("https://example.com/").returncode != 0
                print("PASS wrong UUID denied")
                bad.terminate(); bad.wait(timeout=5)
                # Confirm child death stops the front server instead of serving a dead node.
                child_pids = subprocess.check_output(["pgrep", "-P", str(server.pid)], text=True).split()
                assert len(child_pids) == 1
                os.kill(int(child_pids[0]), 15)
                server.wait(timeout=8)
                assert server.returncode != 0
                print("PASS Xray child failure propagates")
            except Exception:
                log.seek(0)
                print(log.read(), file=sys.stderr)
                raise
            finally:
                if local:
                    local.shutdown(); local.server_close()
                for process in reversed(processes):
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=8)
                        except subprocess.TimeoutExpired:
                            process.kill(); process.wait()


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)
    main()
