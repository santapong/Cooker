"""Disposable Cooker acceptance fixture. Never use an existing n8n instance."""
import concurrent.futures
import http.cookiejar
import json
import re
import secrets
import subprocess
import sys
import time
import urllib.request

base, project = sys.argv[1:]
assert re.fullmatch(r"http://127\.0\.0\.1:\d+", base)
assert re.fullmatch(r"cooker-e2e-n8n-[a-z0-9]{8}", project)
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def api(path, data):
    request = urllib.request.Request(base + path, data=json.dumps(data).encode(), headers={"Content-Type": "application/json"})
    with client.open(request, timeout=30) as response:
        return json.load(response)


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, stderr=subprocess.DEVNULL).strip()


api("/rest/owner/setup", {"email": "cooker-fixture@example.invalid", "firstName": "Cooker", "lastName": "Fixture", "password": "Q9!" + secrets.token_hex(20)})
path = "cooker-acceptance-" + secrets.token_hex(6)
workflow = {
    "name": "Cooker disposable queue acceptance",
    "nodes": [
        {"id": "webhook", "name": "Webhook", "type": "n8n-nodes-base.webhook", "typeVersion": 2, "position": [240, 280], "webhookId": path, "parameters": {"path": path, "httpMethod": "GET", "responseMode": "responseNode", "options": {}}},
        {"id": "response", "name": "Respond", "type": "n8n-nodes-base.respondToWebhook", "typeVersion": 1.4, "position": [480, 280], "parameters": {"respondWith": "json", "responseBody": "={{ { ok: true, marker: $json.query.marker, executionId: $execution.id } }}", "options": {}}},
    ],
    "connections": {"Webhook": {"main": [[{"node": "Respond", "type": "main", "index": 0}]]}},
    "settings": {"executionOrder": "v1", "saveDataSuccessExecution": "all", "saveDataErrorExecution": "all", "timezone": "Asia/Bangkok"},
}
created = api("/rest/workflows", workflow)["data"]
api("/rest/workflows/" + created["id"] + "/activate", {"versionId": created["versionId"]})
worker = project + "-n8n-worker-1"
redis = project + "-redis-1"
postgres = project + "-postgres-1"
docker("stop", worker)
marker = secrets.token_hex(10)


def webhook():
    with urllib.request.urlopen(base + "/webhook/" + path + "?marker=" + marker, timeout=70) as response:
        return json.load(response)


with concurrent.futures.ThreadPoolExecutor() as pool:
    pending = pool.submit(webhook)
    queued = ""
    for _ in range(40):
        queued = docker("exec", redis, "redis-cli", "LRANGE", "bull:jobs:wait", "0", "-1")
        if queued:
            break
        time.sleep(0.25)
    assert queued and not pending.done(), "expected a waiting Redis job with the sole worker stopped"
    docker("start", worker)
    response = pending.result(timeout=60)
assert response.get("ok") is True and response.get("marker") == marker
execution = int(response["executionId"])
status = ""
for _ in range(40):
    status = docker("exec", postgres, "psql", "-U", "n8n", "-d", "n8n", "-Atc", "SELECT status FROM execution_entity WHERE id=" + str(execution))
    if status == "success":
        break
    time.sleep(0.25)
assert status == "success", "n8n did not persist successful execution"
logs = docker("logs", worker)
assert any(("job" in line.lower() or "execution" in line.lower()) and ("finish" in line.lower() or "complet" in line.lower()) for line in logs.splitlines()), "worker completion log absent"
print(json.dumps({"queuedWithoutWorker": True, "completedAfterWorkerStart": True, "databaseStatus": status, "executionId": execution, "workerLogEvidence": True}))
