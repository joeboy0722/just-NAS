"""NFS and official download regression only on the independent test clone."""
import importlib.util,pathlib,os,json
R=pathlib.Path(__file__).resolve().parents[1]
sp=importlib.util.spec_from_file_location('isolated',R/'scripts/launcher-isolated-faults.py');t=importlib.util.module_from_spec(sp);sp.loader.exec_module(t);t.guard();v=t.v;v.ready()
ds='homepool/launcher-nfs-test';target='/mnt/homepool/launcher-nfs-test'
if not any(x['name']==ds for x in v.call('storage/datasets')):v.call('storage/datasets','POST',{'name':ds,'mountpoint':target,'compression':'lz4'})
if not any(x['name']=='LauncherNFS' for x in v.call('shares')):v.call('shares','POST',{'name':'LauncherNFS','protocol':'nfs','path':target,'allowed_hosts':'192.168.56.1/32','read_only':False})
t.remote("printf 'launcher NFS proof' > /mnt/homepool/launcher-nfs-test/proof.txt; chown 65534:65534 /mnt/homepool/launcher-nfs-test/proof.txt; chmod 660 /mnt/homepool/launcher-nfs-test/proof.txt")
os.environ.update(NAS_TEST_HOST='192.168.56.101',NAS_TEST_NFS_PORT='2049',NAS_TEST_NFS_CLIENT='192.168.56.1',NAS_TEST_NFS_PATH=target+'/proof.txt')
sp=importlib.util.spec_from_file_location('nfs',R/'scripts/nfs_probe.py');nfs=importlib.util.module_from_spec(sp);sp.loader.exec_module(nfs)
assert nfs.read()==b'launcher NFS proof';nfs.write(b'Launcher NFS proof');assert nfs.read()==b'Launcher NFS proof'
checks=['NFSv4 read and committed write on disposable clone dataset'];print('PASS',checks[-1],flush=True)
t.remote('ip link set eth1 up; udhcpc -i eth1 -q -n -t 8')
assert v.call('apps/catalog')['source']=='network'
v.job(v.call('apps/download','POST',{'id':'static-site'},202)['task_id'])
assert any(x['package']['manifest']['id']=='static-site' and x['package']['official'] for x in v.call('apps')['imports'])
checks.append('Official Release download verifies package signature and retains it for installation');print('PASS',checks[-1],flush=True)
(v.O/'launcher-isolated-regression.json').write_text(json.dumps({'checks':checks},indent=2),encoding='utf-8')
