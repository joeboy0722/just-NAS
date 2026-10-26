"""Explicit prerelease tooling. Keeps incomplete acceptance visible; never marks it fully verified."""
import argparse,base64,ctypes,hashlib,importlib.util,io,json,os,pathlib,posixpath,re,shutil,stat,struct,subprocess,tarfile,time
R=pathlib.Path(__file__).resolve().parents[1];REPO='joeboy0722/just-NAS';API='https://api.github.com/repos/'+REPO
import requests

def git(*args):return subprocess.check_output(['git','-c','safe.directory='+R.as_posix(),*args],cwd=R,text=True).strip()
def sha(path):
 with pathlib.Path(path).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def signing():
 s=importlib.util.spec_from_file_location('bundles',R/'scripts/build-extension-packages.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m.keypair()
def lx_link(path):
 from ctypes import wintypes as w
 k=ctypes.WinDLL('kernel32',use_last_error=True);k.CreateFileW.argtypes=[w.LPCWSTR,w.DWORD,w.DWORD,w.LPVOID,w.DWORD,w.DWORD,w.HANDLE];k.CreateFileW.restype=w.HANDLE;k.DeviceIoControl.argtypes=[w.HANDLE,w.DWORD,w.LPVOID,w.DWORD,w.LPVOID,w.DWORD,ctypes.POINTER(w.DWORD),w.LPVOID];k.CloseHandle.argtypes=[w.HANDLE]
 h=k.CreateFileW(str(path.resolve()),0,7,None,3,0x2200000,None)
 if h==w.HANDLE(-1).value:raise ctypes.WinError(ctypes.get_last_error())
 try:
  b=ctypes.create_string_buffer(16384);n=w.DWORD()
  if not k.DeviceIoControl(h,0x900a8,None,0,b,len(b),ctypes.byref(n),None):raise ctypes.WinError(ctypes.get_last_error())
  tag,length,_=struct.unpack_from('<IHH',b.raw);assert tag==0xa000001d
  return b.raw[12:8+length].decode('utf-8').rstrip(chr(0))
 finally:k.CloseHandle(h)
def source_bytes(path):
 for _ in range(16):
  if getattr(path.lstat(),'st_reparse_tag',0)==0xa000001d:path=path.parent/lx_link(path)
  elif path.is_symlink():path=path.parent/os.readlink(path)
  else:return path.read_bytes()
 raise ValueError('Source symlink loop')
def add_tree(archive,root,prefix,epoch):
 for path in [root,*sorted(root.rglob('*'))]:
  name=prefix if path==root else prefix+'/'+path.relative_to(root).as_posix();st=path.lstat();info=tarfile.TarInfo(name);info.mtime=epoch
  if getattr(st,'st_reparse_tag',0)==0xa000001d or stat.S_ISLNK(st.st_mode):
   target=lx_link(path) if getattr(st,'st_reparse_tag',0)==0xa000001d else os.readlink(path)
   assert not target.startswith(('/', '\\')) and not re.match(r'^[A-Za-z]:',target)
   normalized=posixpath.normpath(posixpath.join(posixpath.dirname(name),target))
   assert normalized==prefix or normalized.startswith(prefix+'/'),name+' escaping source link'
   info.type=tarfile.SYMTYPE;info.linkname=target;info.mode=0o777;archive.addfile(info)
  elif stat.S_ISDIR(st.st_mode):info.type=tarfile.DIRTYPE;info.mode=0o755;archive.addfile(info)
  elif stat.S_ISREG(st.st_mode):
   info.size=st.st_size;info.mode=0o755 if st.st_mode&0o111 else 0o644
   with path.open('rb') as f:archive.addfile(info,f)
  else:raise ValueError('Non-source special file '+str(path))
def evidence(path,core=False):
 d=json.loads(path.read_text(encoding='utf-8'));assert d['result']=='PREVIEW' and d['channel']=='prerelease'
 for key in ['windows_kvm_desktop','windows_iso_detached_disk_boot','windows_normal_shutdown','nas_self_build','nas_self_built_cold_boot','package_signatures','runtime_sources']:assert d['checks'][key]=='PASS',key
 assert d.get('known_limitations') and d.get('deferred'),'Explicit limitations required'
 if core:assert d['checks']['online_install']=='PASS'
 return d

def prepare_stores(directory):
 directory.mkdir(parents=True,exist_ok=True)
 pairs={'qemu-runtime':R/'output/vm-store-packages/qemu-runtime-11.0.3-r0.jnpkg','vm-manager':R/'output/vm-settings-packages/vm-manager-1.0.0-rc2.jnpkg','container-engine':R/'output/store-packages/container-engine-1.0.0.jnpkg'}
 catalog=json.loads((R/'assets/store-catalog-v2.json').read_text(encoding='utf-8'))['catalog']
 for entry in catalog['entries']:
  if entry['id'] not in pairs:continue
  src=pairs[entry['id']];shutil.copyfile(src,directory/src.name);entry.update(url='https://github.com/'+REPO+'/releases/download/stores-v2/'+src.name,bytes=src.stat().st_size,sha256=sha(src))
  if entry['id']=='vm-manager':entry['version']='1.0.0-rc2'
 raw=json.dumps(catalog,separators=(',',':'),ensure_ascii=False).encode();env=b'{"catalog":'+raw+b',"signature":'+json.dumps(base64.b64encode(signing().sign(raw)).decode()).encode()+b'}'
 (directory/'catalog.json').write_bytes(env);(R/'assets/store-catalog-v2.json').write_bytes(env)
 print('PASS prepared three immutable bundles and signed v2 catalog')

def stage_stores(directory,report):
 assert not git('status','--porcelain'),'Commit sources/catalog before staging'
 d=evidence(report);inventory=json.loads((R/'output/vm-compliance/source-inventory.json').read_text())
 for group in inventory:
  for item in group['sources']:
   assert item['verified'];raw=source_bytes(R/'output/vm-compliance/source-tree'/item['path'])
   if item.get('sha256'):assert hashlib.sha256(raw).hexdigest()==item['sha256']
   if item.get('checksum_algorithm')=='sha512':assert hashlib.sha512(raw).hexdigest()==item['expected_checksum']
 epoch=int(git('show','-s','--format=%ct','HEAD'));own=R/'output/stores-v2-project-source.tar.gz'
 subprocess.run(['git','-c','safe.directory='+R.as_posix(),'archive','--format=tar.gz','--prefix=just-NAS/','-o',str(own),'HEAD'],check=True,cwd=R)
 support=directory/'just-NAS-stores-v2-support.tar.gz'
 with tarfile.open(support,'w:gz',compresslevel=3) as t:
  for name in ['LICENSE','THIRD-PARTY-NOTICES.md','VM-SOURCES.md','COMPLIANCE.md','VM-MANAGER.md','VM-VERIFICATION.md','NAS-BUILD.md','NAS-BUILD-VERIFICATION.md','KVM-HOST-VERIFICATION.md','README.md']:
   t.add(R/name,arcname='documents/'+name)
  t.add(own,arcname='source/just-NAS-source.tar.gz')
  add_tree(t,R/'output/vm-compliance/source-tree','source/qemu-runtime',epoch)
  add_tree(t,R/'output/vm-compliance/licenses','licenses/qemu-runtime',epoch)
  t.add(R/'output/store-packages/runtime-corresponding-sources.tar.zst',arcname='source/container-engine-corresponding-sources.tar.zst')
  t.add(R/'output/compliance/dist/just-NAS-v2.0.0-rc4-licenses.tar.gz',arcname='licenses/core-licenses.tar.gz')
  for name in ['packages.json','source-inventory.json']:t.add(R/'output/vm-compliance'/name,arcname='build/'+name)
  t.add(R/'output/vm-test/runtime-apk-source-coverage.json',arcname='build/runtime-apk-source-coverage.json')
  data=json.dumps({'source_commit':git('rev-parse','HEAD'),'validation':d,'notice':'Explicit prerelease; deferred checks are not PASS'},indent=2).encode();info=tarfile.TarInfo('verification/preview.json');info.size=len(data);info.mtime=epoch;t.addfile(info,io.BytesIO(data))
 expected={'qemu-runtime-11.0.3-r0.jnpkg','vm-manager-1.0.0-rc2.jnpkg','container-engine-1.0.0.jnpkg','catalog.json',support.name}
 assert {p.name for p in directory.iterdir()}<=expected|{'SHA256SUMS'}
 (directory/'SHA256SUMS').write_text(''.join(sha(directory/n)+'  '+n+'\n' for n in sorted(expected)),encoding='ascii')
 print('PASS six stores assets including exact QEMU/Docker sources and explicit incomplete-test report',support.stat().st_size,flush=True)

def checked_files(directory,core):
 expected={'LICENSE','SOURCES.md','SHA256SUMS','just-NAS-v2.1.0-rc4-x86_64.iso','just-NAS-v2.1.0-rc4-support.tar.gz'} if core else {'qemu-runtime-11.0.3-r0.jnpkg','vm-manager-1.0.0-rc2.jnpkg','container-engine-1.0.0.jnpkg','catalog.json','SHA256SUMS','just-NAS-stores-v2-support.tar.gz'}
 assert {p.name for p in directory.iterdir()}==expected
 sums={line.split('  ',1)[1]:line.split('  ',1)[0] for line in (directory/'SHA256SUMS').read_text().splitlines()};assert set(sums)==expected-{'SHA256SUMS'}
 for name,digest in sums.items():assert re.fullmatch('[a-f0-9]{64}',digest) and sha(directory/name)==digest,name
 return expected

def authenticated():
 p=subprocess.run(['git','-c','safe.directory='+R.as_posix(),'credential','fill'],input='protocol=https\nhost=github.com\n\n',capture_output=True,text=True,check=True,env={**os.environ,'GIT_TERMINAL_PROMPT':'0'});d=dict(x.split('=',1) for x in p.stdout.splitlines() if '=' in x)
 s=requests.Session();s.headers.update({'Authorization':'Bearer '+d['password'],'Accept':'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'});return s

def publish(directory,report,core,body_file):
 expected=checked_files(directory,core);evidence(report,core);assert not git('status','--porcelain')
 branch=git('branch','--show-current');commit=git('rev-parse','HEAD');assert (branch=='main' if core else branch.startswith('codex/'))
 assert git('ls-remote','origin','refs/heads/'+branch).split()[0]==commit
 tag='v2.1.0-rc4' if core else 'stores-v2';body=body_file.read_text(encoding='utf-8');assert 'MIT' in body and '預發布' in body and '第三方' in body
 s=authenticated()
 def api(method,path,**kw):
  r=s.request(method,API+path,timeout=60,**kw);r.raise_for_status();return r.json() if r.content else None
 releases=api('GET','/releases?per_page=100');rel=next((x for x in releases if x['tag_name']==tag),None)
 title='just-NAS v2.1.0-rc4 — 選配 VM／Docker 與 NAS 自行建置' if core else 'just-NAS stores-v2 — VM／QEMU／Docker 預發布套件'
 if rel:assert rel['draft'],'Published assets are immutable'
 else:rel=api('POST','/releases',json={'tag_name':tag,'target_commitish':commit,'name':title,'body':body,'draft':True,'prerelease':True})
 assets={x['name']:x for x in api('GET','/releases/'+str(rel['id'])+'/assets')};assert set(assets)<=expected
 for name in sorted(expected):
  p=directory/name;digest='sha256:'+sha(p)
  if name in assets:assert assets[name]['size']==p.stat().st_size and assets[name].get('digest')==digest;continue
  print('UPLOAD',name,p.stat().st_size,flush=True)
  with p.open('rb') as f:r=s.post(rel['upload_url'].split('{',1)[0],params={'name':name},data=f,headers={'Content-Type':'application/octet-stream'},timeout=(30,1800))
  r.raise_for_status();a=r.json();assert a['size']==p.stat().st_size and a.get('digest')==digest
 rel=api('PATCH','/releases/'+str(rel['id']),json={'draft':False,'prerelease':True,'name':title,'body':body,'make_latest':'false'})
 (directory.parent/('published-'+tag+'.json')).write_text(json.dumps({'url':rel['html_url'],'commit':commit,'assets':sorted(expected),'prerelease':True},indent=2));print('PUBLISHED',rel['html_url'],flush=True)

def verify(directory,core):
 expected=checked_files(directory,core);tag='v2.1.0-rc4' if core else 'stores-v2';s=requests.Session();s.trust_env=False;results=[]
 for name in sorted(expected):
  url='https://github.com/'+REPO+'/releases/download/'+tag+'/'+name;h=hashlib.sha256();size=0
  with s.get(url,stream=True,timeout=(30,1800)) as r:
   r.raise_for_status();assert 'Authorization' not in r.request.headers
   for b in r.iter_content(1024**2):h.update(b);size+=len(b)
  assert size==(directory/name).stat().st_size and h.hexdigest()==sha(directory/name),name
  results.append({'file':name,'bytes':size,'sha256':h.hexdigest(),'result':'PASS'});print('PASS anonymous fresh download',name,size,flush=True)
 (directory.parent/('anonymous-'+tag+'.json')).write_text(json.dumps(results,indent=2))

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['prepare-stores','stage-stores','publish','verify']);p.add_argument('--directory',type=pathlib.Path,required=True);p.add_argument('--report',type=pathlib.Path,default=R/'output/release-rc4-evidence/release-validation.json');p.add_argument('--core',action='store_true');p.add_argument('--body',type=pathlib.Path);a=p.parse_args();a.directory.resolve().relative_to((R/'output').resolve())
 if a.action=='prepare-stores':prepare_stores(a.directory)
 elif a.action=='stage-stores':stage_stores(a.directory,a.report)
 elif a.action=='publish':publish(a.directory,a.report,a.core,a.body)
 else:verify(a.directory,a.core)
if __name__=='__main__':main()
