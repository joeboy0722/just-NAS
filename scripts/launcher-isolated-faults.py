"""Fault injection only on the disposable full clone, never on original just-nas.

Requires six independent base VDIs under output/vm-test/just-nas-launcher-validation,
an explicit UUID whitelist, and the original VM powered off. No host disk commands.
"""
import importlib.util,json,pathlib,re,subprocess,time,sys
sp=importlib.util.spec_from_file_location('verification',pathlib.Path(__file__).with_name('verify-launcher-vm.py'));v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v)
O=v.O; VM='just-nas-launcher-validation';ROOT=(O/VM).resolve()
def vb(*args):return subprocess.check_output([v.VBOX,*args],text=True)
def guard():
 assert 'VMState="poweroff"' in vb('showvminfo','just-nas','--machinereadable'),'Original VM must remain off'
 w=json.loads((O/'launcher-isolated-whitelist.json').read_text(encoding='utf-8'));assert w['vm']==VM and w['uuid']=='3e8384f9-f0e5-4ac1-bda5-42589f3a0d0b'
 info=vb('showvminfo',VM,'--machinereadable');assert 'UUID="'+w['uuid']+'"' in info
 attached=[pathlib.Path(json.loads(l.split('=',1)[1])).resolve() for l in info.splitlines() if re.match(r'"SATA-\d+-0"=',l)]
 assert len(attached)==len(w['disks'])==6
 for d in w['disks']:
  p=pathlib.Path(d['path']).resolve();assert p in attached and p.is_relative_to(ROOT) and p.suffix=='.vdi'
  metadata=vb('showmediuminfo','disk',str(p));assert d['uuid'] in metadata and re.search(r'Parent UUID:\s+base',metadata)
def remote(cmd):
 guard();return v.remote(cmd)
checks=json.loads((O/'launcher-isolated-results.json').read_text(encoding='utf-8')) if '--resume' in sys.argv else []
def record(check):
 checks.append({'check':check,'result':'PASS'});(O/'launcher-isolated-results.json').write_text(json.dumps(checks,indent=2),encoding='utf-8');print('PASS',check,flush=True)
def main():
 guard();v.ready();record('Only independent UUID-whitelisted clone VDIs connected; original VM remains off')
 if v.call('apps/launcher')['apps'][0]['status']!='active':
  v.job(v.call('apps/action','POST',{'id':'static-site','action':'start'},202)['task_id'])
 assert v.call('apps/launcher')['apps'][0]['status']=='active'
 if '--resume' not in sys.argv:
  remote('docker --host unix:///run/just-nas-docker.sock kill just-nas-static-site')
  assert v.call('apps/launcher')['apps'][0]['status']=='failed'
  v.job(v.call('apps/action','POST',{'id':'static-site','action':'start'},202)['task_id']);assert v.call('apps/launcher')['apps'][0]['status']=='active'
  record('Unexpected actual container exit reports failure; start recovers')
 remote("test ! -e /usr/local/sbin/docker; mkdir -p /usr/local/sbin; printf '#!/bin/sh\nsleep 7\nexit 1\n' > /usr/local/sbin/docker; chmod 755 /usr/local/sbin/docker")
 try:
  started=time.monotonic();assert v.call('apps/launcher')['apps'][0]['status']=='unknown';assert time.monotonic()-started<12
 finally:remote('rm /usr/local/sbin/docker')
 record('Real Docker query timeout returns unknown within bounded deadline')
 # Cover a mount disappearing while configuration and process are still present.
 # A temporary empty mount masks only this disposable clone's extension path.
 guard()
 args=['ssh','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(O/'launcher-known-hosts'),'-o','IdentitiesOnly=yes','-i',str(O/'stores-admin.key'),'root@192.168.56.101','sh -s']
 channel=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 try:
  channel.stdin.write('set -e; mkdir -p /run/launcher-empty; mount --bind /run/launcher-empty /mnt/homepool/.just-nas-extensions; echo MASKED\n');channel.stdin.flush()
  assert channel.stdout.readline().strip()=='MASKED'
  assert v.call('apps/launcher')['apps'][0]['status']=='unavailable'
  r=v.s.post(v.BASE+'/api/apps/import',data=b'invalid',headers={'Content-Type':'application/octet-stream'},timeout=15);assert r.status_code==409
  assert v.call('auth/status')['authenticated']
 finally:
  channel.stdin.write('test -z "$(find /run/launcher-empty -mindepth 1 -maxdepth 1)"; umount /mnt/homepool/.just-nas-extensions; echo RECOVERED\n');channel.stdin.flush()
  assert channel.stdout.readline().strip()=='RECOVERED'
  channel.stdin.close();assert channel.wait(timeout=30)==0
 assert v.call('apps/launcher')['apps'][0]['status']=='active'
 record('Missing expected mount reports unavailable, refuses RAM writes and restores actual active state')
 record('Management remains available throughout isolated faults')
if __name__=='__main__':main()
