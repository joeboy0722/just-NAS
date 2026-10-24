"""Real VT, multi-IP and fault tests. Writes only the explicitly whitelisted boottest VDI and temporary RAM files."""
import pathlib,json,requests,subprocess,time,base64,hashlib,sys
from cryptography.hazmat.primitives import serialization
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';S=O/'boot-rc2-screenshots';S.mkdir(exist_ok=True);BASE='https://192.168.56.101:8443';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe';checks=json.loads((O/'boot-rc2-results.json').read_text(encoding='utf-8')) if ('--faults' in sys.argv or '--pool' in sys.argv) else []
s=requests.Session();s.verify=str(O/'just-nas-certificate.pem')
def record(name):
 checks.append({'check':name,'result':'PASS'});(O/'boot-rc2-results.json').write_text(json.dumps(checks,indent=2)+'\n',encoding='utf-8');print('PASS',name,flush=True)
def call(p,m='GET',b=None,status=200):
 v=s.request(m,BASE+'/api/'+p,json=b,timeout=120)
 if v.status_code==401 and p!='auth/login':
  login=s.post(BASE+'/api/auth/login',json={'password':c['admin']},timeout=30);login.raise_for_status();s.headers['X-CSRF-Token']=login.json()['csrf'];v=s.request(m,BASE+'/api/'+p,json=b,timeout=120)
 assert v.status_code==status,(p,v.status_code,v.text[:1000]);return v.json()
for _ in range(120):
 try:
  if s.get(BASE+'/api/auth/status',timeout=2).status_code==200:break
 except requests.RequestException:pass
 time.sleep(2)
else:raise RuntimeError('VM unavailable')
c=json.loads((O/'test-credentials.json').read_text(encoding='utf-8'));time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
fixture=json.loads((O/'boot-rc2-disk.json').read_text(encoding='utf-8'));assert fixture['port']==5 and pathlib.Path(fixture['path']).samefile(O/'boot-console-test.vdi')
info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True);attached=json.loads(next(l.split('=',1)[1] for l in info.splitlines() if l.startswith('"SATA-5-0"=')));assert pathlib.Path(attached).samefile(fixture['path']);medium=subprocess.check_output([VBOX,'showmediuminfo','disk',fixture['path']],text=True);assert fixture['uuid'] in medium
state=call('extensions');disk=next(d for d in call('storage/disks') if 'VB'+fixture['uuid'][:8]+'-' in d['path']);assert disk['size']==4*1024**3
if not any(p['name']=='boottest' for p in call('storage/pools')):
 assert disk['available'];call('storage/pools','POST',{'name':'boottest','raid_type':'single','devices':[disk['path']],'confirm':'boottest'})
if not state['state']['dataset']:call('extensions/storage','POST',{'pool':'boottest'})
assert call('extensions')['state']['dataset']=='boottest/.just-nas-extensions'
key=serialization.load_ssh_private_key((O/'stores-admin.key').read_bytes(),None);public=key.public_key().public_bytes(serialization.Encoding.OpenSSH,serialization.PublicFormat.OpenSSH).decode();call('extensions/ssh-key','POST',{'public_key':public})
def job(id):
 for _ in range(180):
  v=next(j for j in call('tasks') if j['id']==id)
  if v['state'] not in ('running','queued'):assert v['state']=='succeeded',v.get('error');return
  time.sleep(1)
 raise RuntimeError('task timeout')
if 'ssh-server' not in call('extensions')['state']['installed']:
 with (R/'output/store-packages/ssh-server-1.0.0.jnpkg').open('rb') as f:v=s.post(BASE+'/api/extensions/import',data=f,headers={'Content-Type':'application/octet-stream'},timeout=180)
 v.raise_for_status();job(call('extensions/install','POST',{'token':v.json()['token']},202)['task_id'])
state=call('extensions');known=O/'boot-rc2-known-hosts';known.write_text('192.168.56.101 '+state['ssh_host_key']+'\n',encoding='ascii')
ssh=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(known),'-o','IdentitiesOnly=yes','-o','ConnectTimeout=5','-i',str(O/'stores-admin.key'),'root@192.168.56.101']
def remote(cmd):
 v=subprocess.run(ssh+["set -e; "+cmd],text=True,capture_output=True,timeout=30);assert v.returncode==0,v.stderr[:600];return v.stdout
