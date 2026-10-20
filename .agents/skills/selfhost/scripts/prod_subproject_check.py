"""Read-only check of include_subprojects on the deployed instance (token from Claude Code's config, never printed).

Usage: prod_subproject_check.py <parent project id>
"""
import json
import os
import sys
import urllib.request

BASE = "http://127.0.0.1:3456/api/v2"
PARENT = int(sys.argv[1])
auth = json.load(open(os.path.expanduser("~/.claude.json")))["mcpServers"]["vikunja"]["headers"]["Authorization"]


def get(path):
    req = urllib.request.Request(BASE + path, headers={"Authorization": auth})
    return json.load(urllib.request.urlopen(req, timeout=20))


def all_items(path):
    sep = "&" if "?" in path else "?"
    items, page = [], 1
    while True:
        res = get(f"{path}{sep}page={page}&per_page=50")
        items += res["items"]
        if page >= res.get("total_pages", 1):
            return items
        page += 1


projects = all_items("/projects?is_archived=false")
children = {p["id"]: p for p in projects}
desc, frontier = set(), {PARENT}
while frontier:
    frontier = {p["id"] for p in projects if p.get("parent_project_id") in frontier and not p.get("is_archived")}
    desc |= frontier
print("parent:", children[PARENT]["title"], "| non-archived descendants:", sorted(desc))

views = get(f"/projects/{PARENT}/views")["items"]
for kind in ("list", "table", "kanban"):
    view = next((v for v in views if v["view_kind"] == kind), None)
    if view is None:
        continue
    if kind == "kanban":
        path = f"/projects/{PARENT}/views/{view['id']}/buckets/tasks?include_subprojects=true"
        try:
            buckets = get(path)
            tasks = [t for b in (buckets.get("items") or buckets) for t in (b.get("tasks") or [])]
            print(f"kanban with flag: task projects = {sorted({t['project_id'] for t in tasks})} (expect only {PARENT})")
        except Exception as e:  # noqa: BLE001
            print("kanban check skipped:", e)
        continue
    own = all_items(f"/projects/{PARENT}/views/{view['id']}/tasks")
    agg = all_items(f"/projects/{PARENT}/views/{view['id']}/tasks?include_subprojects=true")
    by_project = {}
    for t in agg:
        by_project[t["project_id"]] = by_project.get(t["project_id"], 0) + 1
    print(f"{kind} view (filter {view.get('filter') and view['filter'].get('filter')!r}): own={len(own)} aggregate={len(agg)} by project={by_project}")
    print(f"  own projects only {PARENT}:", {t['project_id'] for t in own} <= {PARENT},
          "| aggregate within subtree:", set(by_project) <= desc | {PARENT})
