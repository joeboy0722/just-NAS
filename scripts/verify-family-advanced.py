"""Additional live VM acceptance. Only dedicated homepool test datasets are created."""
import sys,json,pathlib,time,os,secrets,hashlib
import requests
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';sys.path.insert(0,str(ROOT/'output/test-deps'));import smbclient
from smbprotocol.exceptions import SMBResponseException
BASE='http://192.168.56.101:8080';creds=json.loads((OUT/'test-credentials.json').read_text());accounts=json.loads((OUT/'family-test-accounts.json').read_text());admin=requests.Session();results=[]
def record(s):
 results.append({'check':s,'result':'PASS'});(OUT/('family-advanced-'+phase+'.json')).write_text(json.dumps(results,indent=2),encoding='utf-8');print('PASS:',s,flush=True)
def call(path,method='GET',body=None,status=200,session=admin,**kwargs):
 r=session.request(method,BASE+path,json=body,timeout=120,**kwargs);assert r.status_code==status,(path,r.status_code,r.text[:1500]);return r.json() if 'json' in r.headers.get('Content-Type','') else r
for attempt in range(90):
 try:
  ready=requests.get(BASE+'/api/auth/status',timeout=2)
  if ready.status_code==200 and ready.json().get('initialized'):break
 except requests.RequestException:pass
 time.sleep(2)
else:raise RuntimeError('NAS did not become ready')
r=call('/api/auth/login','POST',{'password':creds['admin']});admin.headers['X-CSRF-Token']=r['csrf'];phase=sys.argv[1] if len(sys.argv)>1 else 'backup'
def filelogin(name):
 time.sleep(1.1);s=requests.Session();r=call('/files-api/login','POST',{'name':name,'password':accounts[name]},session=s);s.headers['X-CSRF-Token']=r['csrf'];return s

def job(kind,resource):
 for i in range(60):
  r=admin.post(BASE+'/api/tasks',json={'kind':kind,'resource':resource},timeout=120)
  if r.status_code==202:break
  assert r.status_code==400 and '任務' in r.text,(r.status_code,r.text)
  time.sleep(1)
 else:raise RuntimeError('Task slot unavailable')
 identity=r.json()['id']
 for i in range(180):
  j=next(x for x in call('/api/tasks') if x['id']==identity)
  if j['state']=='succeeded':return j
  assert j['state']=='running',j
  time.sleep(2)
 raise RuntimeError('Task timeout')

if phase=='backup':
 uuid=(OUT/'family-backup.uuid').read_text().strip();mount=call('/api/backup/mount','POST',{'uuid':uuid})['mount'];record('Independent preformatted test VDI mounted by UUID')
 settings=OUT/'family-backup-test.key'
 password=settings.read_text() if settings.exists() else secrets.token_urlsafe(32)
 settings.write_text(password)
 call('/api/backup','POST',{'mount':mount,'password':password,'datasets':['homepool/AliceHome','homepool/Family']});record('Restic repository initialized with UUID and repository identity checks')
 j=job('backup','backup');record('Snapshot-based encrypted data and settings backup')
 snapshots=call('/api/backup/snapshots');assert snapshots and snapshots[-1]['id'];record('Encrypted backup version listing')
 job('backup-check','backup');record('Full repository read-data verification')
 datasets=call('/api/storage/datasets')
 if not any(d['name']=='homepool/RestoreTest' for d in datasets):call('/api/storage/datasets','POST',{'name':'homepool/RestoreTest','compression':'lz4'})
 j=job('backup-restore',snapshots[-1]['id']+':homepool/RestoreTest');record('Restore into a new directory with Restic content verification')
 export=call('/api/settings/export','POST',{'password':password});data=export.content if hasattr(export,'content') else json.dumps(export).encode();(OUT/'family-settings-export.enc').write_bytes(data);assert b'smbpasswd' not in data;record('Encrypted settings export contains no plaintext account database')

