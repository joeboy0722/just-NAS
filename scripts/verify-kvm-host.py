"""Post-reboot KVM acquisition check; no Windows policy changes or TCG fallback."""
import importlib.util,json,pathlib,subprocess
R=pathlib.Path(__file__).resolve().parents[1];OUT=R/'output/kvm-host-test';OUT.mkdir(exist_ok=True)
sp=importlib.util.spec_from_file_location('live',R/'scripts/test-vm-live-settings.py');live=importlib.util.module_from_spec(sp);sp.loader.exec_module(live);v=live.v
VBOX=r'C:/Program Files/Oracle/VirtualBox/VBoxManage.exe'
def main():
 host=subprocess.check_output(['powershell.exe','-NoProfile','-Command','(Get-CimInstance Win32_ComputerSystem).HypervisorPresent'],text=True).strip()
 if host.lower()!='false':raise RuntimeError('Windows Hypervisor remains present; KVM is not ready. Confirm just NAS mode was selected')
 info=subprocess.check_output([VBOX,'showvminfo','just-nas','--machinereadable'],text=True)
 if 'VMState="poweroff"' in info:subprocess.run([VBOX,'startvm','just-nas','--type','headless'],check=True)
 elif 'VMState="running"' not in info:raise RuntimeError('NAS must be normally stopped or running; refusing to reset it')
 v.ready();cap=live.call('capabilities')
 if not cap.get('kvm'):raise RuntimeError('NAS KVM unavailable: '+cap.get('kvm_reason','unknown'))
 assert v.remote('zpool get -H -o value guid vmtest').strip()=='10764411103673780170'
 original={x['id'] for x in live.call('vms')['vms']};machine=None;name='KVM-ACQUIRE-SMOKE'
 try:
  spec={'name':name,'os':'linux','cpus':1,'memory_mib':256,'disk_gib':4,'firmware':'bios','acceleration':'kvm','autostart':False,'forwards':[]}
  live.wait(live.call('vms','POST',spec,202)['task_id'])
  machine=next(x['id'] for x in live.call('vms')['vms'] if x['id'] not in original and x['name']==name)
  live.wait(live.call('vms/'+machine+'/actions','POST',{'action':'start'},202)['task_id'])
  current=next(x for x in live.call('vms')['vms'] if x['id']==machine)
  assert current['status']=='running' and current['actual_acceleration']=='kvm',current
  qmp=v.remote("python3 - "+machine+" <<'PY'\nimport socket,json,sys\ns=socket.socket(socket.AF_UNIX);s.settimeout(4);s.connect('/mnt/vmtest/.just-nas-vms/machines/'+sys.argv[1]+'/run/qmp.sock');f=s.makefile('rwb');json.loads(f.readline())\nfor cmd in ['qmp_capabilities','query-kvm']:\n f.write((json.dumps({'execute':cmd})+chr(10)).encode());f.flush()\n while True:\n  r=json.loads(f.readline())\n  if 'error' in r:raise RuntimeError(str(r['error']))\n  if 'return' in r:break\nprint(json.dumps(r['return']))\nPY",timeout=15)
  result=json.loads(qmp);assert result.get('enabled') is True,result
  log=subprocess.check_output([VBOX,'showvminfo','just-nas','--log','0'],text=True,errors='replace');(OUT/'virtualbox-after.log').write_text(log,encoding='utf-8')
  (OUT/'after-reboot.json').write_text(json.dumps({'result':'PASS','windows_hypervisor_present':False,'nas_kvm':True,'qmp_query_kvm':result,'scope':'Disposable empty guest; OS installation not yet tested'},indent=2))
  print('PASS Windows Hypervisor absent; restricted NAS guest uses KVM; QMP enabled=true')
 finally:
  if machine:
   current=next((x for x in live.call('vms')['vms'] if x['id']==machine),None)
   if current and (current.get('process_present') or current['status'] in ('running','paused','unknown')):live.wait(live.call('vms/'+machine+'/actions','POST',{'action':'force-stop','confirm':name},202)['task_id'])
   if current:live.wait(live.call('vms/'+machine,'DELETE',{'purge':True,'confirm':name},202)['task_id'])
  assert {x['id'] for x in live.call('vms')['vms']}==original
if __name__=='__main__':
 try:main()
 except Exception as e:
  (OUT/'after-reboot.json').write_text(json.dumps({'result':'NOT_PASSED','reason':str(e)},indent=2));raise
