"""Live NAS recovery and configuration acceptance against disposable homepool fixtures."""
import json,pathlib,time,sys,secrets,hashlib,requests,subprocess
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='http://192.168.56.101:8080';phase=sys.argv[1];results=[]
creds=json.loads((OUT/'test-credentials.json').read_text());accounts=json.loads((OUT/'family-test-accounts.json').read_text());admin=requests.Session()
def wait():
 for i in range(120):
  try:
   r=requests.get(BASE+'/api/auth/status',timeout=2)
   if r.status_code==200 and r.json().get('initialized'):return
  except requests.RequestException:pass
  time.sleep(2)
 raise RuntimeError('NAS readiness timeout')
def call(p,m='GET',b=None,s=admin,status=200,**kw):
 r=s.request(m,BASE+p,json=b,timeout=120,**kw);assert r.status_code==status,(p,r.status_code,r.text[:1000]);return r.json() if 'json' in r.headers.get('Content-Type','') else r

def record(name):
 results.append({'check':name,'result':'PASS'});(OUT/('family-system-'+phase+'.json')).write_text(json.dumps(results,indent=2),encoding='utf-8');print('PASS:',name,flush=True)
def login():
 global admin
 time.sleep(1.2);admin.cookies.clear();v=call('/api/auth/login','POST',{'password':creds['admin']});admin.headers['X-CSRF-Token']=v['csrf']
def filelogin(name):
 s=requests.Session();time.sleep(1.1);v=call('/files-api/login','POST',{'name':name,'password':accounts[name]},s=s);s.headers['X-CSRF-Token']=v['csrf'];return s

def job(kind,resource,success=True):
 for i in range(90):
  r=admin.post(BASE+'/api/tasks',json={'kind':kind,'resource':resource},timeout=120)
  if r.status_code==202:break
  assert r.status_code==400 and '任務' in r.text,r.text
  time.sleep(1)
 else:raise RuntimeError('Busy task slot')
 for i in range(180):
  j=next(x for x in call('/api/tasks') if x['id']==r.json()['id'])
  if j['state']!='running':
   assert (j['state']=='succeeded')==success,j
   return j
  time.sleep(2)
 raise RuntimeError('Task timeout')
wait();login()
if phase=='snapshots':
 alice=filelogin('alice');space=next(x for x in call('/api/shares') if x['name']=='AliceHome');par={'space':space['id'],'path':'history-'+str(time.time_ns())+'.txt'}
 original=b'Original historical bytes';r=alice.put(BASE+'/files-api/upload',params=par,data=original);assert r.status_code==200,r.text
 tag='manual-acceptance-'+str(time.time_ns());ds='homepool/AliceHome';call('/api/storage/snapshots','POST',{'dataset':ds,'tag':tag});job('snapshot-hourly',ds)
 r=alice.put(BASE+'/files-api/upload',params={**par,'replace':'true'},data=b'Changed bytes');assert r.status_code==200,r.text
 recovered={**par,'snapshot':ds+'@'+tag,'target':par['path']+'.recovered'};call('/files-api/recover-version','POST',{},s=alice,params=recovered)
 r=alice.get(BASE+'/files-api/download',params={**par,'path':recovered['target']});assert r.content==original;record('Single file recovered from manual snapshot into a new location with identical bytes')
 call('/files-api/recover-version','POST',{},s=alice,params=recovered,status=400);record('Snapshot recovery refuses overwrite')
 call('/files-api/recover-version','POST',{},s=alice,params={**recovered,'snapshot':'homepool/BobHome@'+tag,'target':'denied'},status=403);record('Cross-space snapshot access denied')
 # A manually named auto-like snapshot must not count toward automatic retention.
 manual='nas-auto-hourly-00000000-manual';existing=call('/api/storage/snapshots')
 if not any(x['name']==ds+'@'+manual for x in existing):call('/api/storage/snapshots','POST',{'dataset':ds,'tag':manual})
 for i in range(25):
  job('snapshot-hourly',ds);time.sleep(1.05)
 snaps=call('/api/storage/snapshots');names=[x['name'] for x in snaps];assert ds+'@'+manual in names and ds+'@'+tag in names
 assert len([n for n in names if n.startswith(ds+'@nas-auto-hourly-') and n!=ds+'@'+manual])==24
 record('Automatic hourly retention keeps 24 system snapshots and preserves manual snapshots including auto-like names')
 job('scrub','homepool');record('Mirror scrub completes and reports pool status')
