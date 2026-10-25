"""Quota fault only on the ISO child of the UUID-whitelisted test VDI."""
import importlib.util, json, os, pathlib, subprocess
R=pathlib.Path(__file__).resolve().parents[1]
sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py')
v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready()
white=json.loads((R/'output/vm-test/vm-manager-whitelist.json').read_text())
assert white['uuid']=='e1b847ff-a4c1-4f60-8c0d-c1491ccee830'
assert 'VBe1b847ff-' in v.remote('zpool status -P vmtest')
ds='vmtest/.just-nas-vms/iso'
assert v.remote('zfs get -H -o value refquota '+ds).strip()=='none'
before=v.call('app-services/vm-manager/isos')
assert not any(i['name']=='full-browser-fixture.iso' for i in before)
fixture=bytearray(os.urandom(10<<20));fixture[16*2048+1:16*2048+6]=b'CD001'
(R/'output/vm-test/full-browser-fixture.iso').write_bytes(fixture)
used=int(v.remote('zfs get -Hp -o value usedbydataset '+ds).strip())
try:
    v.remote('zfs set refquota='+str(used+(1<<20))+' '+ds)
    env={**os.environ,'NODE_PATH':'C:/Users/joeboy/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules'}
    result=subprocess.run(['node',str(R/'scripts/vm-manager-upload-failure.cjs')],cwd=R,env=env)
finally:
    v.remote('zfs set refquota=none '+ds)
after=v.call('app-services/vm-manager/isos')
assert after==before, 'Existing media changed'
assert not v.remote("find /mnt/vmtest/.just-nas-vms/iso -maxdepth 1 -name '.upload-*' -print").strip()
assert result.returncode==0
print('PASS ISO quota restored; existing media unchanged and temporary upload removed',flush=True)
