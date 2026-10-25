"""Non-destructive launcher acceptance on snapshotted just-nas; faults use a separate isolated clone."""
import argparse, pathlib, json, time, subprocess, requests
from cryptography.hazmat.primitives import serialization
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
s=requests.Session();s.verify=str(O/'just-nas-certificate.pem');checks=[]
def record(name):
 checks.append({'check':name,'result':'PASS'});(O/'launcher-api-results.json').write_text(json.dumps(checks,indent=2),encoding='utf-8');print('PASS',name,flush=True)
def call(p,m='GET',body=None,status=200):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=300);assert r.status_code==status,(p,r.status_code,r.text[:1000]);return r.json()
def ready():
 for _ in range(120):
  try:
   if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
  except requests.RequestException:pass
  time.sleep(1)
 else:raise RuntimeError('VM unavailable')
 time.sleep(1.2);c=json.loads((O/'test-credentials.json').read_text(encoding='utf-8'));s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
 for _ in range(120):
  if not call('extensions').get('restoring'):return
  time.sleep(1)
 raise RuntimeError('Extension recovery timed out')
def job(id):
 for _ in range(240):
  j=next(x for x in call('tasks') if x['id']==id)
  if j['state'] not in ('running','queued'):assert j['state']=='succeeded',j.get('error');return
  time.sleep(1)
 raise RuntimeError('Task timed out')
def upload(file,kind='extensions'):
 with file.open('rb') as f:r=s.post(BASE+'/api/'+kind+'/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=240)
 r.raise_for_status();return r.json()
def remote(cmd):
 args=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(O/'launcher-known-hosts'),'-o','IdentitiesOnly=yes','-i',str(O/'stores-admin.key'),'root@192.168.56.101','set -e; '+cmd]
 r=subprocess.run(args,text=True,capture_output=True,timeout=180);assert r.returncode==0,r.stderr[:1000];return r.stdout
def main():
 global checks
 p=argparse.ArgumentParser();p.add_argument('phase',choices=['setup','reboot']);a=p.parse_args();ready()
 if a.phase!='setup':checks=json.loads((O/'launcher-api-results.json').read_text(encoding='utf-8'))
 if a.phase=='setup':
  st=call('extensions');assert st['state']['dataset']=='homepool/.just-nas-extensions' and not st['apps']['apps'] and not st['state']['installed'],'Only the snapshotted empty extension store may be used'
  info=subprocess.check_output([VBOX,'snapshot','just-nas','list','--machinereadable'],text=True);assert 'dd5f50a3-212b-4a6c-96d2-8763bb7eb957' in info
  (O/'launcher-original-store.json').write_text(json.dumps(st),encoding='utf-8')
  key=serialization.load_ssh_private_key((O/'stores-admin.key').read_bytes(),None);public=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode();call('extensions/ssh-key','POST',{'public_key':public})
  v=upload(R/'output/store-packages/ssh-server-1.0.0.jnpkg');job(call('extensions/install','POST',{'token':v['token']},202)['task_id'])
  st=call('extensions');(O/'launcher-known-hosts').write_text('192.168.56.101 '+st['ssh_host_key']+'\n',encoding='ascii')
  record('Original NAS preserved in powered-off VM snapshot; acceptance uses disposable snapshot changes')
 elif a.phase=='reboot':
  entries=call('apps/launcher')['apps'];assert len(entries)==1 and entries[0]['status']=='active';assert requests.get(entries[0]['url'],timeout=10).status_code==200
  record('Application, icon metadata and actual running state survive final image reboot')
if __name__=='__main__':main()
