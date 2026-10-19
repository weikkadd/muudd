#!/usr/bin/env python3
"""Create a private VLESS import URI locally; no network requests."""
import getpass
import re
import sys
import uuid
from urllib.parse import quote, urlencode


def build_link(host: str, credential: str, path: str) -> str:
    if len(host) > 253 or not re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?", host):
        raise ValueError("请输入纯域名，不包含协议、端口或路径")
    if "." not in host or any(not label or len(label) > 63 or label.startswith("-") or label.endswith("-") for label in host.split(".")):
        raise ValueError("域名格式无效")
    normalized = str(uuid.UUID(credential))
    if normalized != credential.lower() or uuid.UUID(credential).int == 0:
        raise ValueError("需要非零的标准 UUID")
    if not re.fullmatch(r"/[A-Za-z0-9_-]{12,128}", path) or path == "/healthz":
        raise ValueError("WS 路径格式无效")
    query = urlencode({"encryption": "none", "security": "tls", "type": "ws", "sni": host, "host": host, "path": path})
    return f"vless://{normalized}@{host}:443?{query}#{quote('muudd-InstaCloud')}"


if __name__ == "__main__":
    try:
        host = input("平台域名（不含 https://）：").strip()
        credential = getpass.getpass("UUID（隐藏输入）：").strip()
        path = input("WS 路径：").strip()
        print("\n以下链接包含凭据，请勿公开：")
        print(build_link(host, credential, path))
    except (ValueError, EOFError, KeyboardInterrupt) as exc:
        print(f"未生成链接：{exc}", file=sys.stderr)
        sys.exit(1)