elif phase=='shares':
 alice=filelogin('alice');space=next(x for x in call('/api/shares') if x['name']=='Family');par={'space':space['id'],'path':'.'}
 call('/api/shares/'+space['id'],'PATCH',{'disabled':True});call('/files-api/list',s=alice,params=par,status=403);record('Disabled share disappears from family portal and rejects existing session access')
 call('/api/shares/'+space['id'],'PATCH',{'disabled':False,'read_only':True});r=alice.put(BASE+'/files-api/upload',params={**par,'path':'deny-readonly'},data=b'x');assert r.status_code==403,r.text
 call('/api/shares/'+space['id'],'PATCH',{'read_only':False});record('Share read-only and re-enable changes apply')
 call('/api/shares/'+space['id'],'PATCH',{'name':'ShouldReject'},status=400);record('Immutable share identity update rejected')
 job('permissions',space['id']+'|'+json.dumps({'family':'rw','alice':'rw','bob':'rw'}));bob=filelogin('bob');p={'space':space['id'],'path':'bob-permission-proof-'+str(time.time_ns())};r=bob.put(BASE+'/files-api/upload',params=p,data=b'permission test');assert r.status_code==200,r.text
 job('permissions',space['id']+'|'+json.dumps({'family':'rw','alice':'rw','bob':'ro'}));r=bob.put(BASE+'/files-api/upload',params={**p,'replace':'true'},data=b'denied');assert r.status_code==403,r.text
 record('Public filesystem permission job changes Bob from read-only to writable and back')
elif phase=='settings':
 password=secrets.token_urlsafe(24);data=call('/api/settings/export','POST',{'password':password}).json();(OUT/'settings-restore-acceptance.enc').write_text(json.dumps(data));(OUT/'settings-restore-acceptance.key').write_text(password)
 body={'password':password,'data':data,'confirm':'RESTORE SETTINGS AND REBOOT'}
 call('/api/settings/import','POST',{**body,'password':'wrong-password-acceptance'},status=400);record('Encrypted settings import rejects wrong password without replacing state')
 space=next(x for x in call('/api/shares') if x['name']=='Family');original=space.get('comment','');call('/api/shares/'+space['id'],'PATCH',{'comment':'temporary-before-settings-restore'})
 call('/api/settings/import','POST',body);time.sleep(15);wait();login();space=next(x for x in call('/api/shares') if x['name']=='Family');assert space.get('comment','')==original,space
 record('Encrypted settings import restores committed generation across reboot')
 assert {'family','alice','bob'} <= {x['name'] for x in call('/api/users')};record('Accounts, shares, schedules and task history persist after reboot')
elif phase=='network':
 call('/api/network','POST',{'interface':'eth0','dhcp':False,'address':'192.168.56.101/24','dns':[]});time.sleep(5);call('/api/network/confirm','POST',{});assert not call('/api/network')['settings']['pending'];record('Static address can be applied and confirmed')
 call('/api/network','POST',{'interface':'eth0','dhcp':False,'address':'192.168.56.102/24','dns':[]});time.sleep(10)
 r=requests.get('http://192.168.56.102:8080/api/auth/status',timeout=5);assert r.status_code==200;record('Candidate network address becomes reachable')
 for i in range(55):
  time.sleep(2)
  try:
   r=admin.get(BASE+'/api/network',timeout=2)
   if r.status_code==200 and not r.json()['settings']['pending']:break
  except requests.RequestException:pass
 else:raise RuntimeError('Network rollback failed')
 assert r.json()['settings']['address']=='192.168.56.101/24',r.text;record('Unconfirmed network change automatically returns to previous static settings')
 subprocess.run([sys.executable,str(ROOT/'scripts/vm-cycle.py')],check=True);wait();login();assert call('/api/network')['settings']['address']=='192.168.56.101/24';record('Confirmed static network survives reboot following a rolled-back change')
