"use strict";
window.initStores = function ({api, button, upload}) {
 const $=id=>document.getElementById(id);
 let state=null, busy=false;
 const sections={};
 const labels={active:"運作中",disabled:"已停用",stopped:"已停止",failed:"失敗",unavailable:"無法使用",restoring:"恢復中",unknown:"無法確認",incompatible:"版本不相容","pending-reboot":"重新啟動後套用"};
 function element(tag,text,cls){const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(cls)n.className=cls;return n;}
 function message(parent,text){parent.append(element("p",text,"field-help"));}
 function activate(id){document.querySelectorAll(".nav-item,.section-page").forEach(x=>x.classList.remove("active"));sections[id].section.classList.add("active");sections[id].nav.classList.add("active");$("page-title").textContent=sections[id].title;document.body.classList.remove("nav-open");refresh().catch(showError);}
 function showError(e){for(const s of Object.values(sections)){s.notice.textContent=e.message;s.notice.setAttribute("role","alert");}}
 for(const [id,title,description] of [["extensions","系統擴充","為 NAS 添加硬體支援與系統能力。核心預設不啟動容器引擎。"],["apps","應用程式商店","由管理員安裝、全家使用。應用只可存取明確授權的資料。"]]){
  const nav=element("a",title,"nav-item");nav.href="#"+id;nav.id="nav-"+id;nav.onclick=e=>{e.preventDefault();activate(id);};document.querySelector(".nav-menu").append(nav);
  const section=element("section",undefined,"section-page");section.id="sec-"+id;$("sec-dashboard").parentElement.append(section);
  const panel=element("div",undefined,"panel");panel.append(element("h3",title));message(panel,description);
  const notice=element("div","登入後載入套件資訊","notice");notice.setAttribute("aria-live","polite");panel.append(notice);
  const actions=element("div",undefined,"header-actions");panel.append(actions);
  actions.append(button("設定儲存位置",()=>configure()),button("線上目錄",()=>catalog(id)),button("離線匯入",()=>importFile(id)));
  if(id==="extensions")actions.append(button("SSH 公鑰",()=>window.nasForm("SSH 登入公鑰",[{key:"public_key",label:"Ed25519 公鑰",value:state?.state.ssh_public_key||"",required:true}],v=>api("extensions/ssh-key","POST",v),"SSH 使用 root 帳號與公鑰登入。伺服器指紋："+(state?.ssh_fingerprint||"安裝並啟用後提供"))));
  const installed=element("div"),imports=element("div"),available=element("div");panel.append(element("h3","已安裝"),installed,element("h3","待安裝套件"),imports,available);section.append(panel);
  sections[id]={section,nav,title,notice,installed,imports,available};
 }
 async function configure(){const pools=window.nasData().pools.map(p=>({value:p.name,label:p.name}));if(!pools.length)throw Error("請先建立儲存池");return window.nasForm("擴充儲存位置",[{key:"pool",label:"儲存池",type:"select",options:pools}],async v=>{await api("extensions/storage","POST",v);await refresh();},"建立專用系統資料集，保存套件、映像及應用資料。第一版不提供搬移。既有同名資料集不會被接管。");}
 async function importFile(id){
  if(!state?.mounted)throw Error("請先設定可用的擴充儲存位置");const input=element("input");input.type="file";input.accept=".jnpkg";
  input.onchange=async()=>{if(!input.files.length)return;const s=sections[id];busy=true;try{await upload(id+"/import",input.files[0],p=>s.notice.textContent="上傳套件 "+p+"%；完成後校驗內容");await refresh(true);}catch(e){showError(e);}finally{busy=false;}};input.click();
 }
 async function install(id,item){
  const m=item.package.manifest,fields=[];const isApp=m.kind==="app", currentApp=state.apps.apps[m.id];
  let enginePending=null,engineEntry=null,engineNotice="";
  const engineVersion=m.dependencies?.["container-engine"],currentEngine=state.state.installed["container-engine"];
  const needsEngine=isApp&&(!currentEngine?.enabled||currentEngine.status!=="active"||currentEngine.package.manifest.version!==engineVersion);
  if(needsEngine){
   enginePending=state.imports.find(v=>v.package.manifest.id==="container-engine"&&v.package.manifest.version===engineVersion);
   if(!enginePending){try{const c=await api("extensions/catalog");engineEntry=c.entries.find(v=>v.id==="container-engine"&&v.version===engineVersion);}catch(e){engineNotice=e.message;}}
   if(enginePending||engineEntry)fields.push({key:"install_engine",label:"一併安裝 Docker 容器引擎（系統服務權限）",type:"checkbox",value:false});
   else engineNotice="請先從系統擴充匯入相容的 Docker 容器引擎離線包。"+engineNotice;
  }
  if(isApp){fields.push({key:"reuse_data",label:"若有此應用的保留資料，使用保留資料",type:"checkbox",value:false},{key:"bind_ip",label:"NAS 區網位址",type:"select",options:state.ips,value:currentApp?.bind_ip||state.ips[0]},{key:"port",label:"網頁連接埠",type:"number",value:currentApp?.port||18080,min:1024,max:65535},{key:"mount_dataset",label:"授權內容資料空間（既有空間須具備檔案權限）",type:"select",value:currentApp?.mount_dataset||"",options:[{value:"",label:"應用自己的內容資料夾（建議）"},...window.nasData().datasets.filter(d=>d.name.includes("/")&&!d.name.split("/").some(n=>n.startsWith("."))).map(d=>({value:d.name,label:d.name}))]},{key:"read_only",label:"內容資料僅可讀取",type:"checkbox",value:currentApp?.read_only??true});}
  if(!item.package.official)fields.push({key:"confirm_unofficial",label:"我信任此非官方套件及其宣告權限",type:"checkbox",value:false});
  const help=[m.name+" "+m.version,item.package.official?"官方簽章已驗證":"非官方套件；驅動可取得系統核心權限", "解壓內容："+NAS.bytes(m.installed_bytes),"需求："+(Object.entries(m.dependencies||{}).map(([k,v])=>(k==="container-engine"?"Docker 容器引擎":k)+" "+v).join("、")||"無"),"權限："+(m.permissions||[]).map(p=>({"kernel-modules":"系統核心與硬體","system-service":"系統服務管理","selected-content-dataset":"選定的內容資料空間","lan-port":"區網服務連接埠"}[p]||p)).join("、"),"授權："+m.license,isApp&&m.app?"資源上限："+m.app.cpus+" CPU、"+m.app.memory_mb+" MiB 記憶體":"",m.reboot?"安裝後需要重新啟動":"",engineEntry?"容器引擎另需下載 "+NAS.bytes(engineEntry.bytes):"",enginePending?"容器引擎已備妥離線包，解壓內容 "+NAS.bytes(enginePending.package.manifest.installed_bytes):"",engineNotice,isApp?"應用更新會先停止服務並拍攝資料快照；更新失敗嘗試恢復舊版。應用入口由應用自行管理登入，靜態網站示範不需登入。":""].filter(Boolean).join("。 ");
  window.nasForm("安裝／更新 "+m.name,fields,async v=>{
   if(needsEngine){
    if(!v.install_engine)throw Error(engineNotice||"請勾選一併安裝容器引擎");
    if(!enginePending){const downloaded=await api("extensions/download","POST",{id:"container-engine"});await waitTask(downloaded.task_id);const pending=await api("extensions");enginePending=pending.imports.find(v=>v.package.manifest.id==="container-engine"&&v.package.manifest.version===engineVersion);if(!enginePending)throw Error("下載後找不到相容的容器引擎");}
    const engineTask=await api("extensions/install","POST",{token:enginePending.token});await waitTask(engineTask.task_id);
   }
   const r=await api(id+"/install","POST",{...v,token:item.token,port:Number(v.port||0)});sections[id].notice.textContent="安裝任務已建立："+r.task_id+"。請至任務中心查看結果。";await refresh();},help);
 }

 async function waitTask(id){
  const until=Date.now()+30*60*1000;
  while(Date.now()<until){const jobs=await api("tasks"),job=jobs.find(j=>j.id===id);if(!job)throw Error("找不到安裝任務，請至任務中心確認");
   const note=document.querySelector(".modal-backdrop.active:last-child .field-help");if(note)note.textContent=job.progress||"等待系統回報執行階段";
   if(!["running","queued"].includes(job.state)){if(job.state!=="succeeded")throw Error(job.error||"容器引擎安裝失敗");return;}await new Promise(resolve=>setTimeout(resolve,1000));
  }throw Error("等待時間已到；任務仍由系統管理，請至任務中心確認結果");
 }
 async function action(id,resource,action){
  if(action==="remove"&&!(await NAS.confirm("移除 "+resource+"？應用資料與離線快取預設保留。",resource)))return;
  if(action==="remove"&&id==="apps"){
   return window.nasForm("移除應用",[{key:"delete_data",label:"同時永久刪除應用自己的資料（不刪除外部授權資料空間）",type:"checkbox",value:false},{key:"confirm",label:"如要刪除資料，請輸入 "+resource,value:""}],async v=>{await api(id+"/action","POST",{...v,id:resource,action});await refresh();},"預設保留資料。仍被快照保護的資料集可能無法刪除，將明確回報失敗。");
  }
  const r=await api(id+"/action","POST",{id:resource,action});sections[id].notice.textContent="已建立任務 "+r.task_id;await refresh();
 }
 async function catalog(id){
  const s=sections[id];s.available.replaceChildren(element("p","正在取得官方簽章目錄…"));try{
   const c=await api(id+"/catalog");s.available.replaceChildren(element("h3","官方線上目錄"));if(c.source==="bundled")message(s.available,"使用映像附帶的已簽署目錄；下載需對應套件已正式上架，也可離線匯入。");
   const entries=c.entries.filter(v=>(v.kind==="app")===(id==="apps"));if(!entries.length)message(s.available,"此目錄尚無套件。");
   for(const e of entries){const row=element("div",undefined,"store-card");row.append(element("strong",e.name),element("span",e.version+" · "+NAS.bytes(e.bytes)+" · "+e.license));row.append(button("下載至待安裝",async()=>{const r=await api(id+"/download","POST",{id:e.id});s.notice.textContent="下載任務 "+r.task_id+"；完成後可在待安裝套件檢視權限並安裝。";}));s.available.append(row);}
  }catch(e){s.available.replaceChildren(element("p",e.message,"notice"));}
 }
 async function refresh(force=false){
  if(busy&&!force)return;const selected=Object.values(sections).some(s=>s.section.classList.contains("active"));if(!selected&&!force)return;
  state=await api("extensions");const appData=await api("apps");
  for(const [id,s]of Object.entries(sections)){
   s.notice.textContent=state.restoring?"正在恢復系統擴充與應用，完成前暫停商店操作…":state.state.dataset?(state.mounted?"儲存位置："+state.state.dataset:"儲存位置未掛載：相關擴充與應用無法使用"):"尚未設定擴充儲存位置；請先選擇儲存池。";
   s.installed.replaceChildren();s.imports.replaceChildren();
   const items=id==="apps"?Object.values(state.apps.apps):Object.values(state.state.installed);
   if(!items.length)message(s.installed,"尚未安裝"+s.title+"項目。");
   for(const v of items){const m=v.package.manifest;const row=element("div",undefined,"store-card");row.append(element("strong",m.name),element("span",m.version+" · "+(state.restoring&&v.enabled?"恢復中":labels[v.status]||v.status),"status "+(v.status==="active"?"ok":["failed","unavailable","incompatible"].includes(v.status)?"danger":"")));if(v.error)message(row,v.error);const controls=element("div",undefined,"header-actions");
    if(id==="apps"){
     if(v.status==="active"){const a=element("a","開啟應用","btn btn-secondary btn-sm");a.href="http://"+v.bind_ip+":"+v.port+"/";a.target="_blank";a.rel="noopener";controls.append(a);}
     controls.append(button(v.status==="active"?"停止":"啟動",()=>action(id,m.id,v.status==="active"?"stop":"start")),button("日誌",async()=>{const r=await api("apps/"+encodeURIComponent(m.id)+"/logs");const box=window.nasForm("應用日誌",[],async()=>{},"僅顯示最近 150 筆輸出");const pre=document.createElement("pre");pre.textContent=r.logs;pre.style.whiteSpace="pre-wrap";box.querySelector("form").prepend(pre); }));
    }else controls.append(button(v.status==="failed"?"重試啟動":v.enabled?"停用":"啟用",()=>action(id,m.id,v.status==="failed"?"enable":v.enabled?"disable":"enable")));
    controls.append(button("移除",()=>action(id,m.id,"remove")));row.append(controls);s.installed.append(row);
   }
   const imports=(id==="apps"?appData:state).imports;
   if(!imports.length)message(s.imports,"尚無套件。可線上下載，或匯入完整 .jnpkg 離線包。");
   for(const v of imports){const m=v.package.manifest;const row=element("div",undefined,"store-card");row.append(element("strong",m.name),element("span",m.version+" · "+NAS.bytes(m.installed_bytes)+" · "+(v.package.official?"官方簽章":"非官方")),button("檢視與安裝",()=>install(id,v)),button("移除匯入",async()=>{await api(id+"/discard","POST",{token:v.token});await refresh();}));s.imports.append(row);}
  }
 }
 window.refreshStores=()=>refresh();
};
