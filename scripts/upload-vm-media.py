"""Stream user-provided test ISOs through the app API, with pinned NAS TLS over temporary NAT."""
import pathlib,json,time,requests,sys
from requests.adapters import HTTPAdapter
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';BASE='https://127.0.0.1:18443'
class PinnedNAS(HTTPAdapter):
 def init_poolmanager(self,*args,**kwargs):kwargs['assert_hostname']='192.168.56.101';return super().init_poolmanager(*args,**kwargs)
s=requests.Session();s.mount(BASE,PinnedNAS());s.verify=str(O/'just-nas-certificate.pem')
time.sleep(1.2);r=s.post(BASE+'/api/auth/login',json={'password':json.loads((O/'test-credentials.json').read_text(encoding='utf-8'))['admin']},timeout=30);r.raise_for_status();s.headers['X-CSRF-Token']=r.json()['csrf']
P='/api/app-services/vm-manager'
class Progress:
 def __init__(self,file):self.file=file;self.total=file.stat().st_size;self.stream=file.open('rb');self.sent=0;self.last=0
 def __len__(self):return self.total
 def read(self,size=-1):
  data=self.stream.read(size);self.sent+=len(data)
  if self.sent-self.last>=256*1024**2 or self.sent==self.total:
   self.last=self.sent;state={'name':self.file.name,'bytes_sent':self.sent,'bytes_total':self.total};(O/'vm-media-progress.json').write_text(json.dumps(state),encoding='utf-8');print('UPLOAD',self.file.name,round(self.sent/self.total*100,1),'%',flush=True)
  return data
 def tell(self):return self.stream.tell()
 def close(self):self.stream.close()
for path in ([pathlib.Path(p) for p in sys.argv[1:]] or [pathlib.Path(r'D:\VM ISO\ubuntu-24.04.4-desktop-amd64.iso'),pathlib.Path(r'D:\VM ISO\Windows.iso')]):
 items=s.get(BASE+P+'/isos',timeout=30).json()
 if any(i['name']==path.name for i in items):print('EXISTS',path.name,flush=True);continue
 source=Progress(path)
 try:r=s.post(BASE+P+'/isos',params={'name':path.name},data=source,headers={'Content-Type':'application/octet-stream'},timeout=(20,300))
 finally:source.close()
 assert r.status_code==201,(r.status_code,r.text[:1500]);print('PASS uploaded and verified',path.name,r.json()['sha256'],flush=True)
items=s.get(BASE+P+'/isos',timeout=30).json();(O/'vm-media-registry.json').write_text(json.dumps(items,indent=2),encoding='utf-8')
