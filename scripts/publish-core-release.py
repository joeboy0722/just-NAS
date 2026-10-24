"""Validate five assets; publish only with --publish. Existing public releases are immutable."""
import argparse,pathlib,json,subprocess,os,hashlib,requests,re
R=pathlib.Path(__file__).resolve().parents[1];REPO='joeboy0722/just-NAS';API='https://api.github.com/repos/'+REPO
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  while b:=f.read(1024**2):h.update(b)
 return h.hexdigest()
def git(*args):return subprocess.check_output(['git','-c','safe.directory='+R.as_posix(),*args],cwd=R,text=True).strip()
def main():
 p=argparse.ArgumentParser();p.add_argument('--directory',type=pathlib.Path,required=True);p.add_argument('--tag',default='v2.1.0-rc2');p.add_argument('--publish',action='store_true');a=p.parse_args();assert re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+-rc[0-9]+',a.tag)
 iso='just-NAS-'+a.tag+'-x86_64.iso';support='just-NAS-'+a.tag+'-support.tar.gz';expected={iso,support,'LICENSE','SOURCES.md','SHA256SUMS'};assert {f.name for f in a.directory.iterdir()}==expected
 entries={l.split('  ',1)[1]:l.split('  ',1)[0] for l in (a.directory/'SHA256SUMS').read_text().splitlines()};assert set(entries)==expected-{'SHA256SUMS'}
 for name,digest in entries.items():assert sha(a.directory/name)==digest,name+' checksum differs'
 print('PASS exactly five checksummed release assets',flush=True)
 if not a.publish:return
 assert not git('status','--porcelain'),'Commit before publishing';commit=git('rev-parse','HEAD');assert git('ls-remote','origin','refs/heads/main').split()[0]==commit,'Push validated sources to main first'
 token=subprocess.run(['git','-c','safe.directory='+R.as_posix(),'credential','fill'],input='protocol=https\nhost=github.com\n\n',text=True,capture_output=True,check=True,env={**os.environ,'GIT_TERMINAL_PROMPT':'0'});auth=dict(l.split('=',1) for l in token.stdout.splitlines() if '=' in l);s=requests.Session();s.headers.update({'Authorization':'Bearer '+auth['password'],'Accept':'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'})
 def api(method,path,**kwargs):
  r=s.request(method,API+path,timeout=60,**kwargs);r.raise_for_status();return r.json() if r.content else None
 size=(a.directory/iso).stat().st_size
 body='[**下載 just-NAS '+a.tag+' ISO（'+format(size/1024**2,'.0f')+' MiB）**](https://github.com/'+REPO+'/releases/download/'+a.tag+'/'+iso+')\n\nrc1 已撤下，本版取代 rc1；原 rc4 與 stores-v1 擴充下載保留。\n\n## 新增\n\n- 自有文字啟動畫面與真實階段狀態；啟動時 OK 紅色、FAIL 綠色，完成後 Welcome to Just NAS. 並恢復正常配色。\n- 多網卡／IPv4／IPv6 管理網址，憑證涵蓋提醒、地址分頁、Alt+F2 維護登入、純文字排錯模式。\n- 失敗不偽裝成功，背景擴充不阻塞核心。原有帳號與資料保留。\n\n## 附件簡化\n\n只需下載 ISO 即可製作開機 USB；SHA256SUMS 可驗證下載。五個手動附件為 ISO、SHA256SUMS、LICENSE、SOURCES.md、support 資料包。建置／測試／版本清單／操作說明／自有原始碼／完整授權資料已合併至 support，不再各自占一列。GitHub 自動提供的 Source code zip／tar.gz 仍會存在。\n\n## 驗證與授權\n\nGo、ISO 稽核與 just-nas VM 已驗證真實終端機、多 IP、IPv6、容器網路排除、反色、分享與缺池故障恢復、無資料碟／斷網純文字啟動，以及原 SMB／NFS／HTTPS／帳號隔離。詳見 support 內測試報告與截圖。實機 GPU、特殊控制器、USB、SMART 與實際斷電仍待硬體驗收，這是預發布版。\n\njust-NAS 自有程式、文件與示範採 **MIT**，見 LICENSE。整個 ISO 與第三方元件不是統一 MIT；各元件保留原授權。對應來源與校驗資訊見 SOURCES.md，既有來源分卷維持公開提供。'
 rels=api('GET','/releases');rel=next((v for v in rels if v['tag_name']==a.tag),None)
 if rel:assert rel['draft'],'Refusing to overwrite published release'
 else:rel=api('POST','/releases',json={'tag_name':a.tag,'target_commitish':commit,'name':'just-NAS '+a.tag+' — 品牌啟動與精簡附件','body':body,'draft':True,'prerelease':True})
 existing={v['name']:v for v in api('GET','/releases/'+str(rel['id'])+'/assets')};assert set(existing)<=expected
 for name in [iso,support,'LICENSE','SOURCES.md','SHA256SUMS']:
  path=a.directory/name;digest=sha(path)
  if name in existing:
   v=existing[name];assert v['size']==path.stat().st_size and v.get('digest')=='sha256:'+digest,'Draft asset differs';continue
  print('UPLOAD',name,path.stat().st_size,flush=True)
  with path.open('rb') as f:r=s.post(rel['upload_url'].split('{',1)[0],params={'name':name},data=f,headers={'Content-Type':'application/octet-stream'},timeout=(30,1200))
  r.raise_for_status();v=r.json();assert v['size']==path.stat().st_size and v.get('digest')=='sha256:'+digest
 assets=api('GET','/releases/'+str(rel['id'])+'/assets');assert {v['name'] for v in assets}==expected
 rel=api('PATCH','/releases/'+str(rel['id']),json={'draft':False,'prerelease':True,'name':'just-NAS '+a.tag+' — 品牌啟動與精簡附件','body':body,'make_latest':'false'})
 (R/'output/rc2-publication.json').write_text(json.dumps({'release':rel['html_url'],'commit':commit,'assets':[{k:v.get(k) for k in ['name','size','digest','browser_download_url']} for v in assets]},indent=2),encoding='utf-8');print('PUBLISHED',rel['html_url'],flush=True)
 # The user explicitly abandoned rc1; remove only its already-withdrawn draft.
 old=next((v for v in rels if v['tag_name']=='v2.1.0-rc1'),None)
 if old and old['draft']:
  backup=R/'output/abandoned-rc1';backup.mkdir(exist_ok=True);(backup/'release-before-delete.json').write_text(json.dumps(old,indent=2),encoding='utf-8');api('DELETE','/releases/'+str(old['id']));print('Removed abandoned rc1 release; Git tag retained',flush=True)
if __name__=='__main__':main()
