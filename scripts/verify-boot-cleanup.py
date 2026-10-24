"""NFS proof on the boot test VDI, then remove only the boot test pool and optional SSH."""
import pathlib,json,subprocess,requests,time,os,importlib.util,base64,sys
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';BASE='https://192.168.56.101:8443';s=requests.Session();s.verify=str(O/'just-nas-certificate.pem');c=json.loads((O/'test-credentials.json').read_text(encoding='utf-8'));checks=json.loads((O/'boot-rc2-results.json').read_text(encoding='utf-8'))
def call(p,m='GET',b=None,status=200):
 v=s.request(m,BASE+'/api/'+p,json=b,timeout=120);assert v.status_code==status,(p,v.status_code,v.text[:900]);return v.json()
time.sleep(1.2);s.headers['X-CSRF-Token']=call('auth/login','POST',{'password':c['admin']})['csrf']
def record(name):
 checks.append({'check':name,'result':'PASS'});(O/'boot-rc2-results.json').write_text(json.dumps(checks,indent=2)+chr(10),encoding='utf-8');print('PASS',name,flush=True)
for _ in range(120):
 if not call('extensions').get('restoring'):break
 time.sleep(1)
else:raise RuntimeError('Extension recovery did not finish')
fixture=json.loads((O/'boot-rc2-disk.json').read_text(encoding='utf-8'));assert fixture['port']==5 and pathlib.Path(fixture['path']).samefile(O/'boot-console-test.vdi')
ssh=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(O/'boot-rc2-known-hosts'),'-o','IdentitiesOnly=yes','-i',str(O/'stores-admin.key'),'root@192.168.56.101']
def remote(cmd):
 v=subprocess.run(ssh+['set -e; '+cmd],text=True,capture_output=True,timeout=40);assert v.returncode==0,v.stderr[:600];return v.stdout
assert 'VB'+fixture['uuid'][:8]+'-' in remote('/usr/sbin/zpool status -P boottest');assert call('extensions')['state']['dataset']=='boottest/.just-nas-extensions'
remote('test ! -e /usr/local/sbin/rc-service; test ! -e /usr/local/sbin/zpool')
if '--cleanup-only' not in sys.argv:
 if not any(v['name']=='boottest/nfs-proof' for v in call('storage/datasets')):call('storage/datasets','POST',{'name':'boottest/nfs-proof','mountpoint':'/mnt/boottest/nfs-proof','compression':'lz4'})
 call('shares','POST',{'name':'BootNFSProof','protocol':'nfs','path':'/mnt/boottest/nfs-proof','allowed_hosts':'192.168.56.1/32','read_only':False})
 original=b'just-NAS boot NFS proof'+bytes([10]);encoded=base64.b64encode(original).decode();remote("test \"$(findmnt -n -o SOURCE -T /mnt/boottest/nfs-proof)\" = boottest/nfs-proof; printf '%s' '"+encoded+"' | base64 -d > /mnt/boottest/nfs-proof/proof.txt; chown 65534:65534 /mnt/boottest/nfs-proof/proof.txt; chmod 660 /mnt/boottest/nfs-proof/proof.txt")
 os.environ.update(NAS_TEST_HOST='192.168.56.101',NAS_TEST_NFS_PORT='2049',NAS_TEST_NFS_CLIENT='192.168.56.1',NAS_TEST_NFS_PATH='/mnt/boottest/nfs-proof/proof.txt');spec=importlib.util.spec_from_file_location('probe',R/'scripts/nfs_probe.py');nfs=importlib.util.module_from_spec(spec);spec.loader.exec_module(nfs);assert nfs.read()==original;changed=b'verified'+b'.'*(len(original)-8);nfs.write(changed);assert nfs.read()==changed;nfs.write(original);assert nfs.read()==original;record('Final rc2 NFSv4 reads, writes and restores proof data on whitelisted boottest VDI')
call('storage/pools?name=boottest&confirm=boottest','DELETE');assert not call('extensions')['state']['dataset'] and not any(v['name']=='BootNFSProof' for v in call('shares'));assert next(v for v in call('storage/pools') if v['name']=='homepool')['health']=='ONLINE';record('Boot test pool, optional SSH and proof share removed; original homepool remains ONLINE')
fresh=json.loads((O/'boot-rc2-fresh-results.json').read_text(encoding='utf-8'));checks.extend(fresh);(O/'boot-rc2-results.json').write_text(json.dumps(checks,indent=2)+chr(10),encoding='utf-8')
