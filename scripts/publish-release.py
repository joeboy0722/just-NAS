"""Publish the rc4 replacement only after complete source/ISO verification.

Credentials are read from the Git credential helper in memory, never printed.
Stages: sources (draft only), all (upload/verify), publish (replace rc3).
"""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import argparse, hashlib, json, subprocess
import requests

ROOT=Path(__file__).resolve().parents[1]
TAG='v2.0.0-rc4';OLD='v2.0.0-rc3'
API='https://api.github.com/repos/joeboy0722/just-NAS'
parser=argparse.ArgumentParser();parser.add_argument('stage',choices=['sources','all','publish']);args=parser.parse_args()
result=subprocess.run(['git','credential','fill'],input='protocol=https\nhost=github.com\n\n',capture_output=True,text=True,timeout=60,check=True)
credential=dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
AUTH=(credential.get('username','x-access-token'),credential['password'])

def api(method,url,**kwargs):
    response=requests.request(method,url,auth=AUTH,headers={'Accept':'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28',**kwargs.pop('headers',{})},timeout=(30,1200),**kwargs)
    if not response.ok:raise RuntimeError(f'GitHub {method}: HTTP {response.status_code}')
    return response.json() if response.content else None

def sha(path):
    with path.open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()

def git(*parts):
    return subprocess.check_output(['git','-c','safe.directory='+ROOT.as_posix(),*parts],cwd=ROOT,text=True).strip()

dist=ROOT/'output/compliance/dist'
assert (ROOT/'output/compliance/packaged.ok').is_file()
partinfo=json.loads((dist/'SOURCE-PARTS.json').read_text())
assets={part['name']:dist/part['name'] for part in partinfo['parts']}
for part in partinfo['parts']:
    assert assets[part['name']].stat().st_size==part['bytes']
    assert sha(assets[part['name']])==part['sha256']
assets.update({name:dist/name for name in ['just-NAS-v2.0.0-rc4-licenses.tar.gz','SOURCES.md','SOURCE-PARTS.json']})
if args.stage!='sources':
    assert not (ROOT/'output/UNPUBLISHABLE-BOOTSTRAP').exists()
    assert (ROOT/'output/compliance/archive-verified.ok').is_file()
    assert 'PASS:' in (ROOT/'output/iso-audit/result.txt').read_text()
    assets['just-NAS-v2.0.0-rc4-x86_64.iso']=ROOT/'output/alpine-nas-full.iso'
    assert sha(assets['just-NAS-v2.0.0-rc4-x86_64.iso'])==(ROOT/'output/alpine-nas-full.iso.sha256').read_text().split()[0]
    assets.update({name:ROOT/name for name in ['LICENSE','THIRD-PARTY-NOTICES.md','COMPLIANCE.md']})
    assets.update({name:ROOT/'output'/name for name in ['package-versions.txt','aports-revision.txt']})
    assets['BUILD-INFO.json']=dist/'BUILD-INFO.json'
    checksums=ROOT/'output/SHA256SUMS'
    checksums.write_text(''.join(f'{sha(path)}  {name}\n' for name,path in assets.items()),encoding='ascii',newline='\n')
    assets['SHA256SUMS']=checksums

releases=api('GET',API+'/releases?per_page=100')
release=next((r for r in releases if r['tag_name']==TAG),None)
if release is None:
    assert args.stage!='publish'
    release=api('POST',API+'/releases',json={'tag_name':TAG,'target_commitish':git('rev-parse','HEAD'),'name':'just-NAS v2.0.0-rc4 — 授權與對應來源補件版','body':(ROOT/'RELEASE-NOTES.md').read_text(encoding='utf8'),'draft':True,'prerelease':True})
if args.stage in ['sources','all']:
    assert release['draft'],'Refusing to modify published assets'
    existing={a['name']:a for a in release['assets']}
    def upload(item):
        name,path=item;digest='sha256:'+sha(path)
        if name in existing:
            old=existing[name]
            if old['state']=='uploaded':
                assert old['size']==path.stat().st_size and old.get('digest')==digest
                print('Verified existing:',name,flush=True);return
            api('DELETE',API+'/releases/assets/'+str(old['id']))
        with path.open('rb') as stream:
            uploaded=api('POST',release['upload_url'].split('{')[0],params={'name':name},headers={'Content-Type':'application/octet-stream'},data=stream)
        assert uploaded['state']=='uploaded' and uploaded['size']==path.stat().st_size
        assert uploaded.get('digest')==digest
        print('Uploaded and SHA256 verified:',name,flush=True)
    with ThreadPoolExecutor(max_workers=2) as pool:list(pool.map(upload,assets.items()))
else:
    expected_tag=git('rev-parse',TAG+'^{commit}')
    assert expected_tag==git('rev-parse','HEAD'), 'Release tag must name the verified HEAD'
    remote=api('GET',API+'/git/ref/tags/'+TAG)
    obj=remote['object']
    if obj['type']=='tag':obj=api('GET',API+'/git/tags/'+obj['sha'])['object']
    assert obj['sha']==expected_tag
    current=api('GET',API+'/releases/'+str(release['id']))
    uploaded={a['name']:a for a in current['assets']}
    assert set(uploaded)==set(assets)
    for name,path in assets.items():
        assert uploaded[name]['state']=='uploaded' and uploaded[name]['size']==path.stat().st_size
        assert uploaded[name].get('digest')=='sha256:'+sha(path)
    old=next((r for r in releases if r['tag_name']==OLD),None)
    if old:
        assert old['id']!=release['id']
        api('DELETE',API+'/releases/'+str(old['id']))
        print('Removed superseded release and assets:',OLD,flush=True)
    if current['draft']:
        current=api('PATCH',API+'/releases/'+str(release['id']),json={'draft':False,'prerelease':True,'body':(ROOT/'RELEASE-NOTES.md').read_text(encoding='utf8')})
    assert not current['draft']
    (ROOT/'output/github-release.json').write_text(json.dumps({'url':current['html_url'],'tag':TAG,'replaced_release':OLD,'assets':[{'name':a['name'],'size':a['size'],'digest':a.get('digest'),'url':a['browser_download_url']} for a in current['assets']]},indent=2),encoding='utf8')
    print('Published:',current['html_url'],flush=True)
