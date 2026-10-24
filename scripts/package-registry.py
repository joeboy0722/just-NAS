"""Publish/download declarative bundles as OCI artifacts in GitHub Packages.
Credentials are read from Docker's credential helper for publishing only.
Downloads are anonymous and verify the committed Ed25519 trust key.
"""
import argparse,base64,hashlib,json,os,pathlib,shutil,subprocess,sys,urllib.parse
import requests
ROOT=pathlib.Path(__file__).resolve().parents[1]
REPO='joeboy0722/just-nas-extensions';BASE='https://ghcr.io/v2/'+REPO
OCI='application/vnd.oci.image.manifest.v1+json';CAT='application/vnd.just-nas.catalog.v1+json';BUNDLE='application/vnd.just-nas.bundle.v1+gzip'
def digest(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  while b:=f.read(1024**2):h.update(b)
 return 'sha256:'+h.hexdigest()
def credentials():
 conf=json.loads((pathlib.Path.home()/'.docker/config.json').read_text(encoding='utf-8'))
 helper=conf.get('credHelpers',{}).get('ghcr.io') or conf.get('credsStore')
 if helper:
  exe=shutil.which('docker-credential-'+helper)
  if not exe:
   candidate=pathlib.Path(os.environ.get('ProgramFiles','C:/Program Files'))/'Docker/Docker/resources/bin'/('docker-credential-'+helper+'.exe');assert candidate.is_file(),'Docker credential helper unavailable';exe=str(candidate)
  r=subprocess.run([exe,'get'],input='ghcr.io',text=True,capture_output=True)
  if r.returncode!=0:raise RuntimeError('Run docker login ghcr.io with write:packages first')
  v=json.loads(r.stdout);return (v['Username'],v['Secret'])
 auth=conf.get('auths',{}).get('ghcr.io',{}).get('auth')
 if not auth:raise RuntimeError('Run docker login ghcr.io with write:packages first')
 return tuple(base64.b64decode(auth).decode().split(':',1))
class Registry:
 def __init__(self,publish=False):
  self.s=requests.Session();r=self.s.get('https://ghcr.io/token',params={'service':'ghcr.io','scope':'repository:'+REPO+(':pull,push' if publish else ':pull')},auth=credentials() if publish else None,timeout=30)
  if r.status_code!=200:raise RuntimeError('Cannot access public Packages registry; check package visibility or publishing scope (HTTP '+str(r.status_code)+')')
  token=r.json().get('token');assert token;self.s.headers['Authorization']='Bearer '+token
 def request(self,method,path,**kwargs):
  target=path if path.startswith('https://ghcr.io/') else BASE+path
  assert target.startswith('https://ghcr.io/v2/'+REPO+'/')
  r=self.s.request(method,target,timeout=(30,1200),**kwargs)
  if r.status_code>=400:raise RuntimeError('Packages '+method+' failed (HTTP '+str(r.status_code)+'); check write:packages and public visibility')
  return r
 def upload(self,p,media):
  d=digest(p);head=self.s.head(BASE+'/blobs/'+d,timeout=30)
  if head.status_code!=200:
   r=self.request('POST','/blobs/uploads/');loc=r.headers['Location'];loc=urllib.parse.urljoin('https://ghcr.io',loc);assert loc.startswith((BASE+'/blobs/upload/',BASE+'/blobs/uploads/'))
   sep='&' if '?' in loc else '?'
   print('UPLOAD',p.name,p.stat().st_size,flush=True)
   with p.open('rb') as f:self.request('PUT',loc+sep+'digest='+d,data=f,headers={'Content-Type':'application/octet-stream'})
  else:print('EXISTS',p.name,flush=True)
  return {'mediaType':media,'digest':d,'size':p.stat().st_size,'annotations':{'org.opencontainers.image.title':p.name}}
 def artifact(self,tag,layers,config,kind):
  m={'schemaVersion':2,'mediaType':OCI,'artifactType':kind,'config':config,'layers':layers,'annotations':{'org.opencontainers.image.source':'https://github.com/joeboy0722/just-NAS','org.opencontainers.image.description':'Verified just-NAS optional packages; MIT project with separately licensed third-party components','org.opencontainers.image.title':tag}}
  raw=json.dumps(m,separators=(',',':')).encode();self.request('PUT','/manifests/'+tag,data=raw,headers={'Content-Type':OCI});d='sha256:'+hashlib.sha256(raw).hexdigest();print('PUBLISHED',tag,d,flush=True);return d
 def manifest(self,tag):return self.request('GET','/manifests/'+tag,headers={'Accept':OCI}).json()
 def blob(self,d):
  assert d.startswith('sha256:') and len(d)==71
  r=self.request('GET','/blobs/'+d);assert 'sha256:'+hashlib.sha256(r.content).hexdigest()==d;return r.content

def catalog(registry):
 from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
 m=registry.manifest('catalog-v1');assert m['schemaVersion']==2 and m['mediaType']==OCI and len(m['layers'])==1
 d=m['layers'][0];assert d['mediaType']==CAT and 0<d['size']<=2*1024**2
 raw=registry.blob(d['digest']);assert len(raw)==d['size'];v=json.loads(raw);c=json.dumps(v['catalog'],separators=(',',':'),ensure_ascii=False).encode();key=Ed25519PublicKey.from_public_bytes(base64.b64decode((ROOT/'assets/store-keys/official.pub').read_text().strip()));key.verify(base64.b64decode(v['signature']),c)
 assert v['catalog']['abi']=='just-nas-stores-v1';return v['catalog']

def publish():
 out=ROOT/'output/package-registry';out.mkdir(exist_ok=True);state=subprocess.check_output(['git','-c','safe.directory='+ROOT.as_posix(),'status','--porcelain'],cwd=ROOT,text=True);assert not state,'Commit before publishing'
 reg=Registry(True);env=json.loads((ROOT/'output/store-packages/catalog.json').read_text(encoding='utf-8'));entries=env['catalog']['entries'];assert len(entries)==9
 empty=out/'config.json';empty.write_text('{}',encoding='ascii');config=reg.upload(empty,'application/vnd.oci.empty.v1+json');published={}
 for e in entries:
  p=ROOT/'output/store-packages'/(e['id']+'-'+e['version']+'.jnpkg');d=digest(p);assert d=='sha256:'+e['sha256'] and p.stat().st_size==e['bytes'] and e['url']==BASE+'/blobs/'+d
  layer=reg.upload(p,BUNDLE);published[e['id']]=reg.artifact(e['id']+'-'+e['version'],[layer],config,'application/vnd.just-nas.package.v1')
 # Include source and license artifacts in Packages, not a binary Release.
 source=out/'just-NAS-source.tar.gz';subprocess.run(['git','-c','safe.directory='+ROOT.as_posix(),'archive','--format=tar.gz','--prefix=just-NAS/','-o',str(source),'HEAD'],cwd=ROOT,check=True)
 sources=out/'SOURCES.md';checks=(ROOT/'output/SHA256SUMS').read_text(encoding='utf-8');parts=[line for line in checks.splitlines() if '.part-' in line]
 sources.write_text('# Corresponding sources\n\nThe project source archive uses MIT. Third-party components keep their own licenses. New runtime sources and the exact core license collection accompany this artifact. Unchanged kernel/firmware/Go/core sources reuse rc4, available at https://github.com/joeboy0722/just-NAS/releases/tag/v2.0.0-rc4 . Keep those assets available. Download all seven parts and concatenate in numeric order before decompressing; see rc4 SOURCES.md and SOURCE-PARTS.json.\n\n```text\n'+'\n'.join(parts)+'\n```\n',encoding='utf-8',newline='\n')
 materials=[ROOT/'LICENSE',ROOT/'THIRD-PARTY-NOTICES.md',ROOT/'COMPLIANCE.md',ROOT/'output/store-packages/runtime-corresponding-sources.tar.zst',ROOT/'output/compliance/dist/just-NAS-v2.0.0-rc4-licenses.tar.gz',source,sources]
 layers=[reg.upload(p,'application/octet-stream') for p in materials];published['compliance']=reg.artifact('compliance-v1',layers,config,'application/vnd.just-nas.sources.v1')
 # Catalog is last: do not advertise packages before all immutable blobs exist.
 layer=reg.upload(ROOT/'output/store-packages/catalog.json',CAT);published['catalog']=reg.artifact('catalog-v1',[layer],config,'application/vnd.just-nas.catalog.v1')
 (out/'publication.json').write_text(json.dumps(published,indent=2)+'\n',encoding='utf-8')
 print('Uploaded to GitHub Packages. Confirm PUBLIC visibility before anonymous verification.',flush=True)

def download(id,dest):
 reg=Registry();c=catalog(reg);e=next(e for e in c['entries'] if e['id']==id);assert e['url']==BASE+'/blobs/sha256:'+e['sha256']
 dest.mkdir(parents=True,exist_ok=True);target=dest/(e['id']+'-'+e['version']+'.jnpkg');tmp=target.with_suffix('.partial')
 try:
  r=reg.request('GET','/blobs/sha256:'+e['sha256'],stream=True);h=hashlib.sha256();n=0
  with tmp.open('wb') as f:
   for b in r.iter_content(1024**2):
    n+=len(b);assert n<=e['bytes'];h.update(b);f.write(b)
  assert n==e['bytes'] and h.hexdigest()==e['sha256'];tmp.replace(target)
 finally:
  if tmp.exists():tmp.unlink()
 print('VERIFIED',target,flush=True)
if __name__=='__main__':
 p=argparse.ArgumentParser();g=p.add_mutually_exclusive_group(required=True);g.add_argument('--publish',action='store_true');g.add_argument('--download',metavar='PACKAGE_ID');g.add_argument('--verify-catalog',action='store_true');p.add_argument('--output',type=pathlib.Path,default=pathlib.Path('downloads'));a=p.parse_args()
 if a.publish:publish()
 elif a.verify_catalog:
  c=catalog(Registry());print('VERIFIED public signed Packages catalog:',len(c['entries']),'entries')
 else:download(a.download,a.output)
