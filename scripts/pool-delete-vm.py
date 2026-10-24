"""Prepare an isolated pool-deletion VM fixture; initialize only the new whitelisted VDI."""
from pathlib import Path
import requests,subprocess,json,time,re,sys
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'output/pool-delete-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
C=json.loads((ROOT/'output/vm-test/test-credentials.json').read_text());S=requests.Session();S.verify=str(ROOT/'output/vm-test/just-nas-certificate.pem')
checks=[]
def record(name):
 checks.append(name);(OUT/'acceptance.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding='utf8');print('PASS',name,flush=True)
def login():
 for i in range(90):
  try:
   r=S.post(BASE+'/api/auth/login',json={'password':C['admin']},timeout=3)
   if r.status_code==200:S.headers['X-CSRF-Token']=r.json()['csrf'];return
  except requests.RequestException:pass
  time.sleep(1)
 raise RuntimeError('NAS did not become ready')
def api(p,method='GET',body=None):
 r=S.request(method,BASE+'/api/'+p,json=body,timeout=115);assert r.ok,(p,r.status_code,r.text);return r.json()
def job(kind,resource):
 for i in range(60):
  r=S.post(BASE+'/api/tasks',json={'kind':kind,'resource':resource},timeout=115)
  if r.status_code==202:break
  assert '進行中' in r.text or '此資源已有' in r.text,r.text;time.sleep(1)
 else:raise RuntimeError('Task queue busy')
 ident=r.json()['id']
 for i in range(100):
  j=next(x for x in api('tasks') if x['id']==ident)
  if j['state'] not in ['running','queued']:return j
  time.sleep(.3)
 raise RuntimeError('Fixture job did not complete')
login()
if len(sys.argv)>1 and sys.argv[1]=='verify':
 fixture=json.loads((OUT/'fixture.json').read_text(encoding='utf8'));pool=fixture['pool'];ids=set(fixture['share_ids']);device=fixture['device']
 def related(x):
  r=x['resource'];k=x['kind']
  if k=='permissions':return r.split('|',1)[0] in ids
  if k=='replace':return r.split('|',1)[0]==pool
  if k=='backup-restore':return r.split(':',1)[-1].startswith(pool+'/')
  return r==pool or r.startswith(pool+'/') or (k=='smart-short' and r==device)
 assert pool not in [x['name'] for x in api('storage/pools')]
 assert not any(x['name']==pool or x['name'].startswith(pool+'/') for x in api('storage/datasets'))
 assert not any(x['name'].startswith(pool+'/') for x in api('storage/snapshots'))
 assert not any(x['id'] in ids for x in api('shares'))
 assert not any(related(x) for x in api('tasks'))
 assert not any(related(x) for x in api('schedules'))
 assert not any(pool in x['ID'] for x in api('alerts'))
 before=json.loads((OUT/'settings-before.json').read_text(encoding='utf8'))
 assert before['users']==api('users'),'Global accounts changed'
 assert before['backup']==api('backup'),'Unrelated backup settings changed'
 current=api('shares');normalize=lambda ss:sorted([{**x,'users':sorted(x.get('users',[]))} for x in ss],key=lambda x:x['id'])
 assert normalize(before['shares'])==normalize(current),'Other pool shares changed'
 remaining={x['id'] for x in api('schedules')};assert all(x['id'] in remaining for x in before['schedules']),'Other pool schedules removed'
 assert all(x['health']=='ONLINE' for x in api('storage/pools'))
 (OUT/'verification.json').write_text(json.dumps({'pool_removed':pool,'shares_removed':len(ids),'settings_preserved':True,'tasks_and_schedules_removed':True},ensure_ascii=False,indent=2),encoding='utf8')
 print('PASS Deleted pool, datasets, snapshots, shares, jobs, schedules and alerts absent',flush=True)
 print('PASS Existing accounts, other-pool shares/schedules, backups and ZFS health preserved',flush=True)
 sys.exit(0)
assert not (OUT/'fixture.json').exists(),'Refusing to reuse an existing fixture'
path=(OUT/'cascade-delete-fixture.vdi').resolve();assert path.is_relative_to(ROOT/'output')
info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True)
mapped=json.loads(next(line.split("=",1)[1] for line in info.splitlines() if line.startswith('"SATA-2-0"=')))
assert Path(mapped).resolve()==path,'VDI mapping is not the whitelisted test fixture'
medium=subprocess.check_output([VBOX,'showmediuminfo',str(path)],text=True)
uuid=re.search(r'^UUID:\s+(\S+)',medium,re.M).group(1)
options=api('storage/disks');disks=[d for d in options if uuid[:8].lower() in d['path'].lower()]
assert len(disks)==1 and disks[0]['size']==512*1024**2 and (disks[0]['available'] or (OUT/'settings-before.json').is_file()),disks
device=disks[0]['path'];record('Only the new 512 MiB VDI is whitelisted for initialization')
pool='CascadeDeleteTest'
exists=any(x['name']==pool for x in api('storage/pools'))
if exists:
 members=[m for m in api('ui/storage-options')['pool_members'] if m['pool']==pool]
 assert members and all(uuid[:8].lower() in m['path'].lower() for m in members),'Refusing to reuse a pool outside the test VDI'
 assert (OUT/'settings-before.json').is_file(),'Missing original settings baseline'
