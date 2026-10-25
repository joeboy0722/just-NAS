"""Stopped blank smoke guest only; preserves both OS installation guests."""
import base64, importlib.util, json, pathlib, struct, time
R = pathlib.Path(__file__).resolve().parents[1]
sp = importlib.util.spec_from_file_location('v', R/'scripts/verify-vm-app.py')
v = importlib.util.module_from_spec(sp); sp.loader.exec_module(v); v.ready()
P = 'app-services/vm-manager/'
assert 'VBe1b847ff-' in v.remote('zpool status -P vmtest')
ident = 'd272d397f34788524c302f547f6a3ca3'
m = next(x for x in v.call(P+'vms')['vms'] if x['id']==ident)
assert m['name']=='SMOKE-TCG' and m['status'] in ('stopped','failed') and not m.get('process_present')
assert m['dataset']=='vmtest/.just-nas-vms/machines/'+ident
root = '/mnt/vmtest/.just-nas-vms/machines/'+ident
checks = []
def job(reply, success=True):
    for _ in range(120):
        j = v.call(P+'tasks/'+reply['task_id'])
        if j['state'] not in ('running','queued'):
            assert (j['state']=='succeeded')==success, j
            return j
        time.sleep(1)
    raise RuntimeError('VM task timeout')
def record(s):
    checks.append(s); print('PASS',s,flush=True)
fields = 'name os cpus memory_mib disk_gib firmware acceleration iso boot_iso_once autostart forwards'.split()
spec = {k:m.get(k, '' if k=='iso' else False) for k in fields}
spec.update(disk_gib=5, memory_mib=768)
job(v.call(P+'vms/'+ident,'PATCH',spec,202))
header = base64.b64decode(v.remote('dd if='+root+'/disk.qcow2 bs=32 count=1 2>/dev/null | base64').strip())
assert header[:4]==b'QFI\xfb' and struct.unpack('>Q',header[24:32])[0]==5*1024**3
record('Powered-off VM edits resources and actually grows qcow2 to 5 GiB')
small = dict(spec,disk_gib=4)
job(v.call(P+'vms/'+ident,'PATCH',small,202),False)
record('Disk shrink rejected')
digest = v.remote('sha256sum '+root+'/disk.qcow2').split()[0]
job(v.call(P+'vms/'+ident,'DELETE',{'purge':False,'confirm':''},202))
state = v.call(P+'vms')
assert not any(x['id']==ident for x in state['vms']) and any(x['id']==ident for x in state['retained'])
assert v.remote('sha256sum '+root+'/disk.qcow2').split()[0]==digest
record('Removing deployment retains unchanged disk and recoverable definition')
job(v.call(P+'vms/'+ident+'/restore','POST',{},202))
restored = next(x for x in v.call(P+'vms')['vms'] if x['id']==ident)
assert restored['disk_gib']==5 and restored['memory_mib']==768
assert v.remote('sha256sum '+root+'/disk.qcow2').split()[0]==digest
record('Retained deployment re-registers with identical disk')
(R/'output/vm-test/storage-lifecycle.json').write_text(json.dumps({'result':'PASS','checks':checks},indent=2),encoding='utf-8')
