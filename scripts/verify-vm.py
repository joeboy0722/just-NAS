"""End-to-end acceptance against the existing just-nas VM.
Only /dev/sdb and /dev/sdc may be initialized. Verify their host VDI mapping first.
Credentials and reports are local disposable output artifacts.
"""
import argparse, json, os, pathlib, secrets, subprocess, sys, time
ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "output/test-deps"))
import requests
import smbclient
VBOX = r"C:\Program Files\Oracle\VirtualBox\VBoxManage.exe"
HOST = os.environ.get("NAS_TEST_HOST", "127.0.0.1")
SMB_PORT = int(os.environ.get("NAS_TEST_SMB_PORT", "1445"))
BASE = f"http://{HOST}:8080/api/"
OUT = ROOT / "output/vm-test"
OUT.mkdir(parents=True, exist_ok=True)
REPORT = OUT / "acceptance.json"
CRED = OUT / "test-credentials.json"
checks = []
s = requests.Session()
csrf = ""
def record(name, detail="passed"):
    checks.append({"check": name, "result": detail})
    REPORT.write_text(json.dumps(checks, ensure_ascii=False, indent=2), encoding="utf-8")
    print("PASS:", name, flush=True)
def api(path, method="GET", body=None, expected=200):
    r = s.request(method, BASE + path, json=body, headers={"X-CSRF-Token": csrf}, timeout=115)
    assert r.status_code == expected, (path, r.status_code, r.text)
    return r.json()
def wait_ready():
    for _ in range(120):
        try:
            r = requests.get(BASE + "auth/status", timeout=3)
            if r.status_code == 200: return r.json()
        except requests.RequestException: pass
        time.sleep(2)
    raise RuntimeError("NAS did not become ready")
def smb_read(share="Family"):
    with smbclient.open_file(chr(92)*2 + HOST + chr(92) + share + chr(92) + "proof.txt", "rb", port=SMB_PORT) as f:
        return f.read()
def smb_write(data, share="Family"):
    with smbclient.open_file(chr(92)*2 + HOST + chr(92) + share + chr(92) + "proof.txt", "wb", port=SMB_PORT) as f:
        f.write(data)
def verify_mapping():
    info = subprocess.check_output([VBOX, "showvminfo", "just-nas", "--machinereadable"], text=True)
    for port, filename in [(1,"test-a.vdi"), (2,"test-b.vdi")]:
        expected = str(OUT / filename)
        line = next((l for l in info.splitlines() if l.startswith('"SATA-'+str(port)+'-0"=')), "")
        assert line and json.loads(line.split("=",1)[1]).lower() == expected.lower(), ("unsafe disk mapping", line, expected)
    record("Only newly created VDI files on SATA ports 1 and 2")
def login(cred):
    global csrf
    time.sleep(1.1)
    r=api("auth/login","POST",{"password":cred["admin"]})
    csrf=r["csrf"]
def verify_files(cred):
    try:
        smbclient.register_session(HOST,username="family",password="wrong-password",port=SMB_PORT,connection_cache={})
    except Exception:
        record("Wrong SMB password rejected")
    else: raise AssertionError("Wrong password accepted")
    smbclient.register_session(HOST,username=cred["user"],password=cred["password"],port=SMB_PORT)
    original=b"just-nas VM acceptance: original file\n"
    smb_write(original)
    assert smb_read()==original
    record("Windows SMB authenticated write/read")
    try: smb_write(b"forbidden","ReadOnly")
    except OSError as exc:
        assert getattr(exc,"ntstatus",None)==0xc0000022, str(exc)
        record("SMB read-only share rejects writes")
    else: raise AssertionError("Read-only write accepted")
    tag="acceptance-"+str(time.time_ns())
    snapshot="testpool/files@"+tag
    api("storage/snapshots","POST",{"dataset":"testpool/files","tag":tag})
    smb_write(b"changed after snapshot\n")
    assert smb_read()!=original
    api("storage/snapshots/rollback","POST",{"name":snapshot,"confirm":snapshot})
    assert smb_read()==original
    record("Snapshot rollback restores actual SMB file content")
    api("storage/snapshots?name="+snapshot,"DELETE")
    r=s.delete(BASE+"storage/datasets?name=testpool/files&confirm=testpool/files",headers={"X-CSRF-Token":csrf})
    assert r.status_code==409
    record("Shared dataset deletion blocked")
    record("Create phase complete")

