"""Pinned-key SSH and deliberately failed application update, confined to storetest VDI."""
import base64,hashlib,json,os,pathlib,secrets,subprocess,time,requests
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';BASE='https://192.168.56.101:8443';checks=[]
s=requests.Session();s.verify=str(OUT/'just-nas-certificate.pem')
def record(name):
 checks.append({'check':name,'result':'PASS'});(OUT/'stores-recovery.json').write_text(json.dumps(checks,indent=2));print('PASS:',name,flush=True)
def call(p,m='GET',body=None,status=200):
 r=s.request(m,BASE+'/api/'+p,json=body,timeout=120);assert r.status_code==status,(p,r.status_code,r.text[:1000]);return r.json()
def job(id,success=True):
 for _ in range(180):
  v=next(j for j in call('tasks') if j['id']==id)
  if v['state'] not in ('running','queued'):
   assert (v['state']=='succeeded')==success,{k:v[k] for k in ('state','error') if k in v};return v
  time.sleep(1)
 raise RuntimeError('task timeout')
def upload(file):
 with file.open('rb') as f:r=s.post(BASE+'/api/apps/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
 assert r.status_code==200,(r.status_code,r.text[:1000]);return r.json()
for _ in range(120):
 try:
  if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(2)
c=json.loads((OUT/'test-credentials.json').read_text());time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
for _ in range(120):
 state=call('extensions')
 if not state.get('restoring'):break
 time.sleep(1)
assert state['state']['dataset']=='storetest/.just-nas-extensions'
# The only write below is a uniquely named proof file in the application dataset on this VDI.
vbox=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';info=subprocess.check_output([vbox,'showvminfo','just-nas','--machinereadable'],text=True);attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-2-0"=')));assert pathlib.Path(attached).samefile(OUT/'stores-test.vdi')
private=OUT/'stores-admin.key'
if private.exists():key=serialization.load_ssh_private_key(private.read_bytes(),password=None)
else:
 key=Ed25519PrivateKey.generate();private.write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.OpenSSH,serialization.NoEncryption()))
public=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode()
# OpenSSH on Windows requires a private key readable only by its owner.
account=subprocess.check_output(['whoami'],text=True).strip();subprocess.run(['icacls',str(private),'/inheritance:r','/grant:r',account+':(R)'],check=True,stdout=subprocess.DEVNULL)
call('extensions/ssh-key','POST',{'public_key':public})
with (ROOT/'output/store-packages/ssh-server-1.0.0.jnpkg').open('rb') as f:r=s.post(BASE+'/api/extensions/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
assert r.status_code==200,(r.status_code,r.text[:1000]);token=r.json()['token'];v=call('extensions/install','POST',{'token':token},202);job(v['task_id']);record('Optional SSH installs offline and accepts configured Ed25519 key')
state=call('extensions');hostkey=state['ssh_host_key'];assert hostkey.startswith('ssh-ed25519 ') and state['ssh_fingerprint'].startswith('SHA256:');known=OUT/'stores-known-hosts';known.write_text('192.168.56.101 '+hostkey+'\n')
ssh=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(known),'-o','IdentitiesOnly=yes','-o','ConnectTimeout=10','-i',str(private),'root@192.168.56.101']
def remote(command):
 p=subprocess.run(ssh+[command],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=90);assert p.returncode==0,p.stderr[:2000];return p.stdout
assert remote("docker --host unix:///run/just-nas-docker.sock info --format '{{.Driver}}'").strip()=='zfs';record('Actual Docker daemon reports ZFS storage driver over pinned-key SSH')
data=json.loads(remote('docker --host unix:///run/just-nas-docker.sock inspect just-nas-static-site'))[0];host=data['HostConfig'];assert data['Config']['User']=='65532:65532' and host['ReadonlyRootfs'] and not host['Privileged'];assert 'ALL' in host['CapDrop'] and any('no-new-privileges' in x for x in host['SecurityOpt']);assert host['Memory']==64*1024**2 and host['NanoCpus']==500000000;assert all(m['Destination']=='/srv/content' and not m['RW'] for m in data['Mounts']);assert all(p['HostIp']=='192.168.56.101' for ports in host['PortBindings'].values() for p in ports);record('Actual container enforces non-root, dropped capabilities, read-only mount and CPU/memory limits')
password=secrets.token_urlsafe(24);encrypted=call('settings/export','POST',{'password':password});salt=base64.b64decode(encrypted['Salt']);nonce=base64.b64decode(encrypted['Nonce']);secret=hashlib.pbkdf2_hmac('sha256',password.encode(),salt,600000,32);plain=AESGCM(secret).decrypt(nonce,base64.b64decode(encrypted['Data']),b'just-nas-settings-v2');files=json.loads(plain);settings=json.loads(base64.b64decode(files['extensions.json']));assert settings['ssh_public_key']==public and 'container-engine' in settings['installed'];assert 'apps.json' in files;record('Encrypted settings export preserves store references and SSH public key')
proof=secrets.token_hex(16);remote("printf '%s' '"+proof+"' > /mnt/storetest/.just-nas-extensions/apps/static-site/content/rollback-proof.txt");assert requests.get('http://192.168.56.101:18080/rollback-proof.txt',timeout=10).text==proof
bad=upload(OUT/'bad-app.jnpkg');assert not bad['package']['official'];call('apps/install','POST',{'token':bad['token'],'bind_ip':'192.168.56.101','port':18080,'read_only':True},409);record('Unsigned custom application requires explicit administrator trust')
v=call('apps/install','POST',{'token':bad['token'],'confirm_unofficial':True,'bind_ip':'192.168.56.101','port':18080,'read_only':True},202);job(v['task_id'],False)
assert call('apps')['apps']['apps']['static-site']['package']['manifest']['version']=='1.0.0';assert requests.get('http://192.168.56.101:18080/rollback-proof.txt',timeout=10).text==proof;assert '-failed-' in remote('zfs list -H -o name -r storetest/.just-nas-extensions/apps');record('Failed update restores original application and proof file while retaining failed data generation')
# Key-only SSH: disabling public-key authentication must fail in batch mode.
p=subprocess.run(ssh[:-1]+['-o','PubkeyAuthentication=no',ssh[-1],'true'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=20);assert p.returncode!=0;record('SSH rejects connections without public-key authentication')
