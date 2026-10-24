"""Read-only final verification: existing NAS settings, embedded assets, SMB and VM disks."""
from pathlib import Path
import hashlib,json,re,subprocess,sys
import requests
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'output/ui-review'
BASE='https://192.168.56.101:8443'
credentials=json.loads((ROOT/'output/vm-test/test-credentials.json').read_text())
s=requests.Session();s.verify=str(ROOT/'output/vm-test/just-nas-certificate.pem')
r=s.post(BASE+'/api/auth/login',json={'password':credentials['admin']},timeout=30);r.raise_for_status()
def api(p):
 r=s.get(BASE+'/api/'+p,timeout=30);r.raise_for_status();return r.json()
before=json.loads((OUT/'vm-before.json').read_text(encoding='utf8'))
after={p:api(p) for p in before}
def ordered_users(xs):return sorted(xs,key=lambda x:x['id'])
def ordered_shares(xs):return sorted([{**x,'users':sorted(x.get('users',[]))} for x in xs],key=lambda x:x['id'])
assert ordered_users(before['users'])==ordered_users(after['users']),'Account configuration changed'
assert ordered_shares(before['shares'])==ordered_shares(after['shares']),'Share or ACL changed'
assert before['backup']==after['backup'],'Backup configuration changed'
assert before['network']['settings']==after['network']['settings'],'Network settings changed'
def dataset_settings(xs):return sorted([{k:x[k] for k in ['name','quota','compression','mountpoint']} for x in xs],key=lambda x:x['name'])
assert dataset_settings(before['storage/datasets'])==dataset_settings(after['storage/datasets']),'Dataset settings changed'
def schedule_settings(xs):return sorted([{k:v for k,v in x.items() if k not in ['last_run']} for x in xs],key=lambda x:x['id'])
assert schedule_settings(before['schedules'])==schedule_settings(after['schedules']),'Schedules changed'
checks=['Existing accounts preserved','Existing shares and access rights preserved','Backup and network settings preserved','Dataset quotas and compression preserved','Schedules preserved']
for relative in ['index.html','files.html','css/style.css','js/ui.js','js/layout.js','js/app.js','js/files.js','js/maintenance.js']:
 r=s.get(BASE+'/'+relative,timeout=30);r.raise_for_status();assert r.content==(ROOT/'web'/relative).read_bytes(),'VM embedded asset differs: '+relative
checks.append('All eight VM embedded assets exactly match committed sources')
assert all(p['health']=='ONLINE' for p in api('storage/pools'))
checks.append('ZFS pools ONLINE after reboot')
props=api('storage/dataset-properties?name=homepool/Family');assert props['org.just-nas:quota-locked']=='off'
checks.append('Family quota write lock is off')
vm=subprocess.check_output([r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe','showvminfo','just-nas','--machinereadable'],text=True)
pattern=r'^"SATA-(?:ImageUUID-)?\d+-\d+"=.*$'
assert sorted(re.findall(pattern,(OUT/'vm-before-config.txt').read_text(encoding='utf8'),re.M))==sorted(re.findall(pattern,vm,re.M)),'VM virtual disk mapping changed'
checks.append('All existing VM virtual disk paths and UUIDs unchanged')
sys.path.insert(0,str(ROOT/'output/test-deps'))
import smbclient
smbclient.register_session('192.168.56.101',username=credentials['user'],password=credentials['password'])
with smbclient.open_file(r'\\192.168.56.101\Family\proof.txt','rb') as f: proof=f.read()
portal=requests.Session();portal.verify=s.verify
r=portal.post(BASE+'/files-api/login',json={'name':credentials['user'],'password':credentials['password']});r.raise_for_status()
spaces=portal.get(BASE+'/files-api/spaces').json();family=next(x for x in spaces['spaces'] if x['name']=='Family')
r=portal.get(BASE+'/files-api/download',params={'space':family['id'],'path':'proof.txt'});r.raise_for_status();assert r.content==proof
checks.append('Windows SMB 445 and file portal download identical proof.txt')
checks.append('SMB proof SHA256: '+hashlib.sha256(proof).hexdigest())
(OUT/'vm-final-verification.json').write_text(json.dumps({'checks':checks,'state':after},ensure_ascii=False,indent=2),encoding='utf8')
for check in checks:print('PASS',check)
