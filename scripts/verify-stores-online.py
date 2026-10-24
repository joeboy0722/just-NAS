"""Real anonymous Release downloads on just-nas. Only whitelisted stores-test.vdi is writable."""
import pathlib,json,subprocess,time,requests,hashlib
from cryptography.hazmat.primitives import serialization
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';checks=[]
s=requests.Session();s.verify=str(O/'just-nas-certificate.pem')
def call(p,m='GET',b=None,status=200):
 v=s.request(m,BASE+'/api/'+p,json=b,timeout=120);assert v.status_code==status,(p,v.status_code,v.text[:1200]);return v.json()
def record(name):
 checks.append({'check':name,'result':'PASS'});(O/'stores-online.json').write_text(json.dumps(checks,indent=2)+'\n',encoding='utf-8');print('PASS',name,flush=True)
def job(id):
 for _ in range(1200):
  v=next(j for j in call('tasks') if j['id']==id)
  if v['state'] not in ('queued','running'):assert v['state']=='succeeded',v.get('error');return v
  time.sleep(1)
 raise RuntimeError('task timeout')
for _ in range(120):
 try:
  if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(2)
else:raise RuntimeError('VM unavailable')
c=json.loads((O/'test-credentials.json').read_text(encoding='utf-8'));time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
for _ in range(120):
 if not call('extensions').get('restoring'):break
 time.sleep(1)
info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True);assert 'nic2="nat"' in info
attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-2-0"=')));fixture=json.loads((O/'stores-online-disk.json').read_text(encoding='utf-8'));target=O/'stores-online-test.vdi';assert pathlib.Path(fixture['path']).samefile(target) and pathlib.Path(attached).samefile(target)
medium=subprocess.check_output([VBOX,'showmediuminfo','disk',str(target)],text=True);assert fixture['uuid'] in medium
state=call('extensions');disks=call('storage/disks');disk=next(d for d in disks if 'VB'+fixture['uuid'][:8]+'-' in d['path']);assert disk['size']==4*1024**3
if not any(p['name']=='storetest' for p in call('storage/pools')):
 assert disk['available'],'Newly whitelisted test VDI must be blank'
 call('storage/pools','POST',{'name':'storetest','raid_type':'single','devices':[disk['path']],'confirm':'storetest'})
if not state['state']['dataset']:call('extensions/storage','POST',{'pool':'storetest'})
assert call('extensions')['state']['dataset']=='storetest/.just-nas-extensions'
key=serialization.load_ssh_private_key((O/'stores-admin.key').read_bytes(),None);public=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode();call('extensions/ssh-key','POST',{'public_key':public})
# SSH is an offline bootstrap for the temporary DHCP route, not a substitute for the online test.
if 'ssh-server' not in call('extensions')['state']['installed']:
 with (R/'output/store-packages/ssh-server-1.0.0.jnpkg').open('rb') as f:v=s.post(BASE+'/api/extensions/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
 v.raise_for_status();job(call('extensions/install','POST',{'token':v.json()['token']},202)['task_id'])
state=call('extensions');(O/'stores-known-hosts').write_text('192.168.56.101 '+state['ssh_host_key']+'\n',encoding='ascii')
ssh=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(O/'stores-known-hosts'),'-o','IdentitiesOnly=yes','-o','ConnectTimeout=10','-i',str(O/'stores-admin.key'),'root@192.168.56.101']
def remote(cmd):
 v=subprocess.run(ssh+[cmd],text=True,capture_output=True,timeout=90);assert v.returncode==0,v.stderr[:1000];return v.stdout
remote('cp -p /etc/resolv.conf /run/just-nas/online-resolv.before; ip link set eth1 up; udhcpc -i eth1 -q -n -t 8')
record('Temporary NAT route established without changing saved management network settings')
for _ in range(6):
 cat=call('extensions/catalog')
 if cat.get('source')=='network':break
 time.sleep(2)
else:raise RuntimeError('Live GitHub catalog unavailable; bundled fallback is not accepted for this test')
assert len(cat['entries'])==9;record('Live signed GitHub Release catalog fetched and verified in VM')
def download(id,app=False):
 base='apps' if app else 'extensions';job(call(base+'/download','POST',{'id':id},202)['task_id']);v=next(v for v in call(base)['imports'] if v['package']['manifest']['id']==id);e=next(e for e in cat['entries'] if e['id']==id);assert v['package']['official'] and v['package']['sha256']==e['sha256'];record('Anonymous online download and signature verification: '+id);return v['token']
token=download('container-engine');job(call('extensions/install','POST',{'token':token},202)['task_id']);assert remote("docker --host unix:///run/just-nas-docker.sock info --format '{{.Driver}}'").strip()=='zfs';record('Online engine bundle installed and actual Docker backend is ZFS')
token=download('static-site',True);job(call('apps/install','POST',{'token':token,'bind_ip':'192.168.56.101','port':18080,'read_only':True},202)['task_id']);v=requests.get('http://192.168.56.101:18080/',timeout=10);assert v.status_code==200 and 'just-NAS' in v.text;record('Online application bundle installed and website responds')
token=download('ai-accelerators');call('extensions/discard','POST',{'token':token});record('Driver download verified without loading unneeded hardware modules')
remote('cp -p /run/just-nas/online-resolv.before /etc/resolv.conf; ip link set eth1 down')
call('storage/pools?name=storetest&confirm=storetest','DELETE');assert not call('extensions')['state']['dataset'] and not call('apps')['apps']['apps'];assert next(v for v in call('storage/pools') if v['name']=='homepool')['health']=='ONLINE';record('Online test pool and associated settings cleaned; original homepool remains ONLINE')