elif phase=='quota':
 shares=call('/api/shares')
 if not any(s['name']=='QuotaTest' for s in shares):call('/api/spaces','POST',{'pool':'homepool','name':'QuotaTest','kind':'private','owner':'alice','quota':'200M'})
 space=next(s for s in call('/api/shares') if s['name']=='QuotaTest');call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'200M'});alice=filelogin('alice');data=os.urandom(8*1024*1024);proof='quota-proof-'+str(time.time_ns())+'.bin';params={'space':space['id'],'path':proof,'replace':'true'}
 r=alice.put(BASE+'/files-api/upload',params=params,data=data,timeout=60);assert r.status_code==200,r.text
 before=hashlib.sha256(data).hexdigest();call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'1M'});p=call('/api/storage/dataset-properties?name=homepool/QuotaTest');assert p['org.just-nas:quota-locked']=='on',p
 r=alice.get(BASE+'/files-api/download',params=params,timeout=60);assert r.status_code==200 and hashlib.sha256(r.content).hexdigest()==before;record('Lower quota below usage preserves every original byte and permits download')
 r=alice.put(BASE+'/files-api/upload',params={'space':space['id'],'path':'denied.bin'},data=b'denied',timeout=15);assert r.status_code==403,r.text;record('Over-quota web writes denied')
 cache={};smbclient.register_session('192.168.56.101',username='alice',password=accounts['alice'],connection_timeout=45,connection_cache=cache)
 with smbclient.open_file(r'\\192.168.56.101\QuotaTest'+'\\'+proof,'rb',username='alice',password=accounts['alice'],connection_cache=cache) as f:assert hashlib.sha256(f.read()).hexdigest()==before
 try:
  with smbclient.open_file(r'\\192.168.56.101\QuotaTest'+'\\'+proof,'r+b',username='alice',password=accounts['alice'],connection_cache=cache) as f:f.write(b'x')
 except (OSError,SMBResponseException) as e:assert getattr(e,"ntstatus",getattr(e,"status",None)) in [0xc0000022,0xc000007f,0xc0000044],str(e)
 else:raise AssertionError('Over-quota SMB write was accepted')
 record('Over-quota SMB modification denied')
 kw=dict(username='alice',password=accounts['alice'],connection_cache=cache)
 with smbclient.open_file(r'\\192.168.56.101\QuotaTest'+'\\'+proof,'rb',**kw) as f:assert hashlib.sha256(f.read()).hexdigest()==before
 try:
  with smbclient.open_file(r'\\192.168.56.101\QuotaTest\new-must-deny.bin','wb',**kw) as f:f.write(b'new data')
 except (OSError,SMBResponseException) as e:assert getattr(e,'ntstatus',getattr(e,'status',None)) in [0xc0000022,0xc000007f,0xc0000044]
 else:raise AssertionError('New SMB file accepted while over quota')
 record('Over-quota SMB new-file creation denied without changing original content')
 smbclient.remove(r'\\192.168.56.101\QuotaTest'+'\\'+proof,**kw);record('Over-quota SMB deletion remains available')

 start=int(p['org.just-nas:over-since']);assert abs(time.time()-start)<60;record('Persistent over-quota start recorded; grace period is 30 days')
 call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'200M'});record('Increasing quota unlocks data without deletion')
 params={'space':space['id'],'path':proof,'target':proof}
 call('/files-api/restore','POST',{},session=alice,params=params)
 r=alice.get(BASE+'/files-api/download',params=params,timeout=60);assert r.status_code==200 and hashlib.sha256(r.content).hexdigest()==before;record('Recovered original bytes after increasing quota')
 call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'1M'})
 call('/files-api/trash','POST',{},session=alice,params=params);call('/files-api/purge','POST',{},session=alice,params=params)
 for entry in call('/files-api/trash-list',session=alice,params={'space':space['id'],'path':'.'})['entries']:
  if 'quota-proof' in entry['name']:
   call('/files-api/purge','POST',{},session=alice,params={'space':space['id'],'path':entry['name']})
 for entry in call('/files-api/list',session=alice,params={'space':space['id'],'path':'.'})['entries']:
  if 'quota-proof' in entry['name']:
   par={'space':space['id'],'path':entry['name']};call('/files-api/trash','POST',{},session=alice,params=par);call('/files-api/purge','POST',{},session=alice,params=par)
 for i in range(5):
  job('quota','homepool/QuotaTest');p=call('/api/storage/dataset-properties?name=homepool/QuotaTest')
  if p['org.just-nas:quota-locked']=='off':break
  time.sleep(2)
 assert p['org.just-nas:quota-locked']=='off',p;record('Explicit deletion frees quota and returns the space to writable state')


elif phase=='accounts':
 bob=filelogin('bob');u=next(u for u in call('/api/users') if u['name']=='bob');call('/api/users/'+u['id'],'PATCH',{'disabled':True});call('/files-api/spaces',session=bob,status=401);record('Disable revokes existing file portal session')
 time.sleep(1.1);call('/files-api/login','POST',{'name':'bob','password':accounts['bob']},session=requests.Session(),status=401)
 call('/api/users/'+u['id'],'PATCH',{'disabled':False});new=secrets.token_urlsafe(24);call('/api/users/'+u['id'],'PATCH',{'password':new});time.sleep(1.1)
 call('/files-api/login','POST',{'name':'bob','password':accounts['bob']},session=requests.Session(),status=401)
 time.sleep(1.1);call('/files-api/login','POST',{'name':'bob','password':new},session=requests.Session());record('Password change rejects old password and accepts new password')
 call('/api/users/'+u['id'],'PATCH',{'password':accounts['bob']});record('Disposable test account password restored')
else:raise RuntimeError('Unknown phase')
