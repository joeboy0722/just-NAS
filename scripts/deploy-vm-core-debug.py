"""Deploy only the locally built core to the snapshotted test NAS; final ISO tests remain mandatory."""
import importlib.util,subprocess,pathlib
sp=importlib.util.spec_from_file_location('v',pathlib.Path(__file__).with_name('verify-vm-app.py'));v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready()
assert v.call('extensions')['state']['dataset']=='homepool/.just-nas-extensions'
args=['scp','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(v.O/'vm-manager-known-hosts'),'-o','IdentitiesOnly=yes','-i',str(v.O/'stores-admin.key'),str(v.R/'output/nas-server-vm-linux'),'root@192.168.56.101:/tmp/nas-server-vm-next']
subprocess.run(args,check=True,timeout=180)
print(v.remote('chmod 755 /tmp/nas-server-vm-next; mv /tmp/nas-server-vm-next /usr/local/bin/nas-server; rc-service nas-server restart',timeout=120));v.s.cookies.clear();v.ready();print('PASS debug core deployed; NAS HTTPS is available')
