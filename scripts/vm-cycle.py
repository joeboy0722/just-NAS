"""Clean VM lifecycle; only explicitly named workspace ISO/VDI fixtures are changed."""
import pathlib,subprocess,json,time,sys,requests
ROOT=pathlib.Path(__file__).resolve().parents[1];OUT=ROOT/'output/vm-test';VBOX=r'C:\Program Files\Oracle\VirtualBox\VBoxManage.exe'
def vbox(*args):return subprocess.check_output([VBOX,*args],text=True,stderr=subprocess.STDOUT)
def state():return vbox('showvminfo','just-nas','--machinereadable')
if 'VMState="running"' in state():
 time.sleep(1.2);s=requests.Session();c=json.loads((OUT/'test-credentials.json').read_text());r=s.post('http://192.168.56.101:8080/api/auth/login',json={'password':c['admin']},timeout=60);r.raise_for_status();s.headers['X-CSRF-Token']=r.json()['csrf']
 for i in range(90):
  r=s.post('http://192.168.56.101:8080/api/system/shutdown',json={'confirm':'poweroff'},timeout=30)
  if r.status_code==200:break
  assert r.status_code==409,r.text
  time.sleep(2)
 else:raise RuntimeError('Busy NAS did not stop')
for i in range(90):
 if 'VMState="poweroff"' in state():break
 time.sleep(2)
else:raise RuntimeError('VM did not power off cleanly')
isoarg=next((a for a in sys.argv[1:] if a.endswith('.iso')),None)
if isoarg:
 iso=(ROOT/isoarg).resolve();assert iso.is_relative_to(ROOT/'output') and iso.suffix=='.iso' and iso.is_file()
 print(vbox('storageattach','just-nas','--storagectl','IDE','--port','0','--device','0','--type','dvddrive','--medium',str(iso)))
if '--hotplug-tests' in sys.argv:
 for port,name in [(1,'family-project-a.vdi'),(2,'family-project-b.vdi'),(3,'family-backup.vdi'),(4,'family-spare.vdi')]:
  target=(OUT/name).resolve();assert target.is_relative_to(OUT) and target.is_file()
  print(vbox('storageattach','just-nas','--storagectl','SATA','--port',str(port),'--device','0','--type','hdd','--medium',str(target),'--hotpluggable','on'))
if '--detach-backup' in sys.argv:
 assert 'family-backup.vdi' in state()
 print(vbox('storageattach','just-nas','--storagectl','SATA','--port','3','--device','0','--type','hdd','--medium','none'))
if '--attach-backup' in sys.argv:
 target=(OUT/'family-backup.vdi').resolve();assert target.is_relative_to(OUT) and target.is_file()
 print(vbox('storageattach','just-nas','--storagectl','SATA','--port','3','--device','0','--type','hdd','--medium',str(target)))
for i in range(6):
 try:print(vbox('startvm','just-nas','--type','gui'));break
 except subprocess.CalledProcessError:
  if i==5:raise
  time.sleep(3)
print('VM started; wait for NAS readiness before testing.')
