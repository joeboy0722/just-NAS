"""Observe real OS proof and NAS heartbeats; never equate a live PID with OS readiness."""
import pathlib,importlib.util,requests,json,time,sys,hashlib
R=pathlib.Path(__file__).resolve().parents[1];sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py');v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready()
kind=sys.argv[1];port=18581 if kind=='ubuntu' else 18582;name='Ubuntu 24.04.4' if kind=='ubuntu' else 'Windows 10 Pro';rows=[];since=None;previous=None;last_state=None
deadline=time.monotonic()+7200;out=R/'output/vm-test'/('monitor-'+kind+'.json')
while time.monotonic()<deadline:
 now=time.monotonic();row={'time':time.time()};healthy=False
 try:
  t=time.monotonic();m=next(m for m in v.call('app-services/vm-manager/vms')['vms'] if m['name']==name);row.update(status=m['status'],reason=m.get('reason',''),nas_latency=time.monotonic()-t,used_bytes=m['used_bytes'])
  if m['status']=='failed':rows.append(row);out.write_text(json.dumps(rows,indent=2),encoding='utf-8');raise RuntimeError('Guest failed; evidence saved')
  try:
   proof=requests.get('http://192.168.56.101:'+str(port)+'/',timeout=3).json();row['proof']=proof
   expected=('just-NAS Linux guest persistence proof\n' if kind=='ubuntu' else 'just-NAS Windows guest persistence proof\n').encode()
   assert proof['sha256']==hashlib.sha256(expected).hexdigest(), 'proof file mismatch'
   assert proof['desktop']=='active' if kind=='ubuntu' else proof['desktop']>0
   healthy=True
  except requests.RequestException:pass
 except (requests.RequestException,AssertionError) as e:row['error']=str(e)
 if previous is not None and now-previous>20:row['heartbeat_gap']=now-previous;healthy=False
 if healthy:
  if since is None:since=now
  row['continuous_ready_seconds']=now-since
 else:since=None
 previous=now;rows.append(row);out.write_text(json.dumps(rows,indent=2),encoding='utf-8')
 state=(row.get('status'),healthy)
 if state!=last_state:print(kind,state,'OS proof verified' if healthy else 'OS readiness not established',flush=True);last_state=state
 if since is not None and now-since>=1800:print('PASS 30 minutes verified OS plus NAS API heartbeats',flush=True);break
 time.sleep(10)
else:raise RuntimeError('OS/continuity verification timed out; no success recorded')
