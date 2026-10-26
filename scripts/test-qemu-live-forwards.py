"""Isolated portable-QEMU monitor test: no NAS, disk, guest OS or host settings."""
import argparse,json,pathlib,socket,subprocess,time
R=pathlib.Path(__file__).resolve().parents[1]
def free_port(kind=socket.SOCK_STREAM):
 with socket.socket(type=kind) as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def main():
 a=argparse.ArgumentParser();a.add_argument('--qemu',type=pathlib.Path,required=True);v=a.parse_args()
 out=R/'output/vm-settings-tests';out.mkdir(exist_ok=True);port=free_port();p1=free_port();p2=free_port(socket.SOCK_DGRAM)
 args=[str(v.qemu.resolve()),'-name','just-nas-isolated-nat-test','-machine','q35','-accel','tcg,tb-size=512','-m','64','-S','-display','none','-nodefaults','-netdev',f'user,id=net0,hostfwd=tcp:127.0.0.1:{p1}-:80','-qmp',f'tcp:127.0.0.1:{port},server=on,wait=off']
 with (out/'qemu-test.log').open('w') as log:
  proc=subprocess.Popen(args,stdout=log,stderr=log);c=None;f=None
  try:
   for _ in range(60):
    if proc.poll() is not None:raise RuntimeError('Isolated QEMU exited; inspect qemu-test.log')
    try:c=socket.create_connection(('127.0.0.1',port),timeout=2);break
    except OSError:time.sleep(.1)
   if c is None:raise RuntimeError('QMP unavailable; preserve isolated test PID '+str(proc.pid))
   f=c.makefile('rwb');greeting=json.loads(f.readline())
   def qmp(cmd,arguments=None):
    f.write((json.dumps({'execute':cmd,**({'arguments':arguments} if arguments else {})})+'\n').encode());f.flush()
    while True:
     response=json.loads(f.readline())
     if 'error' in response:raise RuntimeError(str(response['error']))
     if 'return' in response:return response['return']
   qmp('qmp_capabilities')
   def hmp(command):return qmp('human-monitor-command',{'command-line':command})
   assert 'TCP[HOST_FORWARD]' in hmp('info usernet')
   assert hmp(f'hostfwd_add net0 udp:127.0.0.1:{p2}-:53')==''
   table=hmp('info usernet');assert 'UDP[HOST_FORWARD]' in table and str(p2) in table
   # Same listener, changed guest port: remove/add without resuming the guest.
   assert hmp(f'hostfwd_remove net0 tcp:127.0.0.1:{p1}').strip().endswith('removed')
   assert hmp(f'hostfwd_add net0 tcp:127.0.0.1:{p1}-:8080')==''
   assert '8080' in hmp('info usernet')
   with socket.socket() as blocker:
    blocker.bind(('127.0.0.1',0));blocker.listen();blocked=blocker.getsockname()[1]
    result=hmp(f'hostfwd_add net0 tcp:127.0.0.1:{blocked}-:80')
    assert result.strip(),'Port conflict incorrectly reported success'
    assert str(blocked) not in hmp('info usernet')
   assert qmp('query-status')['status']=='prelaunch','NAT operation resumed guest'
   assert hmp(f'hostfwd_remove net0 tcp:127.0.0.1:{p1}').strip().endswith('removed')
   assert hmp(f'hostfwd_remove net0 udp:127.0.0.1:{p2}').strip().endswith('removed')
   with socket.socket() as s:s.bind(('127.0.0.1',p1))
   with socket.socket(type=socket.SOCK_DGRAM) as s:s.bind(('127.0.0.1',p2))
   jit=hmp('info jit')
   result={'result':'PASS','qemu':greeting['QMP']['version'],'checks':['Actual TCP/UDP live add/remove','Same-port target edit','Conflict rejected without phantom rule','Listeners released','VM not restarted/resumed','512 MiB TCG argument accepted'],'jit':jit,'scope':'Independent Windows QEMU; NAS runtime validation remains separate'}
   (out/'qemu-results.json').write_text(json.dumps(result,indent=2));print('PASS isolated QEMU live forwarding and TCG cache')
  finally:
   if f:
    try:f.write(b'{"execute":"quit"}\n');f.flush();proc.wait(timeout=10)
    finally:f.close();c.close()
if __name__=='__main__':main()