else:
 assert not any(x['name'] in ['CascadeFiles','CascadeNFS'] for x in api('shares'))
 before={p:api(p) for p in ['users','shares','backup','schedules','storage/datasets']};(OUT/'settings-before.json').write_text(json.dumps(before,ensure_ascii=False,indent=2),encoding='utf8')
 api('storage/pools','POST',{'name':pool,'raid_type':'stripe','devices':[device],'confirm':pool})
record('Created or strictly verified isolated test storage pool')
if not any(x['name']=='CascadeFiles' for x in api('shares')):api('spaces','POST',{'pool':pool,'name':'CascadeFiles','kind':'public','quota':'16M','access':{C['user']:'rw'}})
if not any(x['name']==pool+'/NFSFiles' for x in api('storage/datasets')):api('storage/datasets','POST',{'name':pool+'/NFSFiles','compression':'lz4'})
if not any(x['name']=='CascadeNFS' for x in api('shares')):api('shares','POST',{'name':'CascadeNFS','protocol':'nfs','path':'/mnt/'+pool+'/NFSFiles','allowed_hosts':'192.168.56.1/32','read_only':False})
ss=[x for x in api('shares') if x['path'].startswith('/mnt/'+pool+'/')];smb=next(x for x in ss if x['protocol']=='smb')
portal=requests.Session();portal.verify=S.verify;r=portal.post(BASE+'/files-api/login',json={'name':C['user'],'password':C['password']});r.raise_for_status();portal.headers['X-CSRF-Token']=r.json()['csrf'];r=portal.put(BASE+'/files-api/upload',params={'space':smb['id'],'path':'fixture.txt'},data=b'isolated pool deletion proof');r.raise_for_status()
record('Created SMB/NFS shares, managed space and test file')
for kind,resource in [('quota',pool+'/CascadeFiles'),('snapshot-daily',pool+'/CascadeFiles'),('permissions',smb['id']+'|'+json.dumps({C['user']:'rw'})),('scrub',pool),('smart-short',device),('snapshot-hourly',pool+'/Missing')]:
 result=job(kind,resource);print('Fixture task:',kind,result['state'],flush=True)
for kind,resource in [('scrub',pool),('smart-short',device)]:api('schedules','POST',{'kind':kind,'resource':resource,'every_seconds':86400,'enabled':False})
record('Created historical tasks, snapshot/quota schedules and physical-disk SMART schedule')
preview=api('storage/pool-deletion?name='+pool)
assert len(preview['shares'])==2 and preview['jobs']>=6 and preview['schedules']==6,preview
fixture={'pool':pool,'device':device,'share_ids':[x['id'] for x in ss],'preview':preview}
(OUT/'fixture.json').write_text(json.dumps(fixture,ensure_ascii=False,indent=2),encoding='utf8');record('Deletion preview accounts for both shares and all six task/schedule references')
print('READY for browser deletion',flush=True)
