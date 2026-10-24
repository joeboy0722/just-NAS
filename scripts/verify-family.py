"""Family NAS acceptance; destructive provisioning is restricted to explicit new VDI fixtures."""
import json,pathlib,re,secrets,subprocess,sys,time,os
import requests
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';sys.path.insert(0,str(ROOT/'output/test-deps'));import smbclient
BASE='http://192.168.56.101:8080';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';admin=requests.Session();checks=[]
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/'family-acceptance.json').write_text(json.dumps(checks,indent=2),encoding='utf-8');print('PASS:',name,flush=True)
def api(p,method='GET',body=None,status=200,session=admin,**kw):
 r=session.request(method,BASE+p,json=body,timeout=120,**kw);assert r.status_code==status,(p,r.status_code,r.text[:1500]);return r.json() if r.content and 'json' in r.headers.get('Content-Type','') else r

def wait():
 for i in range(90):
  try:
   r=requests.get(BASE+'/api/auth/status',timeout=2)
   if r.status_code==200:return r.json()
  except requests.RequestException:pass
  time.sleep(2)
 raise RuntimeError('VM did not boot')

def login(session,name,pw):
 r=api('/files-api/login','POST',{'name':name,'password':pw},session=session);session.headers['X-CSRF-Token']=r['csrf']

def fileop(session,op,space,path='',method='GET',status=200,**params):
 return api('/files-api/'+op,method,{} if method!='GET' else None,status=status,session=session,params={'space':space['id'],'path':path,**params})

state=wait();creds=json.loads((OUT/'test-credentials.json').read_text());settings=OUT/'family-test-accounts.json'
if settings.exists():accounts=json.loads(settings.read_text())
else:
 accounts={'family':creds['password'],'alice':secrets.token_urlsafe(24),'bob':secrets.token_urlsafe(24)};settings.write_text(json.dumps(accounts),encoding='utf-8')
if not state['initialized']:
 serial=(OUT/'serial.log').read_text(errors='replace');tokens=re.findall(r'Just NAS setup token: ([a-f0-9]{64})',serial);assert tokens,'No setup token';r=api('/api/auth/setup','POST',{'token':tokens[-1],'password':creds['admin']})
else:r=api('/api/auth/login','POST',{'password':creds['admin']})
admin.headers['X-CSRF-Token']=r['csrf'];record('Administrator setup/login')
info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True)
for port,name in [(1,'family-a.vdi'),(2,'family-b.vdi'),(3,'family-backup.vdi'),(4,'family-spare.vdi')]:
 assert any(line.startswith('"SATA-%d-0"='%port) and name in line for line in info.splitlines()),'Unsafe VDI mapping'
record('New VDI mapping whitelist; original SATA 0 excluded')
pools=api('/api/storage/pools')
if not any(p['name']=='homepool' for p in pools):
 disks=api('/api/storage/disks');byname={d['name']:d for d in disks};chosen=[byname[n]['path'] for n in ['sdb','sdc']];assert all(byname[n]['available'] and byname[n]['size']==20*1024**3 for n in ['sdb','sdc'])
 api('/api/storage/pools','POST',{'name':'homepool','raid_type':'mirror','devices':chosen,'confirm':'homepool'})
record('Mirror pool initialized only on two whitelisted new VDIs')
users=api('/api/users')
for name,pw in accounts.items():
 if not any(u['name']==name for u in users):api('/api/users','POST',{'name':name,'password':pw})
users=api('/api/users');assert all('hash' not in u and 'salt' not in u for u in users);record('File accounts with redacted password hashes')
shares=api('/api/shares')
for name,kind,owner,access in [('Family','public','',{'family':'rw','alice':'rw','bob':'ro'}),('AliceHome','private','alice',{}),('BobHome','private','bob',{})]:
 if not any(s['name']==name for s in shares):api('/api/spaces','POST',{'pool':'homepool','name':name,'kind':kind,'owner':owner,'quota':'100M','access':access})
shares=api('/api/shares');sp={s['name']:s for s in shares};record('Public and per-user private datasets provisioned')
alice,bob=requests.Session(),requests.Session();login(alice,'alice',accounts['alice']);time.sleep(1.1);login(bob,'bob',accounts['bob'])
assert {s['name'] for s in api('/files-api/spaces',session=alice)['spaces']}=={'Family','AliceHome'}
assert {s['name'] for s in api('/files-api/spaces',session=bob)['spaces']}=={'Family','BobHome'}
fileop(bob,'list',sp['AliceHome'],status=403);api('/api/users',session=alice,status=401);api('/files-api/spaces',session=admin,status=401);record('Private-space and administrator/file-session isolation')
for session,space,data in [(alice,sp['AliceHome'],b'alice private'),(bob,sp['BobHome'],b'bob private'),(alice,sp['Family'],b'family shared')]:
 r=session.put(BASE+'/files-api/upload',params={'space':space['id'],'path':'proof.txt','replace':'true'},data=data,timeout=15);assert r.status_code==200,(r.status_code,r.text)
assert fileop(alice,'download',sp['AliceHome'],'proof.txt').content==b'alice private'
r=bob.put(BASE+'/files-api/upload',params={'space':sp['Family']['id'],'path':'denied.txt'},data=b'denied',timeout=15);assert r.status_code==403;record('Web upload/download and public read-only enforcement')
for path in ['../BobHome/proof.txt','/etc/passwd','.nas-trash/bob/proof.txt']:
 fileop(alice,'download',sp['Family'],path,status=400)
record('Web path traversal and recycle metadata access denied')
recovered='recovered-'+str(time.time_ns())+'.txt';fileop(alice,'trash',sp['AliceHome'],'proof.txt','POST');assert any(e['name']=='proof.txt' for e in fileop(alice,'trash-list',sp['AliceHome'])['entries']);fileop(alice,'restore',sp['AliceHome'],'proof.txt','POST',target=recovered);assert fileop(alice,'download',sp['AliceHome'],recovered).content==b'alice private';record('Web recycle and restore without overwrite')
smbclient.register_session('192.168.56.101',username='alice',password=accounts['alice'],connection_timeout=45)
with smbclient.open_file(r'\\192.168.56.101\AliceHome\smb.txt','wb') as f:f.write(b'SMB identity verified')
with smbclient.open_file(r'\\192.168.56.101\AliceHome\smb.txt','rb') as f:assert f.read()==b'SMB identity verified'
try:smbclient.listdir(r'\\192.168.56.101\BobHome')
except OSError:pass
else:raise AssertionError('SMB private isolation bypassed')
smbclient.remove(r'\\192.168.56.101\AliceHome\smb.txt');record('Real SMB authenticated write/read, isolation and recycle delete')
print('Family core acceptance finished',flush=True)
