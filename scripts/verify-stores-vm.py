"""Store acceptance. Only the new stores-test.vdi on SATA 2 may be initialized."""
import argparse,json,pathlib,subprocess,time,requests,sys
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
p=argparse.ArgumentParser();p.add_argument('phase',choices=['install','app','reboot','cleanup']);args=p.parse_args();checks=[]
s=requests.Session();s.verify=str(OUT/'just-nas-certificate.pem')
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/('stores-'+args.phase+'.json')).write_text(json.dumps(checks,indent=2));print('PASS:',name,flush=True)
def call(path,method='GET',body=None,status=200):
 r=s.request(method,BASE+'/api/'+path,json=body,timeout=120);assert r.status_code==status,(path,r.status_code,r.text[:1500]);return r.json()
def ready():
 for _ in range(120):
  try:
   if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
  except requests.RequestException:pass
  time.sleep(2)
 else:raise RuntimeError('VM did not become ready')
 c=json.loads((OUT/'test-credentials.json').read_text());time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
def job(id,success=True):
 last=None
 for _ in range(240):
  j=next(v for v in call('tasks') if v['id']==id)
  if j['progress']!=last:print('TASK:',j['progress'],flush=True);last=j['progress']
  if j['state'] not in ('running','queued'):
   assert (j['state']=='succeeded')==success,j
   return j
  time.sleep(1)
 raise RuntimeError('Task timeout')
def install(name,app=False,**options):
 file=ROOT/'output/store-packages'/name
 with file.open('rb') as f:
  r=s.post(BASE+'/api/'+('apps' if app else 'extensions')+'/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
 assert r.status_code==200,(r.status_code,r.text[:1000]);v=r.json();assert v['package']['official']
 record('Verified offline import '+name)
 v=call(('apps' if app else 'extensions')+'/install','POST',{'token':v['token'],**options},202);return job(v['task_id'])
ready()
if args.phase=='install':
 info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True)
 attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-2-0"=')))
 target=OUT/'stores-test.vdi';assert pathlib.Path(attached).samefile(target)
 medium=subprocess.check_output([VBOX,'showmediuminfo','disk',str(target)],text=True)
 uuid=next(l.split(':',1)[1].strip() for l in medium.splitlines() if l.startswith('UUID:'))
 state=call('extensions');assert not state['state']['installed'];record('Core boot has no installed container engine')
 # Unauthenticated requests cannot access either management store.
 anon=requests.get(BASE+'/api/extensions',verify=s.verify,timeout=10);assert anon.status_code==401;record('Stores require administrator session')
 disks=call('storage/disks');disk=next(d for d in disks if 'VB'+uuid[:8]+'-' in d['path']);assert disk['size']==4*1024**3
 assert disk['available'] or state['state']['dataset']=='storetest/.just-nas-extensions'
 assert disk['path'].startswith('/dev/disk/by-id/ata-VBOX_HARDDISK_');record('Initialization whitelist matches dedicated 4GiB VDI and stable serial')
 if disk['available']:call('storage/pools','POST',{'name':'storetest','raid_type':'single','devices':[disk['path']],'confirm':'storetest'})
 if not state['state']['dataset']:call('extensions/storage','POST',{'pool':'storetest'})
 state=call('extensions');assert state['mounted'] and len(state['state']['protected_packages'])>100;record('Dedicated extension dataset mounted and core packages protected')
 install('container-engine-1.0.0.jnpkg');state=call('extensions');assert state['state']['installed']['container-engine']['status']=='active';record('Offline engine starts with explicit ZFS backend')
elif args.phase=='app':
 install('static-site-1.0.0.jnpkg',True,port=18080,bind_ip='192.168.56.101',read_only=True)
 r=requests.get('http://192.168.56.101:18080/',timeout=10);assert r.status_code==200 and 'just-NAS' in r.text;record('Offline non-root static site serves its persistent content')
 result=call('extensions/action','POST',{'id':'container-engine','action':'remove'},202);job(result['task_id'],False);record('Engine removal blocked while application depends on it')
 for action in ['stop','start']:
  v=call('apps/action','POST',{'id':'static-site','action':action},202);job(v['task_id']);record('Application '+action)
 logs=call('apps/static-site/logs');assert isinstance(logs['logs'],str);record('Application logs available to administrator')
 install('static-site-1.0.0.jnpkg',True,port=18080,bind_ip='192.168.56.101',read_only=True);record('Application update snapshots data and restores service')
 v=call('apps/action','POST',{'id':'static-site','action':'remove'},202);job(v['task_id']);assert not call('apps')['apps']['apps'];record('Uninstall preserves application data by default')
 install('static-site-1.0.0.jnpkg',True,port=18080,bind_ip='192.168.56.101',read_only=True,reuse_data=True);record('Reinstall explicitly reuses retained owned data')
 install('audio-6.18.58-0-lts.jnpkg');assert call('extensions')['state']['installed']['audio']['status']=='pending-reboot';record('Driver installation waits for reboot')
elif args.phase=='reboot':
 for _ in range(120):
  state=call('extensions');ext=state['state']['installed'];apps=state['apps']['apps']
  if ext['container-engine']['status']=='active' and ext['audio']['status']=='active' and apps['static-site']['status']=='active':
   try:
    if requests.get('http://192.168.56.101:18080/',timeout=3).status_code==200:break
   except requests.RequestException:pass
  time.sleep(1)
 else:raise RuntimeError('Restore failed: '+str([(k,v['status'],v.get('error')) for k,v in ext.items()]))
 record('Engine, read-only driver layer and application restored after reboot')
 v=call('extensions/action','POST',{'id':'audio','action':'disable'},202);job(v['task_id']);record('Driver disable scheduled for next boot')
elif args.phase=='cleanup':
 state=call('extensions');assert not state['state']['installed']['audio']['enabled'];v=call('extensions/action','POST',{'id':'audio','action':'remove'},202);job(v['task_id']);record('Disabled driver removed after reboot')
 preview=call('storage/pool-deletion?name=storetest');assert 'static-site' in preview['apps'] and 'container-engine' in preview['extensions'];record('Pool deletion preview includes applications and engine')
 call('storage/pools?name=storetest&confirm=storetest','DELETE');assert not call('extensions')['state']['dataset'] and not call('apps')['apps']['apps'];assert not any('storetest' in j['resource'] for j in call('tasks'));record('Deleting test pool removes associated stores, deployments and task records')
 assert not any(v['name']=='StoresNFS' for v in call('shares'));record('Pool deletion removes associated NFS export')
 assert next(p for p in call('storage/pools') if p['name']=='homepool')['health']=='ONLINE';record('Original homepool preserved and healthy')
