"""Basic OS acceptance only: remove installation ISO, boot disk and query OS.

Uses existing private test proof helpers; does not run updates or soak tests.
Screenshots still need human inspection before declaring the desktop accepted.
"""
import argparse, importlib.util, json, pathlib, time
import requests

R=pathlib.Path(__file__).resolve().parents[1]
sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py')
v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v)
P='app-services/vm-manager/'
args=argparse.ArgumentParser();args.add_argument('kind',choices=['ubuntu','windows']);args.add_argument('--timeout-seconds',type=int,default=3600);a=args.parse_args()
name='Ubuntu 24.04.4' if a.kind=='ubuntu' else 'Windows 10 Pro'
port=18581 if a.kind=='ubuntu' else 18582
v.ready()

def machine():
    try: items=v.call(P+'vms')
    except AssertionError as e:
        if not (isinstance(e.args[0],tuple) and e.args[0][1]==401):raise
        v.ready();items=v.call(P+'vms')
    return next(x for x in items['vms'] if x['name']==name)

def job(reply):
    for _ in range(180):
        j=v.call(P+'tasks/'+reply['task_id'])
        if j['state'] not in ('running','queued'):
            assert j['state']=='succeeded',j
            return j
        time.sleep(1)
    raise RuntimeError('VM task timeout')

def proof():
    r=requests.get('http://192.168.56.101:'+str(port)+'/',timeout=60);r.raise_for_status();p=r.json()
    assert ('Ubuntu' in p['os'] and p['desktop']=='active') if a.kind=='ubuntu' else ('Windows 10' in p['os'] and p['desktop']>0)
    return p

def wait_proof():
    deadline=time.monotonic()+a.timeout_seconds
    while time.monotonic()<deadline:
        m=machine()
        assert m['status'] not in ('failed','unavailable'),m.get('reason')
        try:return proof()
        except (requests.RequestException,ValueError,AssertionError):time.sleep(5)
    raise RuntimeError('OS desktop readiness not established; no success recorded')

m=machine();before=None
if m.get('iso'):
    if a.kind=='windows':
        before=wait_proof();print('Windows setup reached a desktop; now shutting down to remove ISO',flush=True)
        job(v.call(P+'vms/'+m['id']+'/actions','POST',{'action':'shutdown'},202))
    deadline=time.monotonic()+a.timeout_seconds;last=None
    while time.monotonic()<deadline:
        m=machine()
        if m['status']!=last:print('Installer guest:',m['status'],flush=True);last=m['status']
        if m['status']=='stopped':break
        assert m['status']!='failed',m.get('reason')
        time.sleep(5)
    else:raise RuntimeError('Installer has not powered off; ISO left attached')
    fields='name os cpus memory_mib disk_gib firmware acceleration iso boot_iso_once autostart forwards'.split()
    spec={k:m.get(k,'' if k=='iso' else False) for k in fields};spec.update(iso='',boot_iso_once=False)
    job(v.call(P+'vms/'+m['id'],'PATCH',spec,202))
    print('PASS installation ISO detached through the app API',flush=True)
    m=machine()
if m['status']=='stopped':job(v.call(P+'vms/'+m['id']+'/actions','POST',{'action':'start'},202))
p=wait_proof();m=machine()
assert not m.get('iso') and m['actual_acceleration']=='tcg'
if before:assert p['boot_id']!=before['boot_id']
report={'result':'OS_READY_SCREENSHOT_PENDING','name':name,'vm_id':m['id'],'acceleration':m['actual_acceleration'],
        'installation_iso_attached':False,'os_proof':p,'soak_test':'DEFERRED_BY_USER'}
(R/'output/vm-test'/('basic-disk-boot-'+a.kind+'.json')).write_text(json.dumps(report,indent=2),encoding='utf-8')
print('PASS disk-only TCG OS boot and desktop service; inspect actual desktop screenshot next',flush=True)
