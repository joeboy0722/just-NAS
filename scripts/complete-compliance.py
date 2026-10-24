"""Add exact Go toolchains/module source and notices to the source collection."""
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
import hashlib, json, os, re, shutil, subprocess, tarfile, tempfile, zipfile
import requests

WORK=Path('/work'); TREE=WORK/'source-tree'; NOTICES=WORK/'licenses'
assert (WORK/'sources-collected.ok').is_file(), 'Run collect-compliance.py first'

def run(*args, env=None):
    p=subprocess.run(args,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=300)
    if p.returncode:raise RuntimeError(p.stderr.decode(errors='replace')[-2000:])
    return p.stdout

def sha(path):
    with path.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()

def selected(name):
    return bool(re.search(r'(^|/)[^/]*(licen[cs]e|copying|copyright|notice|patents|authors|readme)[^/]*$',name,re.I))

def save_notice(origin,name,data):
    if Path(name).is_absolute() or '..' in Path(name).parts or len(data)>5*1024*1024:return
    p=NOTICES/origin/name
    if p.is_file() and p.read_bytes()!=data:
        p=p.with_name(p.name+'.'+hashlib.sha256(data).hexdigest()[:12]+'.txt')
    p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)

def archive_notices(path,origin):
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as z:
            for n in z.infolist():
                if not n.is_dir() and selected(n.filename):save_notice(origin,n.filename,z.read(n))
    else:
        with tarfile.open(path) as t:
            for n in t:
                if n.isfile() and n.size<=5*1024*1024 and selected(n.name):save_notice(origin,n.name,t.extractfile(n).read())

with tempfile.TemporaryDirectory() as temp:
    temp=Path(temp)
    packages=json.loads((WORK/'packages.json').read_text())
    restic=next(p for p in packages if p['pkgname']=='restic')
    apk=temp/'restic.apk'
    run('xorriso','-osirrox','on','-indev','/input.iso','-extract',f'/apks/x86_64/restic-{restic["pkgver"]}.apk',str(apk))
    binary=temp/'restic';binary.write_bytes(run('tar','-xzOf',str(apk),'usr/bin/restic'))
    infos={'restic':run('go','version','-m',str(binary)).decode(),
           'just-nas':run('go','version','-m','/nas-server-linux').decode()}
    (WORK/'go-build-info.json').write_text(json.dumps(infos,indent=2))
    versions={re.search(r': (go\d+\.\d+(?:\.\d+)?)',text).group(1) for text in infos.values()}
    modules=[]
    for line in infos['restic'].splitlines():
        fields=line.split()
        if fields and fields[0]=='=>':raise RuntimeError('Handle module replacement explicitly')
        if len(fields)>=3 and fields[0]=='dep':
            modules.append({'module':fields[1],'version':fields[2],'binary_sum':fields[3] if len(fields)>3 else None})

cache=WORK/'go-download-cache';cache.mkdir(exist_ok=True)
env={**os.environ,'GOMODCACHE':str(cache),'GOPROXY':'https://proxy.golang.org','GOSUMDB':'sum.golang.org','GOTOOLCHAIN':'local'}

def get_module(record):
    result=json.loads(run('go','mod','download','-json',record['module']+'@'+record['version'],env=env))
    if result.get('Error'):raise RuntimeError(result['Error'])
    if record['binary_sum'] and result['Sum']!=record['binary_sum']:raise RuntimeError('Binary module checksum mismatch')
    paths=[]
    for field in ['Zip','GoMod','Info']:
        src=Path(result[field]);relative=src.relative_to(cache/'cache/download')
        dest=TREE/'go-module-proxy'/relative;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,dest)
        paths.append({'path':str(dest.relative_to(TREE)),'sha256':sha(dest)})
    record.update(verified_sum=result['Sum'],go_mod_sum=result['GoModSum'],sources=paths)
    archive_notices(Path(result['Zip']),'go-modules')
    print('PASS Go module '+record['module']+'@'+record['version'],flush=True)
    return record

with ThreadPoolExecutor(max_workers=6) as pool:
    list(pool.map(get_module,modules))
(WORK/'go-modules.json').write_text(json.dumps(modules,indent=2))

response=requests.get('https://go.dev/dl/?mode=json&include=all',timeout=60);response.raise_for_status()
toolchains=[]
for version in sorted(versions):
    release=next(item for item in response.json() if item['version']==version)
    source=next(item for item in release['files'] if item['kind']=='source')
    target=TREE/'upstream/go'/source['filename'];target.parent.mkdir(parents=True,exist_ok=True)
    if not target.is_file() or sha(target)!=source['sha256']:
        with requests.get('https://go.dev/dl/'+source['filename'],stream=True,timeout=(20,60)) as r:
            r.raise_for_status()
            with target.open('wb') as f:
                for chunk in r.iter_content(1024*1024):f.write(chunk)
    assert sha(target)==source['sha256']
    archive_notices(target,'go-'+version)
    toolchains.append({'version':version,'source':str(target.relative_to(TREE)),'sha256':source['sha256']})
    print('PASS Go source '+version,flush=True)
(WORK/'go-toolchains.json').write_text(json.dumps(toolchains,indent=2))
assert list((NOTICES/'linux-firmware').rglob('WHENCE')), 'Firmware WHENCE missing'
assert list(NOTICES.rglob('*COPYING*')), 'GPL text missing'
assert list((NOTICES/'zfs').rglob('LICENSE')), 'CDDL text missing'
print(f'PASS {len(modules)} exact Go modules and {len(toolchains)} toolchain source releases',flush=True)
(WORK/'go-sources-collected.ok').write_text('PASS\n')
