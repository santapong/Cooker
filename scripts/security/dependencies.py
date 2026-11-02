#!/usr/bin/env python3
"""Check exact Go/npm versions against OSV; API/coverage failures fail the gate."""

import argparse
import datetime
import json
import pathlib
import re
import sys
import urllib.request


def inventory(root):
    packages = {}

    def add(ecosystem, name, version, source):
        key = (ecosystem, name, version)
        packages.setdefault(key, {"package": {"ecosystem": ecosystem, "name": name},
                                  "version": version, "sources": []})["sources"].append(source)

    block = False
    for raw in (root / "backend/go.mod").read_text().splitlines():
        line = raw.split("//", 1)[0].strip()
        if line == "require (":
            block = True
            continue
        if block and line == ")":
            block = False
            continue
        if line.startswith("replace"):
            # Replacements need a resolved inventory rather than guessing a version.
            raise ValueError("go.mod replacements require a resolved dependency inventory")
        if line.startswith("require "):
            line = line[len("require "):]
        elif not block:
            continue
        if not line:
            continue
        fields = line.split()
        if len(fields) != 2 or not re.fullmatch(r"v\d+\.\d+\.\d+(?:[-+].+)?", fields[1]):
            raise ValueError("Unsupported Go requirement: " + line)
        add("Go", fields[0], fields[1], "backend/go.mod")

    lock = json.loads((root / "frontend/package-lock.json").read_text())
    if lock.get("lockfileVersion") not in (2, 3) or not lock.get("packages"):
        raise ValueError("Expected an npm v2/v3 package lock with packages")
    for path, package in lock["packages"].items():
        if not path:
            continue
        version = package.get("version", "")
        if package.get("link") or not re.fullmatch(r"\d+\.\d+\.\d+(?:[-+].+)?", version):
            raise ValueError("Unsupported npm package version at " + path)
        name = package.get("name") or path.rsplit("node_modules/", 1)[-1]
        add("npm", name, version, "frontend/package-lock.json:" + path)
    if not packages or len(packages) > 1000:
        raise ValueError("Dependency inventory is empty or exceeds the 1000-package request budget")
    return list(packages.values())


def post(queries):
    request = urllib.request.Request(
        "https://api.osv.dev/v1/querybatch",
        data=json.dumps({"queries": queries}).encode(),
        headers={"Content-Type": "application/json", "User-Agent": "Cooker-security-check"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        data = response.read(4 * 1024 * 1024 + 1)
    if len(data) > 4 * 1024 * 1024:
        raise ValueError("OSV response exceeded the response budget")
    return json.loads(data)


def lookup(packages, fetch=post):
    pending = [(i, {k: v for k, v in package.items() if k != "sources"})
               for i, package in enumerate(packages)]
    matches = [dict() for _ in packages]
    requests = 0
    while pending:
        requests += 1
        if requests > 6:
            raise ValueError("OSV pagination exceeded the request budget; scan incomplete")
        response = fetch([query for _, query in pending])
        results = response.get("results")
        if not isinstance(results, list) or len(results) != len(pending):
            raise ValueError("OSV response does not cover the complete request")
        more = []
        for (index, query), result in zip(pending, results):
            if not isinstance(result, dict) or result.get("error"):
                raise ValueError("OSV returned an invalid package result")
            for vuln in result.get("vulns", []):
                if not isinstance(vuln, dict) or not vuln.get("id"):
                    raise ValueError("OSV returned an invalid advisory")
                matches[index][vuln["id"]] = vuln
            if result.get("next_page_token"):
                more.append((index, {**query, "page_token": result["next_page_token"]}))
        pending = more
    return [{**package, "advisories": list(found.values())}
            for package, found in zip(packages, matches) if found], requests


def detail(advisory_id):
    with urllib.request.urlopen("https://api.osv.dev/v1/vulns/" + advisory_id, timeout=15) as response:
        data = response.read(1024 * 1024 + 1)
    if len(data) > 1024 * 1024:
        raise ValueError("OSV advisory exceeded the response budget")
    return json.loads(data)


def check_go_imports(findings, imported, fetch=detail):
    """Exclude only advisories whose explicitly affected packages are all absent."""
    active, absent, cache = [], [], {}
    for finding in findings:
        remaining = []
        for advisory in finding["advisories"]:
            if finding["package"]["ecosystem"] != "Go":
                remaining.append(advisory)
                continue
            identifier = advisory["id"]
            if identifier not in cache:
                if len(cache) >= 20:
                    raise ValueError("Advisory applicability request budget exceeded")
                cache[identifier] = fetch(identifier)
            affected = [a for a in cache[identifier].get("affected", [])
                        if a.get("package") == finding["package"] or
                        (a.get("package", {}).get("ecosystem") == "Go" and
                         a.get("package", {}).get("name") == finding["package"]["name"])]
            paths = {p["path"] for a in affected
                     for p in (a.get("ecosystem_specific") or {}).get("imports", []) if p.get("path")}
            if paths and not any(p == i or i.startswith(p + "/") for p in paths for i in imported):
                absent.append({"package": finding["package"], "version": finding["version"],
                               "advisory": advisory, "affected_imports": sorted(paths),
                               "reason": "Affected packages absent from this default build/test import inventory"})
            else:
                remaining.append(advisory)
        if remaining:
            active.append({**finding, "advisories": remaining})
    return active, absent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=pathlib.Path, default=pathlib.Path("."))
    parser.add_argument("--output", type=pathlib.Path, default=pathlib.Path("security-results/dependencies.json"))
    parser.add_argument("--go-imports", type=pathlib.Path,
                        help="Fresh go list -deps -test -f '{{.ImportPath}}' ./... output from backend")
    args = parser.parse_args()
    report = {"checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "method": "OSV exact Go requirements and npm lockfile versions, including dev dependencies",
              "limits": "No built-image OS inventory or symbol reachability; advisory IDs may be aliases"}
    try:
        packages = inventory(args.root)
        findings, requests = lookup(packages)
        absent = []
        if args.go_imports:
            imported = {line.split()[0] for line in args.go_imports.read_text().splitlines() if line.strip()}
            if not imported:
                raise ValueError("Go import inventory is empty")
            findings, absent = check_go_imports(findings, imported)
        report.update(status="findings" if findings else "complete", packages_checked=len(packages),
                      requests=requests, findings=findings, not_imported=absent)
        status = 1 if findings else 0
        print(f"OSV checked {len(packages)} package/version pairs; {len(findings)} package/version entries with advisories")
        for finding in findings:
            package = finding["package"]
            print(f"{package['ecosystem']} {package['name']}@{finding['version']}: "
                  + ", ".join(v["id"] for v in finding["advisories"]))
    except Exception as error:
        report.update(status="incomplete", error=str(error))
        print("Dependency scan incomplete: " + str(error), file=sys.stderr)
        status = 2
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    return status


if __name__ == "__main__":
    sys.exit(main())