def run(phase):
    global csrf
    verify_mapping()
    status = wait_ready()
    if phase == "create":
        assert not status["initialized"], "Existing administrator found; refusing to overwrite."
        serial = (OUT / "serial.log").read_text(encoding="utf-8", errors="replace")
        import re
        token = re.findall(r"Just NAS setup token: ([0-9a-f]{64})", serial)[-1]
        cred={"admin":secrets.token_urlsafe(24),"user":"family","password":secrets.token_urlsafe(24)}
        CRED.write_text(json.dumps(cred),encoding="utf-8")
        csrf=api("auth/setup","POST",{"token":token,"password":cred["admin"]})["csrf"]
        record("One-time administrator setup")
        assert requests.get(BASE+"storage/pools").status_code==401
        assert s.post(BASE+"storage/pools",json={}).status_code==403
        record("Unauthenticated and CSRF requests rejected")
        diag=api("system/diagnostics")
        assert all(c["ok"] for c in diag["checks"]), diag
        record("Kernel ZFS and file sharing services ready")
        disks=api("storage/disks")
        selected=[d for d in disks if d["name"] in ("sdb","sdc")]
        assert len(selected)==2 and all(d["available"] and d["size"]==20*1024**3 for d in selected), selected
        api("storage/pools","POST",{"name":"testpool","confirm":"testpool","raid_type":"mirror","devices":[d["path"] for d in selected]})
        pools=api("storage/pools")
        assert any(p["name"]=="testpool" and p["health"]=="ONLINE" for p in pools)
        assert api("auth/status")["persistent"]
        record("Mirror created on test VDIs and settings persisted")
        assert all(not d["available"] for d in api("storage/disks") if d["name"] in ("sdb","sdc"))
        record("Used pool members cannot be selected again")
        api("storage/datasets","POST",{"name":"testpool/files","compression":"lz4","mountpoint":""})
        api("users","POST",{"name":cred["user"],"password":cred["password"]})
        for name,ro in [("Family",False),("ReadOnly",True)]:
            api("shares","POST",{"name":name,"protocol":"smb","path":"/mnt/testpool/files","read_only":ro,"users":["family"]})
        api("shares","POST",{"name":"Linux","protocol":"nfs","path":"/mnt/testpool/files","allowed_hosts":os.environ.get("NAS_TEST_NFS_CLIENT", "10.0.2.2")+"/32"})
        record("Dataset, SMB account, SMB and restricted NFS shares created")
        verify_files(cred)
    else:
        cred=json.loads(CRED.read_text())
        if phase=="files":
            login(cred);verify_files(cred);return
        if phase=="cycle":
            login(cred)
            api("system/reboot","POST",{"confirm":"reboot"})
            deadline=time.monotonic()+240
            while True:
                time.sleep(2)
                try:
                    response=s.get(BASE+"auth/status",timeout=3)
                    if response.status_code==200 and not response.json()["authenticated"]: break
                except requests.RequestException: pass
                if time.monotonic()>=deadline: raise RuntimeError("Authenticated reboot did not complete")
            s.cookies.clear()
            status=wait_ready()
        assert status["initialized"] and not status["authenticated"] and status["persistent"],status
        login(cred)
        assert any(p["name"]=="testpool" and p["health"]=="ONLINE" for p in api("storage/pools"))
        assert len(api("shares"))==3
        assert any(u["name"]=="family" for u in api("users"))
        smbclient.register_session(HOST,username=cred["user"],password=cred["password"],port=SMB_PORT)
        assert smb_read()==b"just-nas VM acceptance: original file\n"
        record("After reboot: administrator, pool, accounts, shares and file content restored")
        assert all(c["ok"] for c in api("system/diagnostics")["checks"])
        record("After reboot: all NAS services healthy")
if __name__=="__main__":
    p=argparse.ArgumentParser();p.add_argument("phase",choices=["create","files","reboot","cycle"]);args=p.parse_args()
    if REPORT.exists(): checks=json.loads(REPORT.read_text(encoding="utf-8"))
    try: run(args.phase)
    except Exception as e:
        checks.append({"check":args.phase,"result":"FAILED: "+str(e)})
        REPORT.write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding="utf-8")
        raise

