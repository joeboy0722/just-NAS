"""Cold missing-pool recovery; detach only the whitelisted store test VDI."""
import json,pathlib,subprocess,time,requests
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';checks=[]
s=requests.Session();s.verify=str(OUT/'just-nas-certificate.pem')
def call(p,m='GET',body=None,status=200):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=90);assert r.status_code==status,(p,r.status_code,r.text[:1200]);return r.json()
def ready():
 s.cookies.clear()
 for _ in range(120):
  try:
   if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
  except requests.RequestException:pass
  time.sleep(2)
 else:raise RuntimeError('Core unavailable')
 time.sleep(1.2);c=json.loads((OUT/'test-credentials.json').read_text());s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
 for _ in range(120):
  state=call('extensions')
  if not state.get('restoring'):return state
  time.sleep(1)
 raise RuntimeError('Restore stuck')
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/'stores-missing-pool.json').write_text(json.dumps(checks,indent=2));print('PASS:',name,flush=True)
def cycle(*args):subprocess.run(['python',str(ROOT/'scripts/vm-cycle.py'),*args],check=True)
def vbox(*args):return subprocess.check_output([VBOX,*args],text=True,stderr=subprocess.STDOUT)
state=ready();assert state['mounted'];port=state['apps']['apps']['static-site']['port']
if any(v['name']=='StoresNFS' for v in call('shares')):call('shares?name=StoresNFS&protocol=nfs','DELETE')
cycle('--stop')
info=vbox('showvminfo','just-nas','--machinereadable');assert 'VMState="poweroff"' in info
attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-2-0"=')))
target=OUT/'stores-test.vdi';assert pathlib.Path(attached).samefile(target)
medium=vbox('showmediuminfo','disk',str(target));assert '68254ab5-dfeb-41dd-af5d-b6cc857eaf2c' in medium
vbox('storageattach','just-nas','--storagectl','SATA','--port','2','--device','0','--type','hdd','--medium','none')
cycle();state=ready();assert not state['mounted'] and state['state']['dataset']=='storetest/.just-nas-extensions'
assert state['state']['installed']['container-engine']['status']=='unavailable';assert state['apps']['apps']['static-site']['status']=='unavailable'
record('Missing dedicated extension pool does not block core and reports unavailable state')
r=s.post(BASE+'/api/extensions/import',data=b'not-a-package',headers={'Content-Type':'application/octet-stream'},timeout=10);assert r.status_code==409,(r.status_code,r.text)
record('Missing mount refuses package writes before parsing archive')
subprocess.run(['python',str(ROOT/'scripts/verify-family-final.py'),'output/alpine-nas-stores.iso'],check=True)
record('Original SMB, accounts, private isolation and HTTPS remain available without extension disk')
cycle('--stop');record('Core shuts down normally with configured but absent extension pool')
vbox('storageattach','just-nas','--storagectl','SATA','--port','2','--device','0','--type','hdd','--medium',str(target))
cycle();state=ready();assert state['mounted'] and state['state']['installed']['container-engine']['status']=='active'
for _ in range(60):
 try:
  if requests.get('http://192.168.56.101:'+str(port)+'/',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(1)
else:raise RuntimeError('Application did not recover')
assert state['state']['installed']['ssh-server']['status']=='active'
call('shares','POST',{'name':'StoresNFS','protocol':'nfs','path':'/mnt/storetest/nfs-proof','allowed_hosts':'192.168.56.1/32','read_only':False})
record('Reattaching exact test VDI restores engine, SSH and application without reinstall')
