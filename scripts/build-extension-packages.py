#!/usr/bin/env python3
"""Build signed offline bundles. Signing key stays in ignored secrets/, never in ISO."""
import argparse,base64,hashlib,io,json,pathlib,tarfile
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
ROOT=pathlib.Path(__file__).resolve().parents[1]

def keypair():
    folder=ROOT/'secrets';folder.mkdir(parents=True,exist_ok=True);p=folder/'stores-ed25519.key'
    if p.exists():key=serialization.load_pem_private_key(p.read_bytes(),None)
    else:
        if (ROOT/'assets/store-keys/official.pub').exists():raise RuntimeError('Missing private signing key in secrets/stores-ed25519.key; refusing to replace the existing trust key')
        key=Ed25519PrivateKey.generate();p.write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
    public=base64.b64encode(key.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)).decode()
    dest=ROOT/'assets/store-keys';dest.mkdir(parents=True,exist_ok=True)
    if (dest/'official.pub').exists() and (dest/'official.pub').read_text().strip()!=public:raise RuntimeError('Private key does not match committed public key')
    (dest/'official.pub').write_text(public+'\n',encoding='ascii')
    return key

def pack(src,manifest,target,key):
    files={};paths=[]
    for p in sorted(src.rglob('*')):
        if not p.is_file():continue
        n=p.relative_to(src).as_posix();b=p.read_bytes();mode=0o755 if p.stat().st_mode&0o111 else 0o644
        files[n]={'sha256':hashlib.sha256(b).hexdigest(),'size':len(b),'mode':mode};paths.append((p,n))
    manifest.update(format=1,abi='just-nas-stores-v1',arch='x86_64',files=files,installed_bytes=sum(f['size'] for f in {(v['sha256'],v['mode']):v for v in files.values()}.values()))
    raw=json.dumps(manifest,separators=(',',':'),ensure_ascii=False).encode();sig=base64.b64encode(key.sign(raw)).decode() if key else ''
    envelope=b'{"manifest":'+raw+b',"signature":'+json.dumps(sig).encode()+b'}'
    target.parent.mkdir(parents=True,exist_ok=True)
    with tarfile.open(target,'w:gz',dereference=True) as t:
        info=tarfile.TarInfo('package.json');info.size=len(envelope);info.mode=0o644;t.addfile(info,io.BytesIO(envelope))
        seen={}
        for p,n in paths:
            identity=(files[n]['sha256'],files[n]['mode'])
            if identity in seen:
                info=tarfile.TarInfo('payload/'+n);info.type=tarfile.LNKTYPE;info.linkname='payload/'+seen[identity];info.mode=files[n]['mode'];t.addfile(info);continue
            seen[identity]=n
            info=tarfile.TarInfo('payload/'+n);info.size=p.stat().st_size;info.mode=files[n]['mode']
            with p.open('rb') as f:t.addfile(info,f)
    return manifest

