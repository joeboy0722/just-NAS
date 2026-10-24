"""Regression for the real Chrome headers reported by the NAS owner."""
import json,os,pathlib,time
import requests

root=pathlib.Path(__file__).resolve().parents[1]
credentials=json.loads((root/"output/vm-test/test-credentials.json").read_text())
host=os.environ.get("NAS_TEST_HOST", "127.0.0.1")
base=f"http://{host}:8080/api/"
headers={"Origin":f"http://{host}","Referer":f"http://{host}:8080/","Sec-Fetch-Site":"same-origin","Sec-Fetch-Mode":"cors","Sec-Fetch-Dest":"empty"}
results=[]
def check(name,response,status):
    assert response.status_code==status,(name,response.status_code,response.text)
    results.append({"check":name,"status":response.status_code,"result":"PASS"})
    print("PASS:",name,flush=True)

time.sleep(1.1)
s=requests.Session()
r=s.post(base+"auth/login",headers=headers,json={"password":credentials["admin"]},timeout=10)
check("Login with owner's exact portless Origin headers",r,200)
csrf=r.json()["csrf"]
check("Session can read NAS storage",s.get(base+"storage/pools",headers=headers,timeout=10),200)
check("Portless Origin does not bypass CSRF",s.post(base+"auth/logout",headers=headers,json={},timeout=10),403)
check("Same-origin logout with CSRF",s.post(base+"auth/logout",headers={**headers,"X-CSRF-Token":csrf},json={},timeout=10),200)
for name,overrides in [
    ("Missing Referer rejected",{"Referer":""}),
    ("Cross-port same-site rejected",{"Sec-Fetch-Site":"same-site"}),
    ("Different explicit Origin port rejected",{"Origin":f"http://{host}:9000"}),
    ("Foreign Origin rejected",{"Origin":"http://evil.test"}),
    ("Foreign Referer rejected",{"Referer":"http://evil.test/"}),
    ("Cross-site request rejected",{"Sec-Fetch-Site":"cross-site"}),
]:
    check(name,requests.post(base+"auth/login",headers={**headers,**overrides},json={"password":credentials["admin"]},timeout=10),403)
time.sleep(1.1)
check("SMB password still cannot log into management",requests.post(base+"auth/login",headers=headers,json={"password":credentials["password"]},timeout=10),401)
(root/"output/vm-test/browser-origin-regression.json").write_text(json.dumps(results,indent=2),encoding="utf-8")
