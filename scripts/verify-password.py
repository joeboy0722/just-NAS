import os
"""Rotate only the disposable VM administrator password and verify session expiry."""
import json, pathlib, secrets, time
import requests

root=pathlib.Path(__file__).resolve().parents[1]
path=root/"output/vm-test/test-credentials.json"
cred=json.loads(path.read_text())
base="http://"+os.environ.get("NAS_TEST_HOST", "127.0.0.1")+":8080/api/"
first,other=requests.Session(),requests.Session()
def login(session,password):
    time.sleep(1.1)
    r=session.post(base+"auth/login",json={"password":password},timeout=10)
    r.raise_for_status()
    return r.json()["csrf"]
csrf=login(first,cred["admin"])
login(other,cred["admin"])
new=secrets.token_urlsafe(24)
time.sleep(1.1)
r=first.post(base+"auth/password",json={"current_password":cred["admin"],"password":new},headers={"X-CSRF-Token":csrf},timeout=10)
r.raise_for_status()
old=cred["admin"];cred["admin"]=new
temporary=path.with_suffix(".tmp")
temporary.write_text(json.dumps(cred),encoding="utf-8");temporary.replace(path)
assert other.get(base+"storage/pools").status_code==401,"Old session still active"
time.sleep(1.1)
assert requests.post(base+"auth/login",json={"password":old}).status_code==401,"Old password still accepted"
login(requests.Session(),new)
print("PASS: administrator password rotation, old password rejection, other session invalidation")
