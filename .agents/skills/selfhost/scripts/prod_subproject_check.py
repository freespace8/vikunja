"""Read-only check of include_subprojects on the deployed instance (token from Claude Code's config, never printed).

Usage: prod_subproject_check.py <parent project id>

List and table: the aggregate holds the subtree only.
Kanban: the parent's board with the flag must hold, per parent bucket, its own count plus the counts of the
child buckets that map to it (done role, default role, same title, else the parent's default bucket).
The counts come from the buckets' `count`, not from the first page of tasks. A view filter on a child's board
is not applied to the child's task total, so a mismatch there is worth a look, not necessarily a bug.
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


def home_view(project_id):
    """The first manual kanban view by position, the one a task's bucket is read from."""
    views = get(f"/projects/{project_id}/views")["items"]
    manual = [v for v in views if v["view_kind"] == "kanban" and v.get("bucket_configuration_mode") == "manual"]
    return min(manual, key=lambda v: (v.get("position", 0), v["id"]), default=None)


def board(project_id, view_id, flag=False):
    res = get(f"/projects/{project_id}/views/{view_id}/buckets/tasks" + ("?include_subprojects=true" if flag else ""))
    return res.get("items") or []


def default_bucket(view, buckets):
    return view.get("default_bucket_id") or min(buckets, key=lambda b: (b.get("position", 0), b["id"]))["id"]


def parent_bucket_for(child_view, child_buckets, child_bucket, parent_view, parent_buckets):
    if parent_view.get("done_bucket_id") and child_bucket["id"] == child_view.get("done_bucket_id"):
        return parent_view["done_bucket_id"]
    if child_bucket["id"] == default_bucket(child_view, child_buckets):
        return default_bucket(parent_view, parent_buckets)
    title = child_bucket["title"].strip().casefold()
    for bucket in sorted(parent_buckets, key=lambda b: (b.get("position", 0), b["id"])):
        if bucket["title"].strip().casefold() == title:
            return bucket["id"]
    return default_bucket(parent_view, parent_buckets)


def check_kanban(view, descendants):
    parent_view = home_view(PARENT)
    if parent_view is None:
        print("kanban: the parent has no manual kanban view, skipped")
        return
    plain = board(PARENT, parent_view["id"])
    flagged = board(PARENT, parent_view["id"], flag=True)
    projects_without = {t["project_id"] for b in plain for t in b.get("tasks") or []}
    print(f"kanban without flag: task projects = {sorted(projects_without)} (expect only {PARENT}):",
          projects_without <= {PARENT})
    flagged_tasks = [t for b in flagged for t in b.get("tasks") or []]
    projects_with = {t["project_id"] for t in flagged_tasks}
    ids = [t["id"] for t in flagged_tasks]
    print(f"kanban with flag: task projects = {sorted(projects_with)} within subtree:",
          projects_with <= descendants | {PARENT}, "| no task in two buckets:", len(ids) == len(set(ids)))

    own = {b["id"]: b["count"] for b in plain}
    expected = dict(own)
    parent_buckets = [{k: b.get(k) for k in ("id", "title", "position")} for b in plain]
    parent_default = default_bucket(parent_view, parent_buckets)
    for child in sorted(descendants):
        child_view = home_view(child)
        if child_view is None:
            total = len(all_items(f"/projects/{child}/tasks"))
            expected[parent_default] += total
            print(f"  project {child}: no manual kanban view, {total} task(s) -> default bucket")
            continue
        child_board = board(child, child_view["id"])
        child_buckets = [{k: b.get(k) for k in ("id", "title", "position")} for b in child_board]
        for bucket in child_board:
            target = parent_bucket_for(child_view, child_buckets, bucket, parent_view, parent_buckets)
            expected[target] += bucket["count"]
        in_buckets = sum(b["count"] for b in child_board)
        missing = len(all_items(f"/projects/{child}/tasks")) - in_buckets
        if missing > 0:
            expected[parent_default] += missing
            print(f"  project {child}: {missing} task(s) without a row in its home view -> default bucket")

    for bucket in flagged:
        limit = bucket.get("limit") or 0
        ok = bucket["count"] == expected.get(bucket["id"])
        print(f"  bucket {bucket['title']!r}: count={bucket['count']} expected={expected.get(bucket['id'])}"
              f" (own {own.get(bucket['id'])}) limit={limit} {'OK' if ok else 'MISMATCH'}")


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
        try:
            check_kanban(view, desc)
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
