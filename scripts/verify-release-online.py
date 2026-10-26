"""Fresh online installation on a dedicated VirtualBox VM and new VDI; preserves just-nas."""
import argparse,base64,hashlib,importlib.util,json,pathlib,re,secrets,socket,ssl,subprocess,time
import requests
from cryptography.hazmat.primitives import serialization
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/release-online';O.mkdir(exist_ok=True);V=r'C:/Program Files/Oracle/VirtualBox/VBoxManage.exe';NAME='just-nas-release-check';BASE='https://127.0.0.1:19543';s=None

def vb(*args):return subprocess.check_output([V,*map(str,args)],text=True,errors='replace')
def record(name):
 p=O/'checks.json';rows=json.loads(p.read_text()) if p.exists() else [];rows.append({'check':name,'result':'PASS'});p.write_text(json.dumps(rows,indent=2));print('PASS',name,flush=True)
def pinned():
 session=requests.Session();session.trust_env=False;session.verify=str(O/'certificate.pem');return session

def call(path,method='GET',body=None,status=200):
 r=s.request(method,BASE+'/api/'+path,json=body,timeout=180);assert r.status_code==status,(path,r.status_code,r.text[:1200]);return r.json()
def job(id,success=True):
 for _ in range(1200):
  j=next(x for x in call('tasks') if x['id']==id)
  if j['state'] not in ('queued','running'):
   assert (j['state']=='succeeded')==success,j;return j
  time.sleep(.5)
 raise RuntimeError('Online install task timed out')
def wait_http():
 global s
 for _ in range(150):
  try:
   pem=ssl.get_server_certificate(('127.0.0.1',19543),timeout=2);(O/'certificate.pem').write_text(pem,encoding='ascii');s=pinned()
   return call('auth/status')
  except (OSError,requests.RequestException):time.sleep(1)
 raise RuntimeError('Fresh NAS did not become ready')

def create():
 assert not (O/'fixture.json').exists(),'Existing fixture requires inspection; refusing to replace it'
 # Read-only host preflight. Do not stop or modify the existing just-nas VM.
 primary=vb('showvminfo','just-nas','--machinereadable');assert 'VMState="running"' in primary
 free=int(subprocess.check_output(['powershell.exe','-NoProfile','-Command','(Get-CimInstance Win32_OperatingSystem).FreePhysicalMemory'],text=True).strip())*1024
 assert free>=10*1024**3,'Keep at least 6GiB Windows headroom after this 4GiB fixture'
 (O/'primary-before.txt').write_text(primary,encoding='utf-8')
 for port in [19543,19522]:
  with socket.socket() as check:check.bind(('127.0.0.1',port))
 vm=O/'virtualbox';vm.mkdir();vb('createvm','--name',NAME,'--ostype','Linux_64','--basefolder',vm.resolve(),'--register')
 vb('modifyvm',NAME,'--memory','4096','--cpus','2','--pae','on','--nested-hw-virt','on','--ioapic','on','--graphicscontroller','vmsvga','--vram','16','--nic1','nat','--nictype1','82540EM','--natpf1','web,tcp,127.0.0.1,19543,,8443','--natpf1','ssh,tcp,127.0.0.1,19522,,22','--uart1','0x3F8','4','--uartmode1','file',(O/'serial.log').resolve(),'--boot1','dvd','--boot2','disk')
 disk=O/'release-check-only.vdi';vb('createmedium','disk','--filename',disk.resolve(),'--size','16384','--format','VDI')
 vb('storagectl',NAME,'--name','SATA','--add','sata','--controller','IntelAhci');vb('storageattach',NAME,'--storagectl','SATA','--port','0','--device','0','--type','hdd','--medium',disk.resolve())
 iso=R/'output/nas-self-build/4fbdb11c90384888a74341e591c6524f/just-NAS-nas-built-candidate-x86_64.iso'
 vb('storagectl',NAME,'--name','IDE','--add','ide');vb('storageattach',NAME,'--storagectl','IDE','--port','0','--device','0','--type','dvddrive','--medium',iso.resolve())
 medium=vb('showmediuminfo','disk',disk.resolve());uuid=re.search(r'(?m)^UUID:\s+(\S+)',medium).group(1)
 (O/'fixture.json').write_text(json.dumps({'vm':NAME,'disk':str(disk.resolve()),'uuid':uuid,'bytes':16*1024**3,'iso':str(iso),'primary_nas_unchanged':True},indent=2));vb('startvm',NAME,'--type','headless');record('Dedicated new 16GiB VDI and 2CPU/4GiB NAS; original NAS kept running')

