"""Maintenance failures and NFS use only storetest on the dedicated stores-test.vdi."""
import json,os,pathlib,subprocess,time,requests,base64,importlib.util
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';checks=[]
s=requests.Session();s.verify=str(OUT/'just-nas-certificate.pem')
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/'stores-maintenance.json').write_text(json.dumps(checks,indent=2));print('PASS:',name,flush=True)
def call(p,m='GET',body=None,status=200):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=180);assert r.status_code==status,(p,r.status_code,r.text[:1500]);return r.json()
def job(id):
 for _ in range(180):
  v=next(j for j in call('tasks') if j['id']==id)
  if v['state'] not in ('running','queued'):assert v['state']=='succeeded',v.get('error');return
  time.sleep(1)
 raise RuntimeError('Task timeout')
for _ in range(120):
 try:
  if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(2)
c=json.loads((OUT/'test-credentials.json').read_text());time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
for _ in range(120):
 state=call('extensions')
 if not state.get('restoring') and state['state']['installed']['container-engine']['status']=='active':break
 time.sleep(1)
assert state['state']['dataset']=='storetest/.just-nas-extensions'
info=subprocess.check_output([r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe','showvminfo','just-nas','--machinereadable'],text=True);attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-2-0"=')));assert pathlib.Path(attached).samefile(OUT/'stores-test.vdi')
ssh=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(OUT/'stores-known-hosts'),'-o','IdentitiesOnly=yes','-o','ConnectTimeout=10','-i',str(OUT/'stores-admin.key'),'root@192.168.56.101']
def remote(cmd):
 r=subprocess.run(ssh+[cmd],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=90);assert r.returncode==0,r.stderr[:1500];return r.stdout
# Crash only the demo container; the display must show its actual state, not its desired state.
remote('docker --host unix:///run/just-nas-docker.sock kill just-nas-static-site')
assert call('apps')['apps']['apps']['static-site']['status']=='failed';record('Unexpected container exit is reported as failed')
job(call('apps/action','POST',{'id':'static-site','action':'start'},202)['task_id']);assert requests.get('http://192.168.56.101:18080/',timeout=10).status_code==200;record('Failed application can be restarted from management API')
# A new public dataset and export are fixtures; existing Family and private datasets are never written.
datasets=call('storage/datasets')
if not any(d['name']=='storetest/nfs-proof' for d in datasets):call('storage/datasets','POST',{'name':'storetest/nfs-proof','mountpoint':'/mnt/storetest/nfs-proof','compression':'lz4'})
if not any(v['name']=='StoresNFS' for v in call('shares')):call('shares','POST',{'name':'StoresNFS','protocol':'nfs','path':'/mnt/storetest/nfs-proof','allowed_hosts':'192.168.56.1/32','read_only':False})
original=b'just-nas VM acceptance: original file\n';encoded=base64.b64encode(original).decode();remote("printf '%s' '"+encoded+"' | base64 -d > /mnt/storetest/nfs-proof/proof.txt; chown 65534:65534 /mnt/storetest/nfs-proof/proof.txt; chmod 660 /mnt/storetest/nfs-proof/proof.txt")
os.environ.update(NAS_TEST_HOST='192.168.56.101',NAS_TEST_NFS_PORT='2049',NAS_TEST_NFS_CLIENT='192.168.56.1',NAS_TEST_NFS_PATH='/mnt/storetest/nfs-proof/proof.txt')
spec=importlib.util.spec_from_file_location('nfs',ROOT/'scripts/nfs_probe.py');nfs=importlib.util.module_from_spec(spec);spec.loader.exec_module(nfs);assert nfs.read()==original;changed=b'NFS verified '+b'.'*(len(original)-13);nfs.write(changed);assert nfs.read()==changed;nfs.write(original);assert nfs.read()==original;record('NFSv4 reads and commits writes on dedicated test dataset')
# Hold a cwd with a detached process. zpool destroy must fail, then settings and services must recover.
remote('rm -f /run/just-nas/holder.pid /run/just-nas/holder.ready')
remote("setsid sh -c 'cd /mnt/storetest/nfs-proof && echo $$ > /run/just-nas/holder.pid && echo ready > /run/just-nas/holder.ready && exec sleep 60' </dev/null >/dev/null 2>&1 &")
for _ in range(20):
 if remote('cat /run/just-nas/holder.ready').strip()=='ready':break
 time.sleep(0.2)
result=call('storage/pools?name=storetest&confirm=storetest','DELETE',status=409);assert 'busy' in result['error'].lower()
assert call('extensions')['state']['dataset']=='storetest/.just-nas-extensions';assert requests.get('http://192.168.56.101:18080/',timeout=10).status_code==200;record('Busy pool deletion fails without data loss and restores engine and application')
# This PID belongs only to our sleeper, verified by its cwd before signalling it.
remote('p=$(cat /run/just-nas/holder.pid); if [ "$(readlink /proc/$p/cwd)" = /mnt/storetest/nfs-proof ]; then kill "$p"; fi; rm -f /run/just-nas/holder.pid /run/just-nas/holder.ready')
assert any(v['name']=='StoresNFS' for v in call('shares'));assert next(p for p in call('storage/pools') if p['name']=='homepool')['health']=='ONLINE';record('Failed deletion restores export configuration and preserves original homepool')
catalog=call('extensions/catalog');assert len(catalog['entries'])==9;record('Bundled signed catalog remains browsable without external network')
