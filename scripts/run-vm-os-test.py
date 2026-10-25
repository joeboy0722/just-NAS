"""Drive real installed guest tests through the same administrator app API."""
import importlib.util,pathlib,json,time,sys,requests
R=pathlib.Path(__file__).resolve().parents[1]
sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py');v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready()
P='app-services/vm-manager/'
def job(id,timeout=300):
 for _ in range(timeout):
  j=v.call(P+'tasks/'+id)
  if j['state'] not in ['running','queued']:
   assert j['state']=='succeeded',j;return j
  time.sleep(1)
 raise RuntimeError('VM task timeout')
def action(id,action,confirm=''):return job(v.call(P+'vms/'+id+'/actions','POST',{'action':action,'confirm':confirm},202)['task_id'])
kind=sys.argv[1];name='Ubuntu 24.04.4' if kind=='ubuntu' else 'Windows 10 Pro';media='ubuntu-test-unattended.iso' if kind=='ubuntu' else 'windows10-test-unattended.iso'
machines=v.call(P+'vms')['vms'];m=next((m for m in machines if m['name']==name),None)
if m is None:
 iso=next(i['id'] for i in v.call(P+'isos') if i['name']==media)
 spec={'name':name,'os':'linux' if kind=='ubuntu' else 'windows10','cpus':2,'memory_mib':4096,'disk_gib':32 if kind=='ubuntu' else 64,'firmware':'uefi','acceleration':'auto','iso':iso,'autostart':False,'forwards':[{'protocol':'tcp','host_ip':'192.168.56.101','host_port':18581 if kind=='ubuntu' else 18582,'guest_port':8080}]}
 job(v.call(P+'vms','POST',spec,202)['task_id']);m=next(m for m in v.call(P+'vms')['vms'] if m['name']==name)
 print('PASS created isolated guest',name,m['id'],flush=True)
if '--refresh-media' in sys.argv:
 assert m['status'] in ['stopped','failed'] and not m.get('process_present'), 'Stop the guest before changing test media'
 iso=next(i['id'] for i in v.call(P+'isos') if i['name']==media)
 fields='name os cpus memory_mib disk_gib firmware acceleration iso boot_iso_once autostart forwards'.split()
 spec={k:m.get(k,'' if k=='iso' else False) for k in fields};spec.update(iso=iso,boot_iso_once=True)
 job(v.call(P+'vms/'+m['id'],'PATCH',spec,202)['task_id']);m=next(x for x in v.call(P+'vms')['vms'] if x['id']==m['id'])
 print('PASS selected current verified test-media registration',flush=True)
if '--create-only' not in sys.argv and m['status'] not in ['running','paused']:action(m['id'],'start');print('PASS guest process started',name,flush=True)
(R/'output/vm-test'/('guest-'+kind+'.json')).write_text(json.dumps(m,indent=2),encoding='utf-8')
print('Guest created but not started.' if '--create-only' in sys.argv else 'Guest installation running; OS readiness has not yet been asserted.',flush=True)
