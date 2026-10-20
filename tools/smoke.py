"""Run a compiled binary and verify its embedded UI and offline TCP/UDP pipeline."""
import argparse
import base64
import json
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    version = subprocess.check_output([str(binary), "--version"], text=True).strip()
    assert version.startswith("proto-watch "), version
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    process = subprocess.Popen(
        [str(binary), "--listen", f"127.0.0.1:{port}", "--pcap-dir", "frontend/public", "--read", "frontend/public/tcp-demo.pcap"],
        cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )

    def get(path):
        with urllib.request.urlopen(base + path, timeout=5) as response:
            assert response.status == 200
            return response.read()

    try:
        deadline = time.monotonic() + 30
        while True:
            if process.poll() is not None:
                raise RuntimeError("server exited before becoming ready")
            try:
                state = json.loads(get("/api/state"))
                if state["status"] == "complete":
                    break
                if state["status"] == "error":
                    raise RuntimeError(state.get("error"))
            except (urllib.error.URLError, TimeoutError):
                pass
            if time.monotonic() >= deadline:
                raise TimeoutError("server did not finish the fixture in 30 seconds")
            time.sleep(0.1)
        assert state["packets"] == 86 and state["someip"]["tcp_messages"] == 4 and state["someip"]["udp_messages"] == 1
        html = get("/").decode()
        assert "ProtoWatch" in html
        assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
        assert assets, "frontend bundle missing"
        for asset in assets:
            assert get(asset), asset
        capabilities = json.loads(get("/api/interfaces"))
        assert capabilities["live_supported"] == sys.platform.startswith("linux")
        assert capabilities["version"] in version
        catalog = json.loads(get("/api/files"))
        assert any(item["name"] == "tcp-demo.pcap" for item in catalog["files"])
        detail = json.loads(get("/api/someip/samples/4"))
        assert detail["sample"]["size"] == 102400 and detail["sample"]["transport"] == "TCP"
        assert base64.b64decode(detail["payload_base64"]) == bytes(range(256)) * 64
        export = json.loads(get("/api/export"))
        assert sorted(row["sample"]["transport"] for row in export["someip_retained_samples"]) == ["TCP"] * 4 + ["UDP"]
        print(f"Smoke passed: {version}; embedded UI, local PCAP, 4 TCP + 1 UDP messages and export", flush=True)
    finally:
        if process.poll() is None:
            if sys.platform == "win32":
                process.terminate()
            else:
                process.send_signal(signal.SIGINT)
        try:
            output, _ = process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            output, _ = process.communicate()
        print(output.strip())


if __name__ == "__main__":
    main()
