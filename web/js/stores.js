"use strict";
window.initStores = function ({api, button, upload}) {
 const $=id=>document.getElementById(id);
 let state=null, busy=false;
 const sections={};
 const labels={active:"運作中",disabled:"已停用",stopped:"已停止",failed:"失敗",incompatible:"版本不相容","pending-reboot":"重新啟動後套用"};
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
  if(id==="extensions")actions.append(button("SSH 公鑰",()=>window.nasForm("SSH 登入公鑰",[{key:"public_key",label:"Ed25519 公鑰",value:"",required:true}],v=>api("extensions/ssh-key","POST",v),"SSH 為系統管理權限，僅接受公鑰登入；不開放密碼登入。")));
  const installed=element("div"),imports=element("div"),available=element("div");panel.append(element("h3","已安裝"),installed,element("h3","待安裝套件"),imports,available);section.append(panel);
  sections[id]={section,nav,title,notice,installed,imports,available};
 }
 async function configure(){const pools=window.nasData().pools.map(p=>({value:p.name,label:p.name}));if(!pools.length)throw Error("請先建立儲存池");return window.nasForm("擴充儲存位置",[{key:"pool",label:"儲存池",type:"select",options:pools}],async v=>{await api("extensions/storage","POST",v);await refresh();},"建立專用系統資料集，保存套件、映像及應用資料。第一版不提供搬移。既有同名資料集不會被接管。");}
 async function importFile(id){
  if(!state?.mounted)throw Error("請先設定可用的擴充儲存位置");const input=element("input");input.type="file";input.accept=".jnpkg";
  input.onchange=async()=>{if(!input.files.length)return;const s=sections[id];busy=true;try{await upload(id+"/import",input.files[0],p=>s.notice.textContent="上傳套件 "+p+"%；完成後校驗內容");await refresh(true);}catch(e){showError(e);}finally{busy=false;}};input.click();
 }
 async function install(id,item){
  const m=item.package.manifest,fields=[];const isApp=m.kind==="app";
  if(isApp){fields.push({key:"reuse_data",label:"若有此應用的保留資料，使用保留資料",type:"checkbox",value:false},{key:"bind_ip",label:"NAS 區網位址",type:"select",options:state.ips},{key:"port",label:"網頁連接埠",type:"number",value:18080,min:1024,max:65535},{key:"mount_dataset",label:"授權內容資料空間",type:"select",options:[{value:"",label:"應用自己的內容資料夾"},...window.nasData().datasets.filter(d=>d.name.includes("/")&&!d.name.split("/").some(n=>n.startsWith("."))).map(d=>({value:d.name,label:d.name}))]},{key:"read_only",label:"內容資料僅可讀取",type:"checkbox",value:true});}
  if(!item.package.official)fields.push({key:"confirm_unofficial",label:"我信任此非官方套件及其宣告權限",type:"checkbox",value:false});
  const help=[m.name+" "+m.version,item.package.official?"官方簽章已驗證":"非官方套件；驅動可取得系統核心權限", "解壓內容："+NAS.bytes(m.installed_bytes),"需求："+(Object.entries(m.dependencies||{}).map(([k,v])=>k+" "+v).join("、")||"無"),"權限："+(m.permissions||[]).join("、"),"授權："+m.license,m.reboot?"安裝後需要重新啟動":"",isApp?"應用更新會先停止服務並拍攝資料快照；更新失敗嘗試恢復舊版。":""].filter(Boolean).join("。 ");
  window.nasForm("安裝／更新 "+m.name,fields,async v=>{const r=await api(id+"/install","POST",{...v,token:item.token,port:Number(v.port||0)});sections[id].notice.textContent="安裝任務已建立："+r.task_id+"。請至任務中心查看結果。";await refresh();},help);
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
   const c=await api(id+"/catalog");s.available.replaceChildren(element("h3","官方線上目錄"));
   const entries=c.entries.filter(v=>(v.kind==="app")===(id==="apps"));if(!entries.length)message(s.available,"此目錄尚無套件。");
   for(const e of entries){const row=element("div",undefined,"store-card");row.append(element("strong",e.name),element("span",e.version+" · "+NAS.bytes(e.bytes)+" · "+e.license));row.append(button("下載至待安裝",async()=>{const r=await api(id+"/download","POST",{id:e.id});s.notice.textContent="下載任務 "+r.task_id+"；完成後可在待安裝套件檢視權限並安裝。";}));s.available.append(row);}
  }catch(e){s.available.replaceChildren(element("p",e.message,"notice"));}
 }
 async function refresh(force=false){
  if(busy&&!force)return;const selected=Object.values(sections).some(s=>s.section.classList.contains("active"));if(!selected&&!force)return;
  state=await api("extensions");const appData=await api("apps");
  for(const [id,s]of Object.entries(sections)){
   s.notice.textContent=state.state.dataset?(state.mounted?"儲存位置："+state.state.dataset:"儲存位置未掛載：相關擴充與應用無法使用"):"尚未設定擴充儲存位置；請先選擇儲存池。";
   s.installed.replaceChildren();s.imports.replaceChildren();
   const items=id==="apps"?Object.values(state.apps.apps):Object.values(state.state.installed);
   if(!items.length)message(s.installed,"尚未安裝"+s.title+"項目。");
   for(const v of items){const m=v.package.manifest;const row=element("div",undefined,"store-card");row.append(element("strong",m.name),element("span",m.version+" · "+(labels[v.status]||v.status),"status"));if(v.error)message(row,v.error);const controls=element("div",undefined,"header-actions");
    if(id==="apps"){
     if(v.status==="active"){const a=element("a","開啟應用","btn btn-secondary btn-sm");a.href="http://"+v.bind_ip+":"+v.port+"/";a.target="_blank";a.rel="noopener";controls.append(a);}
     controls.append(button(v.enabled?"停止":"啟動",()=>action(id,m.id,v.enabled?"stop":"start")),button("日誌",async()=>{const r=await api("apps/"+encodeURIComponent(m.id)+"/logs");const box=window.nasForm("應用日誌",[],async()=>{},"僅顯示最近 150 筆輸出");const pre=document.createElement("pre");pre.textContent=r.logs;pre.style.whiteSpace="pre-wrap";box.querySelector("form").prepend(pre); }));
    }else controls.append(button(v.enabled?"停用":"啟用",()=>action(id,m.id,v.enabled?"disable":"enable")));
    controls.append(button("移除",()=>action(id,m.id,"remove")));row.append(controls);s.installed.append(row);
   }
   const imports=(id==="apps"?appData:state).imports;
   if(!imports.length)message(s.imports,"尚無套件。可線上下載，或匯入完整 .jnpkg 離線包。");
   for(const v of imports){const m=v.package.manifest;const row=element("div",undefined,"store-card");row.append(element("strong",m.name),element("span",m.version+" · "+NAS.bytes(m.installed_bytes)+" · "+(v.package.official?"官方簽章":"非官方")),button("檢視與安裝",()=>install(id,v)));s.imports.append(row);}
  }
 }
 window.refreshStores=()=>refresh();
};
