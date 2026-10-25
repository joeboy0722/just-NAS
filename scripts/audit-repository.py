"""Pre-publication checks of Git content; never print local secret values.

Checks current tracked paths, all historical blob sizes, common key/token formats,
and exact current VM secrets when local verification credentials are available.
This is a focused check, not a replacement for a full secret scanner.
"""
from pathlib import Path
import json
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]

def git(*args):
    return subprocess.check_output(
        ['git', '-c', 'safe.directory=' + ROOT.as_posix(), *args], cwd=ROOT
    )

def values(value):
    if isinstance(value, dict):
        for item in value.values():
            yield from values(item)
    elif isinstance(value, str) and len(value) >= 12:
        yield value.encode()

secrets = set()
for filename in ['test-credentials.json', 'family-test-accounts.json', 'test-guest-credentials.json']:
    path = ROOT / 'output/vm-test' / filename
    if path.is_file():
        secrets.update(values(json.loads(path.read_text(encoding='utf-8-sig'))))
key = ROOT / 'output/vm-test/family-backup-test.key'
if key.is_file():
    secrets.add(key.read_bytes().strip())

tracked = git('ls-files', '-z').decode().split('\0')
forbidden = re.compile(
    r'(^output/|(^|/)\.env(?:\.|$)|\.(?:iso|vdi|pem|key|bundle|exe)$|'
    r'(^|/)nas-server-linux$|credentials\.json$|(^|/)__pycache__/)', re.I
)
failures = ['unexpected tracked artifact: ' + p for p in tracked if p and forbidden.search(p)]
patterns = [
    re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----'),
    re.compile(rb'\bgh[pousr]_[A-Za-z0-9]{30,}\b'),
    re.compile(rb'\bgithub_pat_[A-Za-z0-9_]{30,}\b'),
]

objects = git('rev-list', '--objects', '--all').splitlines()
entries = {line.split(b' ', 1)[0]: line.split(b' ', 1)[-1].decode(errors='replace') for line in objects}
inventory = subprocess.check_output(
    ['git', '-c', 'safe.directory=' + ROOT.as_posix(), 'cat-file', '--batch-check'],
    cwd=ROOT, input=b'\n'.join(entries) + b'\n'
).decode()
blobs = []
for line in inventory.splitlines():
    oid, kind, size = line.split()
    if kind != 'blob':
        continue
    if int(size) >= 10 * 1024 * 1024:
        failures.append('large historical blob: ' + entries[oid.encode()])
    else:
        blobs.append(oid)
if blobs:
    batch = subprocess.Popen(
        ['git', '-c', 'safe.directory=' + ROOT.as_posix(), 'cat-file', '--batch'],
        cwd=ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE
    )
    # Stream each object to avoid pipe deadlock on a growing repository.
    for oid in blobs:
        batch.stdin.write(oid.encode() + b'\n'); batch.stdin.flush()
        header = batch.stdout.readline().split()
        data = batch.stdout.read(int(header[2])); batch.stdout.read(1)
        if any(secret and secret in data for secret in secrets) or any(p.search(data) for p in patterns):
            failures.append('possible secret in Git blob: ' + entries[oid.encode()])
    batch.stdin.close()
    if batch.wait() != 0:
        raise RuntimeError('Git blob audit failed')

if failures:
    print('\n'.join(sorted(set(failures))))
    sys.exit(1)
print(f'PASS: tracked artifacts, {len(blobs)} historical blobs, key/token patterns and available local VM secrets')
