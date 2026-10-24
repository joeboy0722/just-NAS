"""Read-only final smoke against delivered ISO with pinned HTTPS trust."""
import pathlib,json,requests,sys,time,subprocess
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';CA=str(OUT/'just-nas-certificate.pem');ROLLBACK='--rollback' in sys.argv;creds=json.loads((OUT/'test-credentials.json').read_text());accounts=json.loads((OUT/'family-test-accounts.json').read_text());checks=[]
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/('family-rollback-smoke.json' if ROLLBACK else 'family-final-smoke.json')).write_text(json.dumps(checks,indent=2));print('PASS:',name,flush=True)
for i in range(120):
 try:
  r=requests.get(BASE+'/api/auth/status',verify=CA,timeout=2)
  if r.status_code==200 and r.json()['initialized']:break
 except requests.RequestException:pass
 time.sleep(2)
else:raise RuntimeError('Pinned HTTPS readiness failed')
admin=requests.Session();admin.verify=CA;time.sleep(1.2);r=admin.post(BASE+'/api/auth/login',json={'password':creds['admin']});assert r.status_code==200,r.text;admin.headers['X-CSRF-Token']=r.json()['csrf']
def get(p):
 r=admin.get(BASE+p,timeout=60);assert r.status_code==200,(p,r.status_code);return r.json()
assert get('/api/tls')['enabled'];assert next(p for p in get('/api/storage/pools') if p['name']=='homepool')['health']=='ONLINE';record('Delivered ISO boots with persistent HTTPS, accounts and healthy repaired mirror')
assert get('/api/network')['settings']['address']=='192.168.56.101/24';record('Confirmed static IP persists through final image replacement')
family=requests.Session();family.verify=CA;r=family.post(BASE+'/files-api/login',json={'name':'family','password':accounts['family']});assert r.status_code==200
spaces=family.get(BASE+'/files-api/spaces').json()['spaces'];assert {s['name'] for s in spaces}=={'Family','FamilyHome'};record('Family portal permissions persist across image update')
assert family.get(BASE+'/api/users').status_code==401 and admin.get(BASE+'/files-api/spaces').status_code==401;record('Administrator and family sessions remain isolated')
r=requests.get('http://192.168.56.101:8080/',allow_redirects=False,timeout=10);assert r.status_code==307;record('Plain HTTP still redirects to HTTPS after reboot')
sys.path.insert(0,str(ROOT/'output/test-deps'));import smbclient
kw={'username':'family','password':accounts['family']}
with smbclient.open_file(r'\\192.168.56.101\Family\proof.txt','rb',**kw) as f:assert f.read()==b'family shared'
volume=smbclient.stat_volume(r'\\192.168.56.101\Family',**kw);assert volume.total_size>0;record('Windows-compatible SMB reads original data and reports share capacity')
try:
 with smbclient.open_file(r'\\192.168.56.101\AliceHome\proof.txt','rb',**kw) as f:f.read()
except OSError as e:assert getattr(e,'ntstatus',None)==0xc0000022 or getattr(e,'errno',None)==13,(type(e).__name__,str(e))
else:raise AssertionError('Family accessed Alice private data')
record('SMB private data isolation persists after mirror replacement and image update')
VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True)
mounted=json.loads(next(line.split('=',1)[1] for line in info.splitlines() if line.startswith('"IDE-0-0"=')));assert pathlib.Path(mounted).samefile(ROOT/next((a for a in sys.argv[1:] if a.endswith('.iso')),('output/previous-family-nas-rc1.iso' if ROLLBACK else 'output/alpine-nas-full.iso'))),mounted
(OUT/('vm-rollback-config.txt' if ROLLBACK else 'vm-final-config.txt')).write_text(info,encoding='utf-8');record('VM is running the compatible previous ISO' if ROLLBACK else 'VM is running the delivered canonical ISO')
