"""Real NAS VM app bootstrap; pool creation only on the explicit 128 GiB test VDI UUID."""
import argparse,pathlib,json,time,requests,subprocess
from cryptography.hazmat.primitives import serialization
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
s=requests.Session();s.verify=str(O/'just-nas-certificate.pem')
def call(p,m='GET',body=None,status=200):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=300);assert r.status_code==status,(p,r.status_code,r.text[:2000]);return r.json()
def ready():
 for _ in range(180):
  try:
   if s.get(BASE+'/api/auth/status',timeout=3).status_code==200:break
  except requests.RequestException:pass
  time.sleep(1)
 else:raise RuntimeError('NAS unavailable')
 time.sleep(1.2);c=json.loads((O/'test-credentials.json').read_text(encoding='utf-8'));s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
 for _ in range(120):
  if not call('extensions').get('restoring'):return
  time.sleep(1)
 raise RuntimeError('Extensions recovery timed out')
def job(id,timeout=600):
 for _ in range(timeout):
  j=next(x for x in call('tasks') if x['id']==id)
  if j['state'] not in ('running','queued'):
   assert j['state']=='succeeded',j.get('error');return
  time.sleep(1)
 raise RuntimeError('Task timed out')
def upload(file,kind):
 with file.open('rb') as f:r=s.post(BASE+'/api/'+kind+'/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=300)
 assert r.status_code==200,(r.status_code,r.text[:1500]);return r.json()
def install(file,kind='extensions',**settings):
 v=upload(file,kind);j=call(kind+'/install','POST',{'token':v['token'],**settings},202);job(j['task_id']);print('PASS installed',file.name,flush=True)
def remote(cmd,timeout=90):
 args=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(O/'vm-manager-known-hosts'),'-o','IdentitiesOnly=yes','-i',str(O/'stores-admin.key'),'root@192.168.56.101','set -e; '+cmd]
 p=subprocess.run(args,capture_output=True,text=True,timeout=timeout);assert p.returncode==0,p.stderr[:2000];return p.stdout
def bootstrap():
 w=json.loads((O/'vm-manager-whitelist.json').read_text());assert w['uuid']=='e1b847ff-a4c1-4f60-8c0d-c1491ccee830';assert pathlib.Path(w['path']).samefile(O/'vm-manager-test.vdi');info=subprocess.check_output([VBOX,'showmediuminfo','disk',w['path']],text=True);assert w['uuid'] in info
 pools=call('storage/pools')
 if not any(p['name']=='vmtest' for p in pools):
  d=next(d for d in call('storage/disks') if 'VBe1b847ff-' in d['path']);assert d['available'] and d['size']==128*1024**3
  call('storage/pools','POST',{'name':'vmtest','raid_type':'single','devices':[d['path']],'confirm':'vmtest'});print('PASS dedicated whitelisted VM test pool created',flush=True)
 st=call('extensions');assert st['state']['dataset']=='homepool/.just-nas-extensions'
 key=serialization.load_ssh_private_key((O/'stores-admin.key').read_bytes(),None);pub=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode();call('extensions/ssh-key','POST',{'public_key':pub})
 if 'ssh-server' not in st['state']['installed']:install(R/'output/store-packages/ssh-server-1.0.0.jnpkg')
 st=call('extensions');(O/'vm-manager-known-hosts').write_text('192.168.56.101 '+st['ssh_host_key']+'\n',encoding='ascii')
 print(remote('ip link set eth1 up; udhcpc -i eth1 -q -n -t 8; test -f /sys/fs/cgroup/cgroup.controllers; cat /sys/fs/cgroup/cgroup.controllers'),flush=True)
 print('PASS NAS, SSH and cgroup v2 available; no container engine installed',flush=True)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('phase',choices=['bootstrap','qemu','app','capabilities']);args=p.parse_args();ready()
 if args.phase=='bootstrap':bootstrap()
 elif args.phase=='qemu':install(R/'output/vm-store-packages/qemu-runtime-11.0.3-r0.jnpkg')
 elif args.phase=='app':install(R/'output/vm-store-packages/vm-manager-1.0.0-rc1.jnpkg','apps',reuse_data=True)
 else:print(json.dumps(call('app-services/vm-manager/capabilities'),ensure_ascii=True,indent=2))
