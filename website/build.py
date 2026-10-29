#!/usr/bin/env python3
"""Package Cooker's static pages and the pinned optional OGL renderer."""
from hashlib import sha256
from pathlib import Path
import json
import shutil

ROOT = Path(__file__).resolve().parent
OUTPUT = ROOT / 'dist'
PUBLIC_FILES = (
    'index.html',
    'vercel.json',
    'styles.css',
    'main.js',
    'kitchen.css',
    'kitchen-page.js',
    'kitchen-motion.js',
    'login.html',
    'login.css',
    'login.js',
    'login-ideas.html',
    'docs.html',
    'docs.css',
    'docs.js',
    'workspace.html',
    'workspace.css',
    'workspace.js',
    'workspace-dag.js',
    'workspace-examples.js',
    'workspace-dag.css',
    'workspace-n8n.js',
    'workspace-n8n.css',
    'examples/n8n-queue/compose.yaml',
    'examples/n8n-queue/env.example',
    'examples/n8n-queue/README.md',
    'assets/login-paper.jpg',
    'assets/login-workshop.jpg',
    'assets/login-console.jpg',
    'assets/login-workshop-art.png',
    'assets/login-split.jpg',
    'assets/login-focused.jpg',
    'assets/cooker-open-workbench-v1.png',
    'assets/cooker-b3.svg',
    'assets/cooker-kitchen-paths-v2.png',
    'assets/cooker-recipe-desk.png',
    'assets/cooker-service-pass.png',
    'assets/favicon.svg',
    'assets/tokens.css',
    'assets/cooker-theme.css',
)

# Exact vendored import-closure manifest, not a recursive public-directory copy.
VENDOR_ROOT = ROOT / 'vendor' / 'ogl'
VENDOR_HASHES = json.loads((VENDOR_ROOT / 'manifest.json').read_text())
VENDOR_FILES = tuple('vendor/ogl/' + name for name in sorted(VENDOR_HASHES)) + (
    'vendor/ogl/LICENSE', 'vendor/ogl/VERSION', 'vendor/ogl/manifest.json',
)


def main():
    # An explicit allowlist keeps design notes, scripts, and repository files private.
    for name, expected in VENDOR_HASHES.items():
        path = (VENDOR_ROOT / name).resolve()
        if not path.is_relative_to(VENDOR_ROOT.resolve()) or not path.is_file():
            raise SystemExit(f'Invalid OGL manifest path: {name}')
        if sha256(path.read_bytes()).hexdigest() != expected:
            raise SystemExit(f'OGL source differs from the pinned copy: {name}')
    names = PUBLIC_FILES + VENDOR_FILES
    sources = [ROOT / name for name in names]
    for source in sources:
        if not source.is_file():
            raise SystemExit(f'Missing public asset: {source.relative_to(ROOT)}')
    manifest = {}
    for name, source in zip(names, sources):
        destination = OUTPUT / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
        manifest[name] = {'bytes': source.stat().st_size, 'sha256': sha256(source.read_bytes()).hexdigest()}
    (OUTPUT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(f'Packaged {len(names)} files ({sum(v["bytes"] for v in manifest.values()):,} bytes) in {OUTPUT}')


if __name__ == '__main__':
    main()
