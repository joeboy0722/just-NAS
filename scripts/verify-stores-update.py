"""A stopped demo application must remain stopped after update; all data is on storetest."""
import pathlib,json,time,requests
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';s=requests.Session();s.verify=str(OUT/'just-nas-certificate.pem')
for _ in range(120):
 try:
  if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(2)
c=json.loads((OUT/'test-credentials.json').read_text());time.sleep(1.2);r=s.post(BASE+'/api/auth/login',json={'password':c['admin']},timeout=30);r.raise_for_status();s.headers['X-CSRF-Token']=r.json()['csrf']
def call(p,m='GET',body=None):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=120);r.raise_for_status();return r.json()
def job(id):
 for _ in range(120):
  v=next(j for j in call('tasks') if j['id']==id)
  if v['state'] not in ('running','queued'):assert v['state']=='succeeded',v.get('error');return
  time.sleep(1)
 raise RuntimeError('task timeout')
for _ in range(120):
 state=call('apps')
 if not state.get('restoring'):break
 time.sleep(1)
assert state['state']['dataset']=='storetest/.just-nas-extensions';old=state['apps']['apps']['static-site']
job(call('apps/action','POST',{'id':'static-site','action':'stop'})['task_id'])
with (ROOT/'output/store-packages/static-site-1.0.0.jnpkg').open('rb') as f:r=s.post(BASE+'/api/apps/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=120)
r.raise_for_status();token=r.json()['token'];job(call('apps/install','POST',{'token':token,'port':old['port'],'bind_ip':old['bind_ip'],'mount_dataset':old.get('mount_dataset',''),'read_only':old['read_only']})['task_id'])
a=call('apps')['apps']['apps']['static-site'];assert not a['enabled'] and a['status']=='stopped';assert a['port']==old['port'] and a['bind_ip']==old['bind_ip'];job(call('apps/action','POST',{'id':'static-site','action':'start'})['task_id']);assert requests.get('http://'+a['bind_ip']+':'+str(a['port'])+'/rollback-proof.txt',timeout=10).status_code==200
(OUT/'stores-stopped-update.json').write_text(json.dumps({'result':'PASS','checks':['Stopped application remains stopped after update','Port and IP retained','Explicit restart serves retained proof file']},indent=2));print('PASS: stopped-state update, settings retention and proof-file continuity',flush=True)