def build():
    key=keypair();source=ROOT/'output/extensions-payload';dest=ROOT/'output/store-packages';entries=[]
    inventory=json.loads((source/'split-inventory.json').read_text());kernel=inventory['kernel']
    names={'gpu-nvidia':'NVIDIA GPU 支援','gpu-amd':'AMD GPU 支援','gpu-intel':'Intel GPU 支援','audio':'音效支援','media':'攝影機與電視卡','ai-accelerators':'AI 運算加速器','ssh-server':'SSH 遠端維護','container-engine':'Docker 容器引擎','static-site':'靜態網站示範'}
    for id,name in names.items():
        src=source/id
        if not src.exists():continue
        kind='app' if id=='static-site' else ('driver' if id not in ('ssh-server','container-engine') else ('ssh' if id=='ssh-server' else 'runtime'))
        manifest=dict(id=id,name=name,version=kernel if kind=='driver' else '1.0.0',kind=kind,license='See bundled licenses',dependencies={},permissions=['kernel-modules'] if kind=='driver' else ['system-service'],reboot=kind=='driver')
        if kind=='driver':manifest['kernel']=kernel
        if kind=='app':
            with tarfile.open(src/'image.tar') as t:
                saved=json.load(t.extractfile('manifest.json'));config=t.extractfile(saved[0]['Config']).read();image='sha256:'+hashlib.sha256(config).hexdigest()
            manifest.update(license='MIT',dependencies={'container-engine':'1.0.0'},permissions=['selected-content-dataset','lan-port'],app=dict(image=image,user='65532:65532',container_port=8080,content_path='/srv/content',health_path='/',memory_mb=64,cpus=0.5))
        licenses=src/'licenses';licenses.mkdir(exist_ok=True)
        import shutil
        if kind!='app':shutil.copytree(ROOT/'output/compliance/iso',licenses/'core-third-party',dirs_exist_ok=True)
        extra=ROOT/'output/extensions-compliance'
        if kind=='runtime' and (extra/'licenses').exists():
            shutil.copytree(extra/'licenses',licenses/'runtime-third-party',dirs_exist_ok=True)
            shutil.copyfile(extra/'packages.json',licenses/'runtime-packages.json')
            shutil.copyfile(extra/'source-inventory.json',licenses/'runtime-source-inventory.json')
        for f in ['LICENSE','THIRD-PARTY-NOTICES.md']:(licenses/f).write_bytes((ROOT/f).read_bytes())
        file=dest/(id+'-'+manifest['version']+'.jnpkg');m=pack(src,manifest,file,key)
        entries.append(dict(id=id,name=name,version=m['version'],kind=kind,url='https://ghcr.io/v2/joeboy0722/just-nas-extensions/blobs/sha256:'+hashlib.sha256(file.read_bytes()).hexdigest(),bytes=file.stat().st_size,sha256=hashlib.sha256(file.read_bytes()).hexdigest(),license=m['license'],dependencies=m['dependencies'],reboot=m['reboot'],**({'kernel':kernel} if kind=='driver' else {})))
        print(id,file.stat().st_size,flush=True)
    catalog=json.dumps(dict(version=1,abi='just-nas-stores-v1',entries=entries),separators=(',',':'),ensure_ascii=False).encode()
    (dest/'catalog.json').write_bytes(b'{"catalog":'+catalog+b',"signature":'+json.dumps(base64.b64encode(key.sign(catalog)).decode()).encode()+b'}')
    (ROOT/'assets/store-catalog.json').write_bytes((dest/'catalog.json').read_bytes())
def catalog_only():
    key=keypair();dest=ROOT/'output/store-packages';env=json.loads((dest/'catalog.json').read_text(encoding='utf-8'));catalog=env['catalog']
    assert catalog['version']==1 and catalog['abi']=='just-nas-stores-v1'
    assert len(catalog['entries'])==9
    for entry in catalog['entries']:
        candidates=list(dest.glob(entry['id']+'-'+entry['version']+'.jnpkg'));assert len(candidates)==1
        p=candidates[0];digest=hashlib.sha256(p.read_bytes()).hexdigest();assert digest==entry['sha256'] and p.stat().st_size==entry['bytes']
        entry['url']='https://ghcr.io/v2/joeboy0722/just-nas-extensions/blobs/sha256:'+digest
    raw=json.dumps(catalog,separators=(',',':'),ensure_ascii=False).encode()
    envelope=b'{"catalog":'+raw+b',"signature":'+json.dumps(base64.b64encode(key.sign(raw)).decode()).encode()+b'}'
    (dest/'catalog.json').write_bytes(envelope);(ROOT/'assets/store-catalog.json').write_bytes(envelope)
    print('Re-signed Packages catalog; nine package binaries unchanged')

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--key-only',action='store_true');p.add_argument('--catalog-only',action='store_true');p.add_argument('--manifest',type=pathlib.Path);p.add_argument('--payload',type=pathlib.Path);p.add_argument('--output',type=pathlib.Path);a=p.parse_args()
    if a.manifest:
        if not a.payload or not a.output:p.error('--manifest requires --payload and --output')
        pack(a.payload,json.loads(a.manifest.read_text(encoding='utf-8')),a.output,None)
    elif a.key_only:keypair()
    elif a.catalog_only:catalog_only()
    else:build()