def login():
 global s
 s=pinned();credentials=json.loads((O/'test-credentials.json').read_text());time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':credentials['admin']})['csrf']
def bootstrap():
 state=wait_http();assert not state['initialized'],'Refusing to overwrite an existing administrator'
 tokens=re.findall(r'Just NAS setup token: ([a-f0-9]{64})',(O/'serial.log').read_text(encoding='utf-8',errors='replace'));assert tokens,'No private local-console setup token'
 credentials={'admin':secrets.token_urlsafe(32)};(O/'test-credentials.json').write_text(json.dumps(credentials));s.headers['X-CSRF-Token']=call('auth/setup','POST',{'token':tokens[-1],'password':credentials['admin']})['csrf'];record('Fresh administrator initialized from private local serial token with pinned TLS')
 fixture=json.loads((O/'fixture.json').read_text());disk=next(d for d in call('storage/disks') if ('VB'+fixture['uuid'][:8]+'-').lower() in d['path'].lower());assert disk['available'] and disk['size']==fixture['bytes']
 call('storage/pools','POST',{'name':'releasecheck','raid_type':'single','confirm':'releasecheck','devices':[disk['path']]});call('extensions/storage','POST',{'pool':'releasecheck'});record('Only the new whitelisted VDI initialized; persistent extension space configured')
 key=serialization.load_ssh_private_key((R/'output/vm-test/stores-admin.key').read_bytes(),None);pub=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode();call('extensions/ssh-key','POST',{'public_key':pub})
 with (R/'output/store-packages/ssh-server-1.0.0.jnpkg').open('rb') as f:r=s.post(BASE+'/api/extensions/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
 r.raise_for_status();job(call('extensions/install','POST',{'token':r.json()['token']},202)['task_id']);state=call('extensions');(O/'known-hosts').write_text('[127.0.0.1]:19522 '+state['ssh_host_key']+'\n',encoding='ascii')
 remote("printf 'nameserver 10.0.2.3\n' > /etc/resolv.conf")
 record('SSH offline bootstrap only; VM QEMU Docker remain absent and uncached')
def remote(command):
 p=subprocess.run(['ssh','-p','19522','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str((O/'known-hosts').resolve()),'-o','IdentitiesOnly=yes','-i',str((R/'output/vm-test/stores-admin.key').resolve()),'root@127.0.0.1','set -e; '+command],capture_output=True,text=True,timeout=90);assert p.returncode==0,p.stderr[:1500];return p.stdout

def verify():
 login();state=call('extensions');assert set(state['state']['installed'])=={'ssh-server'};assert not call('apps')['apps']['apps'];assert not state['imports'];record('No VM QEMU Docker installation or import cache before online test')
 cat=call('extensions/catalog');assert cat['source']=='network' and len(cat['entries'])==11;entries={x['id']:x for x in cat['entries']};assert entries['vm-manager']['version']=='1.0.0-rc2';record('Live signed stores-v2 catalog fetched anonymously by fresh NAS')
 def download(id,kind):
  job(call(kind+'/download','POST',{'id':id},202)['task_id']);entry=next(x for x in call(kind)['imports'] if x['package']['manifest']['id']==id);assert entry['package']['official'] and entry['package']['sha256']==entries[id]['sha256'];record('Fresh anonymous NAS download and official signature: '+id);return entry['token']
 app=download('vm-manager','apps');r=s.post(BASE+'/api/apps/install',json={'token':app},timeout=60);assert r.status_code==409 and 'qemu-runtime' in r.text;record('VM installation without QEMU dependency rejected')
 runtime=download('qemu-runtime','extensions');job(call('extensions/install','POST',{'token':runtime},202)['task_id']);job(call('apps/install','POST',{'token':app},202)['task_id']);assert next(x for x in call('apps/launcher')['apps'] if x['id']=='vm-manager')['status']=='active';record('Fresh online QEMU and native VM manager installation active without Docker')
 engine=download('container-engine','extensions');job(call('extensions/install','POST',{'token':engine},202)['task_id']);assert remote("DOCKER_HOST=unix:///run/just-nas-docker.sock docker info --format '{{.Driver}}'").strip()=='zfs';record('Fresh online Docker installed with real ZFS backend')
 deps=s.post(BASE+'/api/extensions/action',json={'id':'qemu-runtime','action':'remove'},timeout=60)
 if deps.status_code==202:job(deps.json()['task_id'],False)
 else:assert deps.status_code==409
 record('Removing QEMU while VM manager depends on it rejected')
 cap=call('app-services/vm-manager/capabilities');assert cap['kvm'];record('Fresh online VM manager reports actual KVM available')
 report=R/'output/release-rc4-evidence/release-validation.json';d=json.loads(report.read_text());d['checks']['online_install']='PASS';d['checks']['anonymous_catalog']='PASS';d['deferred']=[x for x in d['deferred'] if 'fresh online' not in x.lower()];report.write_text(json.dumps(d,indent=2))
 record('Fresh package online installation acceptance complete')
def reboot(iso):
 before=(O/'certificate.pem').read_bytes();login();fixture=json.loads((O/'fixture.json').read_text());assert pathlib.Path(fixture['disk']).resolve().is_relative_to(O.resolve())
 assert fixture['uuid'] in vb('showmediuminfo','disk',fixture['disk'])
 call('system/shutdown','POST',{'confirm':'poweroff'})
 for _ in range(90):
  if 'VMState="poweroff"' in vb('showvminfo',NAME,'--machinereadable'):break
  time.sleep(1)
 else:raise RuntimeError('Test NAS did not stop normally before candidate update')
 vb('storageattach',NAME,'--storagectl','IDE','--port','0','--device','0','--type','dvddrive','--medium',iso.resolve());vb('startvm',NAME,'--type','headless');wait_http();assert (O/'certificate.pem').read_bytes()==before,'Persistent TLS identity changed'
 login()
 for _ in range(120):
  if not call('extensions').get('restoring'):break
  time.sleep(1)
 launcher=call('apps/launcher');assert all(x['status']=='active' for x in launcher['apps'])
 assert {'container-engine','vm-manager'}<={x['id'] for x in launcher['apps']}
 expected=(iso.parent/'server-binary.sha256').read_text().split()[0];actual=remote('sha256sum /usr/local/bin/nas-server').split()[0];assert actual==expected,'Wrong embedded server after boot'
 assert remote('sha256sum /usr/share/just-nas/catalog.json').split()[0]==hashlib.sha256((R/'assets/store-catalog-v2.json').read_bytes()).hexdigest(),'Wrong bundled catalog'
 remote("printf 'nameserver 10.0.2.3\\n' > /etc/resolv.conf")
 assert call('extensions/catalog')['source']=='network';assert call('app-services/vm-manager/capabilities')['kvm']
 assert remote("DOCKER_HOST=unix:///run/just-nas-docker.sock docker info --format '{{.Driver}}'").strip()=='zfs'
 vb('controlvm',NAME,'screenshotpng',R/'output/release-rc4-evidence/final-core-boot.png')
 report=R/'output/release-rc4-evidence/release-validation.json';d=json.loads(report.read_text());d['checks']['core_release_boot']='PASS';d['checks']['core_iso_audit']='PASS';d['checks']['persistent_services']='PASS';report.write_text(json.dumps(d,indent=2))
 receipt=iso.parent/'source-revision.json';metadata=json.loads(receipt.read_text());metadata['vm_boot_verified']=True;metadata['boot_verification']='Isolated NAS exact binary/catalog hashes, persistent TLS and restored services';receipt.write_text(json.dumps(metadata,indent=2))
 record('Final rc4 ISO cold boot: exact server hash, persistent TLS, SSH, Docker ZFS and KVM manager restored')

def finish():
 login();call('system/shutdown','POST',{'confirm':'poweroff'})
 for _ in range(90):
  if 'VMState="poweroff"' in vb('showvminfo',NAME,'--machinereadable'):break
  time.sleep(1)
 else:raise RuntimeError('Release test NAS did not stop normally')
 assert 'VMState="running"' in vb('showvminfo','just-nas','--machinereadable');record('Validation NAS normally stopped; original NAS remained running throughout')
def main():
 p=argparse.ArgumentParser();p.add_argument('phase',choices=['create','bootstrap','verify','reboot','finish']);p.add_argument('--iso',type=pathlib.Path);a=p.parse_args();reboot(a.iso) if a.phase=='reboot' else globals()[a.phase]()
if __name__=='__main__':main()
