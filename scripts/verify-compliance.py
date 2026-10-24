"""Verify split source stream and required corresponding-source paths before release."""
from pathlib import Path
import hashlib,json,subprocess,tarfile

WORK=Path('/work');DIST=WORK/'dist'
info=json.loads((DIST/'SOURCE-PARTS.json').read_text())
listing=WORK/'source-archive-files.txt'
with listing.open('wb') as out:
    decoder=subprocess.Popen(['zstd','-q','-d','-c'],stdin=subprocess.PIPE,stdout=subprocess.PIPE)
    archive=subprocess.Popen(['tar','-tf','-'],stdin=decoder.stdout,stdout=out)
    decoder.stdout.close();digest=hashlib.sha256()
    for part in info['parts']:
        p=DIST/part['name'];current=hashlib.sha256();size=0
        with p.open('rb') as stream:
            for chunk in iter(lambda:stream.read(1024*1024),b''):
                digest.update(chunk);current.update(chunk);size+=len(chunk);decoder.stdin.write(chunk)
        assert size==part['bytes'] and current.hexdigest()==part['sha256']
        print('Verified source part:',part['name'],flush=True)
    decoder.stdin.close()
    assert decoder.wait()==0 and archive.wait()==0,'Invalid source archive stream'
    assert digest.hexdigest()==info['assembled_sha256']
paths={p.removeprefix('./') for p in listing.read_text().splitlines()}
assert not any(p.startswith('/') or '..' in Path(p).parts for p in paths)
for origin in json.loads((WORK/'source-inventory.json').read_text()):
    assert 'recipes/'+origin['origin']+'/APKBUILD' in paths
    for source in origin['sources']:assert source['path'] in paths
for module in json.loads((WORK/'go-modules.json').read_text()):
    for source in module['sources']:assert source['path'] in paths
for runtime in json.loads((WORK/'go-toolchains.json').read_text()):assert runtime['source'] in paths
with tarfile.open(DIST/'just-NAS-v2.0.0-rc4-licenses.tar.gz') as licenses:
    names=set(licenses.getnames())
    assert 'go.mod' in names and 'iso/files.sha256' in names and 'iso/SOURCES.md' in names
    assert any(n.endswith('/WHENCE') for n in names)
    assert any('/go-go1.25.6/' in n and n.endswith('/LICENSE') for n in names)
(WORK/'archive-verified.ok').write_text('PASS: source parts, complete stream, required source paths and license archive\n')
print(f'PASS complete source stream: {len(paths)} archive entries verified',flush=True)