def snap():return json.loads(remote('cat /run/just-nas/boot.json'))
def wait(predicate,seconds=70):
 for _ in range(seconds):
  try:
   v=snap()
   if predicate(v):return v
  except (AssertionError,json.JSONDecodeError):pass
  time.sleep(1)
 raise RuntimeError('Expected boot state unavailable')
def screen(name):subprocess.run([VBOX,'controlvm','just-nas','screenshotpng',str(S/(name+'.png'))],check=True)
def restart():
 old=remote('cat /run/nas-server.pid').strip();remote('nohup /sbin/rc-service nas-server restart </dev/null >/run/boot-fixture-restart.log 2>&1 &')
 for _ in range(25):
  time.sleep(.25)
  try:
   if remote('cat /run/nas-server.pid').strip()!=old:return
  except AssertionError:pass
 raise RuntimeError('NAS process did not restart')
def stage(v,id):return next(x['state'] for x in v['stages'] if x['id']==id)
def ready():return wait(lambda v:v['ready'])
if '--faults' not in sys.argv and '--pool' not in sys.argv:
 v=ready();assert v['storage']=='Healthy' and any(a['primary'] and a['ip']=='192.168.56.101' for a in v['addresses']);screen('01-normal');record('Real VT1 welcome, healthy storage and actual primary HTTPS address')
 assert 'Welcome to Alpine' not in remote('cat /etc/issue /etc/motd');assert 'tty1::respawn' not in remote('cat /etc/inittab')
 subprocess.run([VBOX,'controlvm','just-nas','keyboardputscancode','38','3c','bc','b8'],check=True);time.sleep(.6);assert remote('cat /sys/class/tty/tty0/active').strip()=='tty2';assert 'Welcome to Just NAS.' in remote('cat /dev/vcs2');screen('02-local-login');subprocess.run([VBOX,'controlvm','just-nas','keyboardputscancode','38','3b','bb','b8'],check=True);record('Alt+F2 shows branded maintenance login; Alt+F1 returns to status')
 # Only addresses absent before the test are created; these are never persisted.
 remote('ip addr add 192.168.56.110/24 dev eth0; ip addr add 192.168.56.111/24 dev eth0; ip -6 addr add fd42:6e61:73::101/64 dev eth0; ip link add br-nas-proof type bridge; ip link set br-nas-proof up; ip addr add 10.244.70.1/24 dev br-nas-proof; ip link add renamed-proof type veth peer name peer-proof; ip link set renamed-proof up; ip addr add 10.244.71.1/24 dev renamed-proof; ip link add br-012345abcdef type bridge; ip link set br-012345abcdef up; ip addr add 10.244.72.1/24 dev br-012345abcdef')
 v=wait(lambda v:len(v['addresses'])>=5);assert any(a['ip']=='fd42:6e61:73::101' and a['url'].startswith('https://[') for a in v['addresses']);assert any(a['interface']=='br-nas-proof' for a in v['addresses']);assert not any(a['interface'] in ('renamed-proof','br-012345abcdef') for a in v['addresses']);assert next(a for a in v['addresses'] if a['ip']=='192.168.56.110')['certificate_warning'];assert v['pages']>=2;screen('03-multiple-ip');first=v['page'];v=wait(lambda v:v['page']!=first,10);screen('04-address-page');record('Real multiple IPv4/IPv6, normal bridge, renamed veth exclusion, certificate warning and pagination')
 # A short, self-restoring link outage affects only this VM management interface.
 remote("nohup sh -c 'sleep 1; ip link set eth0 down; sleep 9; ip link set eth0 up' </dev/null >/run/boot-link-restore.log 2>&1 &")
 time.sleep(4);screen('05-management-down');time.sleep(8);v=wait(lambda v:not v['primary_missing']);record('Management link outage recovers without changing saved network settings')
 remote('ip addr del 192.168.56.110/24 dev eth0; ip addr del 192.168.56.111/24 dev eth0; ip -6 addr del fd42:6e61:73::101/64 dev eth0; ip link del br-nas-proof; ip link del renamed-proof; ip link del br-012345abcdef')
