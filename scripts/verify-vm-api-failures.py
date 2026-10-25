"""Non-destructive negative API cases; ISO quota fault is confined to whitelisted vmtest."""
import importlib.util,pathlib,json,time,ssl,http.client,os,requests
R=pathlib.Path(__file__).resolve().parents[1];O=R/'output/vm-test';sp=importlib.util.spec_from_file_location('v',R/'scripts/verify-vm-app.py');v=importlib.util.module_from_spec(sp);sp.loader.exec_module(v);v.ready();P='app-services/vm-manager/';results=[]
def passed(label):results.append(label);print('PASS',label,flush=True)
for path in ['apps/vm-manager/','api/app-services/vm-manager/capabilities']:
 r=requests.get(v.BASE+'/'+path,verify=v.s.verify,timeout=10);assert r.status_code in [401,403],r.status_code
passed('Native page and API require administrator authentication')
saved=v.s.headers.pop('X-CSRF-Token')
try:r=v.s.post(v.BASE+'/api/'+P+'storage',json={'pool':'vmtest'},timeout=10);assert r.status_code==403
finally:v.s.headers['X-CSRF-Token']=saved
passed('Missing CSRF token rejected')
r=v.s.get(v.BASE+'/api/'+P+'capabilities',headers={'Origin':'https://untrusted.example'},timeout=10);assert r.status_code==403
passed('Cross-origin application request rejected')
existing=v.call(P+'isos');name=existing[0]['name'];r=v.s.post(v.BASE+'/api/'+P+'isos',params={'name':name},data=b'no ISO',timeout=15);assert r.status_code==409
passed('Duplicate ISO name rejected without overwrite')
r=v.s.post(v.BASE+'/api/'+P+'isos',params={'name':'invalid-fixture.iso'},data=b'not an installation medium',timeout=15);assert r.status_code==409
passed('Invalid media rejected')
# Close a TLS upload before its declared length; do not create a network service.
c=http.client.HTTPSConnection('192.168.56.101',8443,context=ssl.create_default_context(cafile=v.s.verify),timeout=10)
c.putrequest('POST','/api/'+P+'isos?name=interrupted-fixture.iso')
for k,val in {'Content-Length':str(10<<20),'Content-Type':'application/octet-stream','X-CSRF-Token':saved,'Cookie':'; '.join(k+'='+val for k,val in v.s.cookies.get_dict().items())}.items():c.putheader(k,val)
c.endheaders();c.send(os.urandom(65536));c.close();time.sleep(2)
assert not any(i['name']=='interrupted-fixture.iso' for i in v.call(P+'isos'))
passed('Interrupted upload never registered')
white=json.loads((O/'vm-manager-whitelist.json').read_text(encoding='utf-8'));assert white['uuid']=='e1b847ff-a4c1-4f60-8c0d-c1491ccee830'
assert white['uuid'] in __import__('subprocess').check_output([v.VBOX,'showmediuminfo','disk',white['path']],text=True)
pool=v.remote('zpool status -P vmtest');assert 'VBe1b847ff-' in pool,pool
ds='vmtest/.just-nas-vms/iso';old=v.remote('zfs get -H -o value refquota '+ds).strip();assert old=='none',old
used=int(v.remote('zfs get -Hp -o value usedbydataset '+ds).strip())
try:
 v.remote('zfs set refquota='+str(used+(1<<20))+' '+ds)
 body=bytearray(os.urandom(10<<20));body[16*2048+1:16*2048+6]=b'CD001'
 r=v.s.post(v.BASE+'/api/'+P+'isos',params={'name':'full-fixture.iso'},data=body,timeout=30);assert r.status_code==409,(r.status_code,r.text[:200])
finally:v.remote('zfs set refquota=none '+ds)
assert not any(i['name']=='full-fixture.iso' for i in v.call(P+'isos'))
assert not v.remote("find /mnt/vmtest/.just-nas-vms/iso -maxdepth 1 -name '.upload-*' -print").strip()
assert len(v.call(P+'isos'))==len(existing)
passed('Quota-induced full ISO filesystem rejects upload, cleans temporary data and preserves existing media')
(O/'vm-api-failures.json').write_text(json.dumps({'result':'PASS','checks':results},indent=2),encoding='utf-8')
