"""Smoke-test the deployed MCP upload + download flow with the token from Claude Code's config (never printed).

Usage: prod_dl_check.py <task id> <any local file to upload>
Uploads the file to the task, downloads it again through a one-time link and compares the bytes.
Delete the scratch task afterwards.
"""
import filecmp
import json
import os
import subprocess
import sys
import tempfile
import urllib.request

BASE = "http://127.0.0.1:3456"
TASK = int(sys.argv[1])
IMG = sys.argv[2]
auth = json.load(open(os.path.expanduser("~/.claude.json")))["mcpServers"]["vikunja"]["headers"]["Authorization"]


def mcp(method, params, rid):
    req = urllib.request.Request(
        BASE + "/api/v2/mcp", method="POST",
        data=json.dumps({"jsonrpc": "2.0", "id": rid, "method": method, "params": params}).encode(),
        headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                 "Authorization": auth})
    raw = urllib.request.urlopen(req, timeout=20).read().decode()
    for line in raw.splitlines():
        if line.startswith("data:"):
            return json.loads(line[5:])
    return json.loads(raw)


def payload(res):
    r = res["result"]
    if r.get("isError"):
        raise SystemExit(f"tool error: {r}")
    return r.get("structuredContent") or json.loads(r["content"][0]["text"])


def curl(*args):
    return subprocess.run(["curl", "-s", *args], capture_output=True, text=True).stdout


info = json.load(urllib.request.urlopen(BASE + "/api/v1/info", timeout=10))
print("version:", info["version"])
mcp("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                   "clientInfo": {"name": "prod-dl-check", "version": "0"}}, 1)
names = [t["name"] for t in mcp("tools/list", {}, 2)["result"]["tools"]]
print("upload tool listed:", "task_attachments_upload_link" in names)
print("download tool listed:", "task_attachments_download_link" in names, f"({len(names)} tools)")

up = payload(mcp("tools/call", {"name": "task_attachments_upload_link", "arguments": {"task": TASK}}, 3))
out = curl("-w", "\n%{http_code}", "-X", "POST", up["upload_url"],
           "-F", f"files=@{IMG};filename=prod-dl-check-{os.path.basename(IMG)}")
body, code = out.rsplit("\n", 1)
att = json.loads(body)["success"][0]
print("upload:", code, att["id"], att["file"]["name"])

dl = payload(mcp("tools/call", {"name": "task_attachments_download_link",
                                "arguments": {"task": TASK, "attachment": att["id"]}}, 4))
print("download link file:", dl["file"], "| curl_example starts:", dl["curl_example"].split("'")[0] + "'" + dl["curl_example"].split("'")[1] + "'")
with tempfile.TemporaryDirectory() as d:
    dest = os.path.join(d, "out")
    code = curl("-o", dest, "-w", "%{http_code}", dl["download_url"])
    print("download:", code, "bytes identical:", filecmp.cmp(dest, IMG, shallow=False))
again = curl("-o", "/dev/null", "-w", "%{http_code}", dl["download_url"])
print("reuse rejected:", again == "404", f"(HTTP {again})")
print("ticket in container log:", dl["download_url"].rsplit("/", 1)[-1] in
      subprocess.run(["docker", "logs", "vikunja"], capture_output=True, text=True).stderr +
      subprocess.run(["docker", "logs", "vikunja"], capture_output=True, text=True).stdout)
print("attachment id to clean up:", att["id"])
