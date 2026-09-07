#!/usr/bin/env python3
"""Regenerate self-contained documentation SVGs with Python's standard library."""
from html import escape
from pathlib import Path
import re

OUT = Path(__file__).resolve().parents[1]
ROOT = OUT.parents[1]


def text(x, y, value, size=20, fill="#f2f2f0", **attrs):
    extra = " ".join(f'{k.replace("_", "-")}="{escape(str(v), quote=True)}"' for k, v in attrs.items())
    return f'<text x="{x}" y="{y}" font-size="{size}" fill="{fill}" {extra}>{escape(value)}</text>'


def svg(name, title, description, width, height, body):
    content = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" width="{width}" height="{height}" role="img" aria-labelledby="title desc">
<title id="title">{escape(title)}</title><desc id="desc">{escape(description)}</desc>
<rect width="{width}" height="{height}" rx="18" fill="#0b0b0c"/>
<g font-family="Arial, Helvetica, sans-serif">{body}</g>
</svg>
'''
    (OUT / name).write_text(content)


# Reuse the actual product's path geometry, so documentation does not invent a
# second icon language. Fail if a stage's source shape is missing.
source = (ROOT / "frontend/src/components/pipeline/StageSymbol.tsx").read_text()
body = text(32, 42, "COOKER / STAGE TYPES", 15, "#e07a1f", letter_spacing=2)
body += text(32, 79, "Recognise the action before reading the label.", 25)
for i, kind in enumerate(["build", "test", "push", "deploy", "approval", "custom"]):
    pattern = r"default:\s*shape = <>(.*?)</>" if kind == "custom" else rf"case '{kind}':\s*shape = <>(.*?)</>"
    found = re.search(pattern, source, re.S)
    if not found:
        raise ValueError(f"Missing stage geometry: {kind}")
    shape = found.group(1)
    x = 32 + i * 156
    body += f'<rect x="{x}" y="105" width="140" height="122" rx="10" fill="#141416" stroke="#333336"/>'
    body += f'<g transform="translate({x+52} 125) scale(2)" fill="none" stroke="#e07a1f" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round">{shape}</g>'
    body += text(x+70, 204, kind.title(), 19, text_anchor="middle")
body += text(32, 266, "Shape identifies the stage. Colour stays available for run status.", 17, "#a3a39c")
svg("stage-types.svg", "Cooker stage types", "Build cube, Test flask, Push upload, Deploy rocket, Approval diamond with check, and Custom terminal. These are the application's actual stage symbols.", 984, 292, body)

steps = [
    ("Repository", "GitHub App or public", "Choose branch or tag"),
    ("Compose files", "Base + ordered overrides", "Profiles and inputs"),
    ("Preview", "Services + Dockerfiles", "Inspect before execution"),
    ("Destination", "Prefix, target, registry", "Optional database binding"),
    ("Review", "Check the pinned SHA", "Save, then deploy"),
]
body = text(32, 42, "COOKER / COMPOSE IMPORT", 15, "#e07a1f", letter_spacing=2)
body += text(32, 78, "One reviewed revision, from repository to runtime.", 25)
for i, (label, first, second) in enumerate(steps):
    x = 32 + i * 222
    body += f'<rect x="{x}" y="109" width="200" height="151" rx="10" fill="#141416" stroke="#333336"/>'
    body += text(x+16, 140, f"0{i+1}", 16, "#e07a1f")
    body += text(x+16, 178, label, 23)
    body += text(x+16, 213, first, 14, "#a3a39c")
    body += text(x+16, 238, second, 14, "#a3a39c")
    if i < 4:
        body += f'<path d="M{x+202} 183h16m-5-5 5 5-5 5" fill="none" stroke="#e07a1f" stroke-width="1.5"/>'
body += text(32, 304, "Preview is read-only. Deploy from the App page after saving the review.", 18, "#a3a39c")
svg("compose-workflow.svg", "Five-step GitHub Compose import", "Repository selection, ordered Compose files, read-only preview, destination and database binding, then saved review. Deployment is a separate action from the App page.", 1152, 336, body)

# A compact README composition of the same architecture specification. The HTML
# companion retains the Archify viewer; this SVG prioritises Markdown readability.
import json
spec = json.loads((OUT / "sources/github-compose.architecture.json").read_text())
positions = {
    "repository": (32, 112), "review": (432, 112), "build": (832, 112),
    "database": (32, 336), "ecs": (432, 336), "registry": (832, 336),
}
body = text(32, 42, "COOKER / REVIEWED DEPLOYMENT", 15, "#e07a1f", letter_spacing=2)
body += text(32, 78, "GitHub source. AWS compute. An existing GCP database.", 25)
body += '<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0 0 8 4 0 8" fill="#e07a1f"/></marker></defs>'
for node in spec["components"]:
    x, y = positions[node["id"]]
    body += f'<rect x="{x}" y="{y}" width="288" height="100" rx="10" fill="#141416" stroke="#4a4036"/>'
    body += text(x+20, y+29, node['type'].upper(), 12, "#e07a1f", letter_spacing=1.4)
    body += text(x+20, y+57, node['label'], 22)
    body += text(x+20, y+82, node['sublabel'], 16, "#a3a39c")
for edge in spec['connections']:
    ax, ay = positions[edge['from']]; bx, by = positions[edge['to']]
    if ax == bx:
        start, end = (ax+144, ay+100), (bx+144, by)
        lx, ly = ax+144, (ay+100+by)/2-7
    elif bx > ax:
        start, end = (ax+288, ay+50), (bx, by+50)
        lx, ly = (ax+288+bx)/2, ay+39
    else:
        start, end = (ax, ay+50), (bx+288, by+50)
        lx, ly = (ax+bx+288)/2, ay+39
    dash = ' stroke-dasharray="5 5"' if edge.get('variant') == 'dashed' else ''
    body += f'<path d="M{start[0]} {start[1]} L{end[0]} {end[1]}" stroke="#e07a1f" stroke-width="1.5" marker-end="url(#arrow)"{dash}/>'
    if ax == bx:
        body += f'<rect x="{lx-60}" y="{ly-16}" width="120" height="24" rx="4" fill="#0b0b0c"/>'
    body += text(lx, ly, edge['label'], 14, "#a3a39c", text_anchor="middle")
body += text(32, 480, "Cooker deploys the application. The operator provides infrastructure and database access.", 19)
body += text(32, 512, "Source builds: dev/UAT only. Live GitHub and AWS-to-GCP acceptance remain pending.", 17, "#a3a39c")
svg("github-compose.svg", spec['meta']['title'], "GitHub Compose source is inspected at a pinned SHA, saved and deployed through Docker build and an image registry to existing ECS Fargate infrastructure. The workload connects to existing GCP Cloud SQL using operator-provided access. This is an implementation illustration, not live cloud acceptance evidence.", 1152, 548, body)
