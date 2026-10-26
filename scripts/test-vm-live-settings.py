"""NAS integration: disposable blank guest on the preapproved vmtest VDI only."""
import importlib.util,json,pathlib,socket,time
R=pathlib.Path(__file__).resolve().parents[1]
sp=importlib.util.spec_from_file_location('verify',R/'scripts/verify-vm-app.py');v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v)
def call(path,method='GET',body=None,status=200):return v.call('app-services/vm-manager/'+path,method,body,status)
def wait(task,success=True):
 for _ in range(90):
  result=call('tasks/'+task)
  if result['state'] not in ('running','queued'):
   assert (result['state']=='succeeded')==success,result;return result
  time.sleep(.5)
 raise RuntimeError('VM task timed out')
def main():
 v.ready()
 assert v.remote("zpool get -H -o value guid vmtest").strip()=='10764411103673780170'
 assert not v.remote("DOCKER_HOST=unix:///run/just-nas-docker.sock docker ps -q").strip(),'Do not run test during build'
 before=call('vms')['vms'];original={x['id'] for x in before}
 assert all(not x.get('process_present') and x['status'] in ('stopped','failed') for x in before)
 name='NAT-CACHE-SMOKE';machine=None;out=R/'output/vm-settings-tests';out.mkdir(exist_ok=True)
 def monitor(command):
  return v.remote("python3 - "+machine+" "+__import__('shlex').quote(command)+" <<'PY'\nimport socket,json,sys\np='/mnt/vmtest/.just-nas-vms/machines/'+sys.argv[1]+'/run/qmp.sock'\ns=socket.socket(socket.AF_UNIX);s.settimeout(4);s.connect(p);f=s.makefile('rwb');json.loads(f.readline())\nfor cmd in [{'execute':'qmp_capabilities'},{'execute':'human-monitor-command','arguments':{'command-line':sys.argv[2]}}]:\n f.write((json.dumps(cmd)+chr(10)).encode());f.flush()\n while True:\n  r=json.loads(f.readline())\n  if 'error' in r:raise RuntimeError(str(r['error']))\n  if 'return' in r:break\nprint(r['return'])\nPY",timeout=15)
 try:
  spec={'name':name,'os':'linux','cpus':1,'memory_mib':256,'tcg_cache_mib':256,'disk_gib':4,'firmware':'bios','acceleration':'tcg','autostart':False,'forwards':[]}
  wait(call('vms','POST',spec,202)['task_id'])
  machine=next(x['id'] for x in call('vms')['vms'] if x['id'] not in original and x['name']==name)
  wait(call('vms/'+machine+'/actions','POST',{'action':'start'},202)['task_id'])
  record=v.remote('cat /run/just-nas/vm-records/'+machine+'.json').strip()
  limit=v.remote('cat /sys/fs/cgroup/just-nas-vms/'+machine+'/memory.max').strip()
  assert int(limit)==(256+768+256-64)*1024**2,limit
  rows=[{'protocol':'tcp','host_ip':'192.168.56.101','host_port':29480,'guest_port':80},{'protocol':'udp','host_ip':'192.168.56.101','host_port':29481,'guest_port':53}]
  wait(call('vms/'+machine+'/forwards','PATCH',{'forwards':rows},202)['task_id'])
  table=monitor('info usernet');assert 'TCP[HOST_FORWARD]' in table and 'UDP[HOST_FORWARD]' in table
  with socket.create_connection(('192.168.56.101',29480),timeout=3):pass
  rows[0]['guest_port']=8080
  wait(call('vms/'+machine+'/forwards','PATCH',{'forwards':rows},202)['task_id'])
  assert '8080' in monitor('info usernet')
  conflict=rows+[{'protocol':'tcp','host_ip':'192.168.56.101','host_port':8443,'guest_port':80}]
  wait(call('vms/'+machine+'/forwards','PATCH',{'forwards':conflict},202)['task_id'],False)
  current=next(x for x in call('vms')['vms'] if x['id']==machine)
  assert current['status']=='running' and current['forwards']==rows
  assert v.remote('cat /run/just-nas/vm-records/'+machine+'.json').strip()==record,'QEMU restarted during NAT change'
  wait(call('vms/'+machine+'/forwards','PATCH',{'forwards':[]},202)['task_id'])
  assert '[HOST_FORWARD]' not in monitor('info usernet')
  try:
   s=socket.create_connection(('192.168.56.101',29480),timeout=2);s.close();raise AssertionError('Removed listener still open')
  except OSError:pass
  (out/'nas-results.json').write_text(json.dumps({'result':'PASS','checks':['256 MiB cache cgroup budget','Actual TCP/UDP live rules','Same-port target edit','NAS HTTPS conflict rejected','Configuration preserved after failure','Same QEMU process throughout','Listener release'],'guest':'Disposable blank BIOS guest; not OS installation evidence'},indent=2))
  print('PASS real NAS TCG cache budget and live NAT settings')
 finally:
  if machine:
   current=next((x for x in call('vms')['vms'] if x['id']==machine),None)
   if current and (current.get('process_present') or current['status'] in ('running','paused','unknown')):
    wait(call('vms/'+machine+'/actions','POST',{'action':'force-stop','confirm':name},202)['task_id'])
   if current:wait(call('vms/'+machine,'DELETE',{'purge':True,'confirm':name},202)['task_id'])
  assert {x['id'] for x in call('vms')['vms']}==original,'Original VM list changed'
if __name__=='__main__':main()