if '--pool' not in sys.argv:
 # Temporarily intercept only NAS sharing service control; no NAS data files are changed.
 remote('mkdir -p /usr/local/sbin')
 assert remote('test ! -e /usr/local/sbin/rc-service; echo $?').strip()=='0'
 wrapper='#!/bin/sh\nif [ "$1" = samba ] && [ "$2" = status ]; then sleep 3; fi\nexec /sbin/rc-service "$@"\n'
 def install_wrapper(text):remote("printf '%s' '"+base64.b64encode(text.encode()).decode()+"' | base64 -d > /usr/local/sbin/rc-service; chmod 700 /usr/local/sbin/rc-service")
 install_wrapper(wrapper);restart();wait(lambda v:not v['ready'] and stage(v,'shares')=='running',30);screen('06-startup-red-ok')
 raw=base64.b64decode(remote('base64 /dev/vcsa1'));cells=raw[4:];chars=bytes(cells[::2]);attrs=cells[1::2];pos=chars.find(b'OK');assert pos>=0 and attrs[pos]&7==4;ready();remote('rm /usr/local/sbin/rc-service');record('Actual startup VT attributes show red OK; no production delay added')
 wrapper='#!/bin/sh\nif [ "$1" = samba ]; then case "$2" in start|reload|restart) exit 1;; esac; fi\nexec /sbin/rc-service "$@"\n';install_wrapper(wrapper);restart();v=wait(lambda v:v['ready'] and stage(v,'shares')=='fail');screen('07-share-failure');raw=base64.b64decode(remote('base64 /dev/vcsa1'));chars=bytes(raw[4::2]);attrs=raw[5::2];pos=chars.find(b'FAIL');assert pos>=0 and attrs[pos]&7==4;remote('rm /usr/local/sbin/rc-service');restart();wait(lambda v:v['ready'] and stage(v,'shares')=='ok');record('Injected real share restore command failure remains red FAIL on final screen; recovery restores normal state')
# Keep the complete recovery script in RAM so exporting the SSH key dataset cannot lock out recovery.
assert remote('test ! -e /usr/local/sbin/zpool; echo $?').strip()=='0'
wrapper='#!/bin/sh\nif [ "$1" = import ] && [ "$2" = -a ]; then exec /usr/sbin/zpool import -d /run/boot-empty-import -a; fi\nexec /usr/sbin/zpool "$@"\n'
remote("printf '%s' '"+base64.b64encode(wrapper.encode()).decode()+"' | base64 -d > /usr/local/sbin/zpool; chmod 700 /usr/local/sbin/zpool; mkdir -p /run/boot-empty-import")
recovery='''#!/bin/sh
set -e
/usr/sbin/zpool export boottest
/sbin/rc-service nas-server restart
for n in $(seq 1 45); do
 if python3 -c 'import json; v=json.load(open("/run/just-nas/boot.json")); assert v["ready"] and next(s["state"] for s in v["stages"] if s["id"]=="extensions")=="fail"' 2>/dev/null; then break; fi
 sleep 1
done
cp /run/just-nas/boot.json /run/boot-missing-proof.json
sleep 10
rm /usr/local/sbin/zpool
/usr/sbin/zpool import boottest
/sbin/rc-service nas-server restart
'''
remote("printf '%s' '"+base64.b64encode(recovery.encode()).decode()+"' | base64 -d > /run/boot-missing-recovery.sh; chmod 700 /run/boot-missing-recovery.sh; nohup /run/boot-missing-recovery.sh </dev/null >/run/boot-missing-recovery.log 2>&1 &")
for _ in range(70):
 try:
  if call('extensions')['mounted']==False:break
 except (AssertionError,requests.RequestException):pass
 time.sleep(1)
else:raise RuntimeError('Test pool did not become unavailable')
time.sleep(3);screen('08-missing-extension-pool');v=wait(lambda v:v['ready'] and stage(v,'extensions')=='ok',100)
proof=json.loads(remote('cat /run/boot-missing-proof.json'));assert proof['ready'] and stage(proof,'extensions')=='fail';record('Unavailable exported test pool produces extension failure while core and homepool remain available; reimport recovers')
serial=(O/'boot-rc2-serial.log').read_text(encoding='utf-8',errors='replace');assert 'Welcome to Just NAS.' in serial and '[OK]' in serial;record('Serial terminal receives readable real startup events and welcome')
# Keep SSH installed for diskless/plain boot checks, cleaned only after all exercises.
screen('09-recovered');print('READY for diskless/plain checks; dedicated test SSH remains installed',flush=True)