elif phase=='files-edge':
 import os
 call('/api/storage/dataset-properties?name=homepool/AliceHome','PATCH',{'quota':'512M'});alice=filelogin('alice');space=next(x for x in call('/api/shares') if x['name']=='AliceHome');name='large-stream-'+str(time.time_ns())+'.bin';par={'space':space['id'],'path':name};digest=hashlib.sha256()
 def chunks():
  for i in range(2048):
   b=os.urandom(65536);digest.update(b);yield b
 r=alice.put(BASE+'/files-api/upload',params=par,data=chunks(),timeout=180);assert r.status_code==200,r.text
 actual=hashlib.sha256();r=alice.get(BASE+'/files-api/download',params=par,stream=True,timeout=180);assert r.status_code==200
 for b in r.iter_content(65536):actual.update(b)
 assert actual.hexdigest()==digest.hexdigest();record('128 MiB upload and download stream with matching SHA256')
 r=alice.put(BASE+'/files-api/upload',params=par,data=b'collision');assert r.status_code==400,r.text;record('Same-name upload refuses overwrite without consent')
 call('/files-api/trash','POST',{},s=alice,params=par);call('/files-api/purge','POST',{},s=alice,params=par)
 broken={**par,'path':'interrupted-'+str(time.time_ns())}
 def interrupted():
  for i in range(16):yield os.urandom(65536)
  raise RuntimeError('intentional client disconnect')
 try:alice.put(BASE+'/files-api/upload',params=broken,data=interrupted(),timeout=30)
 except RuntimeError:pass
 else:raise RuntimeError('Interrupted request did not abort')
 time.sleep(2);entries=call('/files-api/list',s=alice,params={'space':space['id'],'path':'.'})['entries'];assert not any(x['name']==broken['path'] for x in entries);record('Interrupted upload never publishes a partial destination file')
 quota=next(x for x in call('/api/shares') if x['name']=='QuotaTest');call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'200M'})
 for e in call('/files-api/list',s=alice,params={'space':quota['id'],'path':'.'})['entries']:
  if e['name']=='disk-full-proof':
   p={'space':quota['id'],'path':e['name']};call('/files-api/trash','POST',{},s=alice,params=p);call('/files-api/purge','POST',{},s=alice,params=p)
 call('/api/storage/dataset-properties?name=homepool/QuotaTest','PATCH',{'quota':'1M'})
 r=alice.put(BASE+'/files-api/upload',params={'space':quota['id'],'path':'disk-full-proof'},data=(os.urandom(65536) for _ in range(1024)));assert r.status_code==400,r.text
 entries=call('/files-api/list',s=alice,params={'space':quota['id'],'path':'.'})['entries'];assert not any(x['name']=='disk-full-proof' for x in entries);record('Quota-exhausted upload fails without publishing a partial file')
elif phase=='identity':
 name='uidtest';password=secrets.token_urlsafe(24)
 # Reserved disposable identity with no pre-existing account.
 assert not any(u['name']==name for u in call('/api/users'))
 call('/api/users','POST',{'name':name,'password':password});first=next(u for u in call('/api/users') if u['name']==name)
 time.sleep(1.2);old=requests.Session();r=call('/files-api/login','POST',{'name':name,'password':password},s=old);old.headers['X-CSRF-Token']=r['csrf']
 call('/api/users?name='+name,'DELETE');call('/files-api/spaces',s=old,status=401)
 call('/api/users','POST',{'name':name,'password':password});second=next(u for u in call('/api/users') if u['name']==name);assert second['uid']>first['uid'],(first,second)
 call('/files-api/spaces',s=old,status=401);call('/api/users?name='+name,'DELETE');record('Deleted identity UID is never reused and old sessions stay revoked after same-name account recreation')
elif phase=='nfs':
 sys.path.insert(0,str(ROOT/'output/test-deps'));import smbclient,os
 ds='homepool/LinuxShare';path='/mnt/'+ds
 if not any(d['name']==ds for d in call('/api/storage/datasets')):call('/api/storage/datasets','POST',{'name':ds,'compression':'lz4'})
 shares=call('/api/shares')
 for share in [{'name':'LinuxFiles','protocol':'smb','path':path,'users':['family'],'read_only':False},{'name':'Linux','protocol':'nfs','path':path,'allowed_hosts':'192.168.56.1/32','read_only':False}]:
  if not any(x['name']==share['name'] for x in shares):call('/api/shares','POST',share)
 with smbclient.open_file(r'\\192.168.56.101\LinuxFiles\proof.txt','wb',username='family',password=accounts['family']) as f:f.write(b'just-nas VM acceptance: original file\n')
 env={**os.environ,'NAS_TEST_HOST':'192.168.56.101','NAS_TEST_NFS_PORT':'2049','NAS_TEST_NFS_CLIENT':'192.168.56.1','NAS_TEST_NFS_PATH':path+'/proof.txt'}
 subprocess.run([sys.executable,str(ROOT/'scripts/nfs_probe.py')],env=env,check=True);record('NFSv4 actual read, committed write and original-byte restoration')
elif phase=='backup-missing':
 j=job('backup','backup',success=False);assert any(x in j['error'].lower() for x in ['mount','uuid','掛載','裝置']),j
 record('Absent backup VDI fails safely instead of writing into an empty mount directory')
else:raise RuntimeError('Unknown acceptance phase')
