"""Export an explicit public site; exclude disconnected login studies and private files."""
from pathlib import Path
from hashlib import sha256
import json
import shutil

from build import ROOT, VENDOR_FILES, VENDOR_HASHES, VENDOR_ROOT

OUTPUT = ROOT / 'public-dist'
FILES = (
    'index.html', 'styles.css', 'kitchen.css', 'kitchen-page.js', 'kitchen-motion.js',
    'docs.html', 'docs.css', 'docs.js',
    'workspace.html', 'workspace.css', 'workspace.js', 'workspace-dag.js',
    'workspace-examples.js', 'workspace-dag.css', 'workspace-n8n.js', 'workspace-n8n.css',
    'examples/n8n-queue/compose.yaml', 'examples/n8n-queue/env.example', 'examples/n8n-queue/README.md',
    'assets/cooker-b3.svg', 'assets/favicon.svg', 'assets/cooker-theme.css', 'assets/tokens.css',
    'assets/cooker-kitchen-paths-v2.png', 'assets/cooker-recipe-desk.png', 'assets/cooker-service-pass.png',
)

def main():
    for name, expected in VENDOR_HASHES.items():
        path = (VENDOR_ROOT / name).resolve()
        if not path.is_relative_to(VENDOR_ROOT.resolve()) or sha256(path.read_bytes()).hexdigest() != expected:
            raise SystemExit('OGL input does not match its pinned manifest')
    names = FILES + VENDOR_FILES
    for name in names:
        if not (ROOT / name).is_file(): raise SystemExit(f'Missing public input: {name}')
    # Remove only the exporter's dedicated output so retired files cannot linger.
    if OUTPUT.exists(): shutil.rmtree(OUTPUT)
    manifest = {}
    for name in names:
        data = (ROOT / name).read_bytes()
        if name == 'index.html':
            text = data.decode().replace('href="login.html">Login preview ↗','href="docs.html#quickstart">Get started ↗')
            data = text.encode()
        elif name == 'docs.html':
            data = data.decode().replace('href="login-ideas.html">Explore the design ideas ↗','href="docs.html#overview">Back to start ↗').encode()
        target = OUTPUT / name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
        manifest[name] = {'bytes':len(data), 'sha256':sha256(data).hexdigest()}
    # Publish policy metadata without build scripts, private paths or credentials.
    config = json.loads((ROOT / 'vercel.json').read_text())
    for key in ['buildCommand', 'installCommand', 'outputDirectory']: config.pop(key, None)
    (OUTPUT / 'vercel.json').write_text(json.dumps(config,indent=2)+'\n')
    data=(OUTPUT/'vercel.json').read_bytes();manifest['vercel.json']={'bytes':len(data),'sha256':sha256(data).hexdigest()}
    total=sum(v['bytes'] for v in manifest.values())
    if len(manifest)>100 or total>8*1024*1024 or any(v['bytes']>4*1024*1024 for v in manifest.values()):
        raise SystemExit('Public artifact exceeds the documented static-publisher envelope')
    digest=sha256()
    for name,item in sorted(manifest.items()): digest.update(name.encode()+b'\0'+bytes.fromhex(item['sha256']))
    (OUTPUT/'manifest.json').write_text(json.dumps({'files':manifest,'totalBytes':total,'artifactSHA256':digest.hexdigest()},indent=2)+'\n')
    print(json.dumps({'files':len(manifest),'bytes':total,'artifactSHA256':digest.hexdigest(),'output':str(OUTPUT)}))

if __name__ == '__main__': main()
