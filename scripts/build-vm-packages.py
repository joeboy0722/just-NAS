"""Build fixed-version v2 offline packages; never publishes or includes guest ISO/disks."""
import argparse,importlib.util,pathlib,shutil,json,hashlib,base64,sys
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output'
p=argparse.ArgumentParser();p.add_argument('--app-only',action='store_true');p.add_argument('--app-version',default='1.0.0-rc1');p.add_argument('--binary',type=pathlib.Path,default=O/'vm-manager-linux');p.add_argument('--output',type=pathlib.Path,default=O/'vm-store-packages');p.add_argument('--bundle',action='store_true');args=p.parse_args()
dest=args.output;dest.mkdir(exist_ok=True,parents=True)
sp=importlib.util.spec_from_file_location('bundles',R/'scripts/build-extension-packages.py');b=importlib.util.module_from_spec(sp);sp.loader.exec_module(b);key=b.keypair()
runtime=O/'vm-runtime-payload';app=O/'vm-app-payload';(app/'bin').mkdir(exist_ok=True,parents=True);shutil.copyfile(args.binary,app/'bin/vm-manager')
for folder in ([app] if args.app_only else [runtime,app]):
 licenses=folder/'licenses';licenses.mkdir(exist_ok=True)
 for name in ['LICENSE','THIRD-PARTY-NOTICES.md']:shutil.copyfile(R/name,licenses/name)
if not args.app_only:shutil.copytree(O/'compliance/iso',runtime/'licenses/core-third-party',dirs_exist_ok=True)
if not args.app_only:shutil.copytree(O/'vm-compliance/licenses',runtime/'licenses/qemu-third-party',dirs_exist_ok=True)
for name in ([] if args.app_only else ['packages.json','source-inventory.json']):shutil.copyfile(O/'vm-compliance'/name,runtime/'licenses'/name)
shutil.copytree(R/'third_party/novnc-source/docs',app/'licenses/novnc',dirs_exist_ok=True);shutil.copyfile(R/'third_party/novnc-source/LICENSE.txt',app/'licenses/novnc/LICENSE.txt')
shutil.copyfile(R/'vendor/github.com/gorilla/websocket/LICENSE',app/'licenses/gorilla-websocket-LICENSE')
common=dict(format=2,min_nas_version='2.1.0-rc4',license='MIT project code; see bundled third-party licenses',reboot=False)
specs=[(runtime,dict(common,id='qemu-runtime',name='QEMU 虛擬化執行環境',version='11.0.3-r0',kind='runtime',dependencies={},permissions=['system-service','virtualization'])),(app,dict(common,id='vm-manager',name='虛擬機管理器',version=args.app_version,kind='app',native={'service':'vm-manager'},icon='assets/icon.png',dependencies={'qemu-runtime':'11.0.3-r0'},permissions=['native-service','virtualization','vm-storage','lan-port']))]
entries=json.loads((R/'assets/store-catalog.json').read_text(encoding='utf-8'))['catalog']['entries']
for folder,manifest in specs:
 if args.app_only and manifest['id']!='vm-manager':continue
 target=dest/(manifest['id']+'-'+manifest['version']+'.jnpkg');m=b.pack(folder,manifest,target,key)
 entry=dict(id=m['id'],name=m['name'],version=m['version'],kind=m['kind'],url='https://github.com/joeboy0722/just-NAS/releases/download/stores-v2/'+target.name,bytes=target.stat().st_size,sha256=hashlib.sha256(target.read_bytes()).hexdigest(),license=m['license'],dependencies=m['dependencies'],reboot=False,min_nas_version='2.1.0-rc4')
 if m.get('native'):entry['runtime']='native'
 entries.append(entry)
 print(target.name,target.stat().st_size,flush=True)
catalog=json.dumps(dict(version=2,abi='just-nas-stores-v2',entries=entries),separators=(',',':'),ensure_ascii=False).encode();(dest/'catalog.json').write_bytes(b'{"catalog":'+catalog+b',"signature":'+json.dumps(base64.b64encode(key.sign(catalog)).decode()).encode()+b'}')
if args.bundle:shutil.copyfile(dest/'catalog.json',R/'assets/store-catalog-v2.json')
