"""Anonymous download verification of all published official Release bundles."""
import pathlib,json,hashlib,base64,time,requests
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
R=pathlib.Path(__file__).resolve().parents[1];BASE='https://github.com/joeboy0722/just-NAS/releases/download/stores-v1/';s=requests.Session()
r=s.get(BASE+'catalog.json',timeout=60);r.raise_for_status();raw=r.content;assert raw==(R/'assets/store-catalog.json').read_bytes();v=json.loads(raw);c=json.dumps(v['catalog'],separators=(',',':'),ensure_ascii=False).encode();key=Ed25519PublicKey.from_public_bytes(base64.b64decode((R/'assets/store-keys/official.pub').read_text().strip()));key.verify(base64.b64decode(v['signature']),c);assert len(v['catalog']['entries'])==9
checks=[{'check':'Anonymous remote catalog matches committed signed catalog','result':'PASS'}];out=R/'output/release-download-verification.json'
for e in v['catalog']['entries']:
 assert e['url']==BASE+e['id']+'-'+e['version']+'.jnpkg';h=hashlib.sha256();size=0
 with s.get(e['url'],stream=True,timeout=(20,180)) as res:
  res.raise_for_status()
  for b in res.iter_content(1024**2):h.update(b);size+=len(b);assert size<=e['bytes']
 assert h.hexdigest()==e['sha256'] and size==e['bytes'];checks.append({'check':e['id'],'result':'PASS','bytes':size,'sha256':h.hexdigest()});out.write_text(json.dumps(checks,indent=2)+'\n',encoding='utf-8');print('PASS anonymous download',e['id'],size,flush=True)
print('PASS all nine published official packages',flush=True)
