document.addEventListener('DOMContentLoaded', () => {
'use strict';
const $ = id => document.getElementById(id);
let csrf = '', initialized = false, loggedIn = false, refreshing = false;
const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const bytes = v => {if(!v)return '0 B';const i=Math.min(4,Math.floor(Math.log(v)/Math.log(1024)));return (v/1024**i).toFixed(1)+' '+['B','KB','MB','GB','TB'][i]};
const val = id => $(id).value.trim();
function toast(message,error=false){const el=document.createElement('div');el.className='toast '+(error?'toast-error':'toast-success');el.textContent=message;$('toast-container').append(el);setTimeout(()=>el.remove(),7000)}
function open(id){$(id).classList.add('active')}
function close(id){$(id).classList.remove('active')}
async function api(path, method='GET', body) {
 const res=await fetch('/api/'+path,{method,headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(110000)});
 const data=await res.json();
 if(!res.ok){if(res.status===401){loggedIn=false;open('auth-modal')}throw Error(data.error||'請求失敗 ('+res.status+')')}
 return data;
}
function authUI(status){
 if(status.recovery_error){$('auth-error').textContent='設定還原失敗，已停止初始化以保護原有設定：'+status.recovery_error;$('auth-submit').disabled=true;open('auth-modal');return}
 initialized=status.initialized;loggedIn=status.authenticated;csrf=status.csrf||'';
 $('auth-title').textContent=initialized?'管理員登入':'首次設定';
 $('auth-token-group').hidden=initialized;
 $('auth-help').textContent=initialized?'輸入管理員密碼。':'從 VM 主控台取得 setup token，設定至少 12 字元的管理員密碼。建立第一個儲存池後，帳號才會永久保存。';
 if(!loggedIn)open('auth-modal');else close('auth-modal');
}
$('auth-form').onsubmit=async e=>{e.preventDefault();const b=$('auth-submit');b.disabled=true;try{
 const result=await api('auth/'+(initialized?'login':'setup'),'POST',{password:$('auth-password').value,token:val('auth-token')});csrf=result.csrf;initialized=true;loggedIn=true;$('auth-password').value='';$('auth-token').value='';close('auth-modal');await refresh();
}catch(err){$('auth-error').textContent=err.message}finally{b.disabled=false}};
$('btn-logout').onclick=async()=>{try{await api('auth/logout','POST');loggedIn=false;authUI(await api('auth/status'))}catch(err){toast(err.message,true)}};
$('btn-password').onclick=()=>open('password-modal');
$('password-form').onsubmit=async e=>{e.preventDefault();const b=$('password-submit');b.disabled=true;try{if($('password-new').value!==$('password-confirm').value)throw Error('兩次新密碼不一致');const r=await api('auth/password','POST',{current_password:$('password-current').value,password:$('password-new').value});csrf=r.csrf;$('password-form').reset();close('password-modal');toast('密碼已更新，其他登入工作階段已登出')}catch(err){toast(err.message,true)}finally{b.disabled=false}};
document.querySelectorAll('[data-close]').forEach(b=>b.onclick=()=>close(b.dataset.close));
document.querySelectorAll('.nav-item').forEach(a=>a.onclick=e=>{e.preventDefault();document.querySelectorAll('.nav-item,.section-page').forEach(x=>x.classList.remove('active'));a.classList.add('active');$('sec-'+a.hash.slice(1)).classList.add('active');$('page-title').textContent=a.textContent.trim()});
function render(id, rows, cols){$(id).innerHTML=rows.length?rows.join(''):'<tr><td colspan="'+cols+'" class="empty-row">尚無資料</td></tr>'}
const cells = values => values.map(v=>'<td>'+esc(v)+'</td>').join('');
function button(label,fn){const b=document.createElement('button');b.className='btn btn-secondary btn-sm';b.textContent=label;b.onclick=async()=>{b.disabled=true;try{await fn()}catch(err){toast(err.message,true)}finally{b.disabled=false}};return b}
function actions(tbody, index, items){const row=$(tbody).rows[index];const td=row.insertCell();items.forEach(([label,fn])=>td.append(button(label,fn)))}
async function destroy(path,name){if(prompt('此操作會刪除資料。請輸入完整名稱確認：'+name)!==name)return;await api(path+'?name='+encodeURIComponent(name)+'&confirm='+encodeURIComponent(name),'DELETE');toast('已刪除');await refresh()}
async function refresh(){
 if(!loggedIn||refreshing)return;refreshing=true;
 const errors=[];
 async function load(label,fn){try{await fn()}catch(err){errors.push(label+'：'+err.message)}}
 await Promise.all([
 load('監控',async()=>{const s=await api('system/status');$('val-cpu-pct').textContent=s.cpu_usage_pct.toFixed(1)+'%';$('fill-cpu').style.width=s.cpu_usage_pct+'%';$('val-loadavg').textContent='Load: '+s.load_avg.map(n=>n.toFixed(2)).join(', ');$('val-ram-pct').textContent=s.ram_usage_pct.toFixed(1)+'%';$('fill-ram').style.width=s.ram_usage_pct+'%';$('val-ram-details').textContent=bytes(s.ram_used)+' / '+bytes(s.ram_total);$('val-uptime').textContent=s.uptime_hours.toFixed(1)+' 小時';$('val-os-info').textContent=s.os_info+' ('+s.arch+')';$('sys-hostname').textContent=s.hostname}),
 load('儲存池',async()=>{const p=await api('storage/pools');render('dash-tbody-pools',p.map(s=>'<tr>'+cells([s.name,bytes(s.size),bytes(s.allocated),bytes(s.free),s.capacity_pct+'%',s.health])+'</tr>'),6);render('tbody-pools',p.map(s=>'<tr>'+cells([s.name,bytes(s.size),bytes(s.allocated),bytes(s.free),s.capacity_pct+'%',s.fragmentation,s.health])+'</tr>'),8);p.forEach((s,i)=>actions('tbody-pools',i,[['銷毀',()=>destroy('storage/pools',s.name)]]));$('ds-parent-select').replaceChildren(...p.map(s=>new Option(s.name,s.name)))}),
 load('資料集',async()=>{const d=await api('storage/datasets');render('tbody-datasets',d.map(s=>'<tr>'+cells([s.name,bytes(s.used),bytes(s.available),s.compression,s.mountpoint])+'</tr>'),6);d.forEach((s,i)=>actions('tbody-datasets',i,s.name.includes('/')?[['快照',()=>{$('snap-dataset-input').value=s.name;open('modal-snapshot')}],['刪除',()=>destroy('storage/datasets',s.name)]]:[]))}),
 load('快照',async()=>{const snaps=await api('storage/snapshots');render('tbody-snapshots',snaps.map(s=>'<tr>'+cells([s.name,bytes(s.used),bytes(s.referenced)])+'</tr>'),4);snaps.forEach((s,i)=>actions('tbody-snapshots',i,[['還原',async()=>{if(prompt('還原會覆蓋目前內容，請輸入快照名稱：'+s.name)!==s.name)return;await api('storage/snapshots/rollback','POST',{name:s.name,confirm:s.name});toast('快照已還原');await refresh()}],['刪除',()=>destroy('storage/snapshots',s.name)]]))}),
 load('分享',async()=>{const ss=await api('shares');render('tbody-shares',ss.map(s=>'<tr>'+cells([s.name,s.protocol,s.path,s.read_only?'唯讀':'讀寫',s.protocol==='nfs'?s.allowed_hosts:s.guest_ok?'訪客':(s.users||[]).join(', '),s.comment])+'</tr>'),7);ss.forEach((s,i)=>actions('tbody-shares',i,[['移除',async()=>{if(!confirm('移除分享 '+s.name+'？檔案會保留。'))return;await api('shares?name='+encodeURIComponent(s.name)+'&protocol='+s.protocol,'DELETE');toast('分享已移除');await refresh()}]]))}),
 load('硬碟',async()=>{const disks=await api('storage/smart');render('tbody-disks',disks.map(d=>'<tr>'+cells([d.device,d.model||'虛擬硬碟／未知',bytes(d.size),d.temperature==null?'不支援':d.temperature+' °C',d.power_on_hours==null?'不支援':d.power_on_hours+' 小時',d.health==='UNKNOWN'?'未知／不支援 SMART':d.health])+'</tr>'),6)}),
 load('日誌',async()=>{const logs=await api('system/logs?limit=200');$('log-console').textContent=logs.map(l=>'['+l.timestamp+'] ['+l.level+'] ['+l.tag+'] '+l.message).join('\n')||'尚無日誌'}),
 load('帳號',async()=>{const users=await api('users');$('users-list').replaceChildren(...users.map(u=>{const d=document.createElement('div');d.textContent=u.name+' ';d.append(button('移除',async()=>{if(!confirm('移除帳號 '+u.name+'？'))return;await api('users?name='+encodeURIComponent(u.name),'DELETE');await refresh()}));return d}));$('share-users-select').replaceChildren(...users.map(u=>new Option(u.name,u.name)))}),
 load('登入狀態',async()=>{const s=await api('auth/status');$('persistence-status').textContent=s.persistent?'設定已保存於 ZFS':'尚未建立設定池：目前管理員設定暫存於記憶體，請先建立儲存池。'})
 ]);
 $('connection-status').textContent=errors.join('；');refreshing=false;
}
$('btn-refresh').onclick=refresh;
$('btn-reboot').onclick=async()=>{if(!confirm('確定重新啟動 NAS？分享會暫時中斷。'))return;try{await api('system/reboot','POST',{confirm:'reboot'});toast('NAS 正在重新啟動，請稍後重新整理並登入')}catch(err){toast(err.message,true)}};
$('btn-refresh-logs').onclick=refresh;
$('btn-clear-logs').onclick=async()=>{try{if(confirm('清除記憶體日誌？')){await api('system/logs','DELETE');await refresh()}}catch(err){toast(err.message,true)}};
$('btn-diagnostics').onclick=async()=>{try{const d=await api('system/diagnostics');$('diagnostics-output').textContent=d.checks.map(c=>(c.ok?'✓ ':'✕ ')+c.name+'\n'+(c.detail||'')).join('\n\n');open('diagnostics-modal')}catch(err){toast(err.message,true)}};
$('btn-open-create-pool').onclick=async()=>{open('modal-create-pool');$('pool-disks-container').textContent='掃描中…';try{const disks=await api('storage/disks');$('pool-disks-container').replaceChildren(...disks.map(d=>{const l=document.createElement('label');l.className='disk-item checkbox-group';const c=document.createElement('input');c.type='checkbox';c.name='pool-disks';c.value=d.path;c.disabled=!d.available;l.append(c,document.createTextNode(d.name+' — '+bytes(d.size)+' '+(d.reason||'可用')));return l}));if(!disks.length)$('pool-disks-container').textContent='找不到資料硬碟'}catch(err){$('pool-disks-container').textContent=err.message}};
$('btn-open-create-ds').onclick=()=>open('modal-create-dataset');
$('btn-open-create-share').onclick=()=>open('modal-create-share');
$('share-proto-select').onchange=()=>{const smb=val('share-proto-select')==='smb';$('group-guest-ok').hidden=!smb;$('group-share-users').hidden=!smb;$('group-allowed-hosts').style.display=smb?'none':'block'};
function submit(id,modal,fn){$(id).onclick=async()=>{const b=$(id);b.disabled=true;try{const result=await fn();if(result===false)return;if(modal)close(modal);toast('操作完成');await refresh()}catch(err){toast(err.message,true)}finally{b.disabled=false}}}
submit('btn-submit-create-pool','modal-create-pool',async()=>{const name=val('pool-name-input');const devices=[...document.querySelectorAll('input[name="pool-disks"]:checked')].map(c=>c.value);if(!name||!devices.length)throw Error('請輸入名稱並選擇硬碟');if(prompt('將初始化選取硬碟。請輸入儲存池名稱確認：'+name)!==name)return false;return api('storage/pools','POST',{name,raid_type:val('pool-raid-select'),devices,confirm:name})});
submit('btn-submit-create-ds','modal-create-dataset',()=>api('storage/datasets','POST',{name:val('ds-parent-select')+'/'+val('ds-name-input'),compression:val('ds-comp-select'),mountpoint:val('ds-mount-input')}));
submit('btn-submit-snap','modal-snapshot',()=>api('storage/snapshots','POST',{dataset:val('snap-dataset-input'),tag:val('snap-tag-input')}));
submit('btn-submit-create-share','modal-create-share',()=>api('shares','POST',{name:val('share-name-input'),protocol:val('share-proto-select'),path:val('share-path-input'),comment:val('share-comment-input'),read_only:$('share-readonly-input').checked,guest_ok:$('share-guest-input').checked,allowed_hosts:val('share-hosts-input'),users:[...$('share-users-select').selectedOptions].map(o=>o.value)}));
$('user-form').onsubmit=async e=>{e.preventDefault();const b=$('btn-create-user');b.disabled=true;try{await api('users','POST',{name:val('user-name'),password:$('user-password').value});$('user-password').value='';toast('帳號已建立');await refresh()}catch(err){toast(err.message,true)}finally{b.disabled=false}};
api('auth/status').then(s=>{authUI(s);if(loggedIn)refresh()}).catch(err=>{$('auth-error').textContent=err.message;open('auth-modal')});
setInterval(refresh,10000);
});

