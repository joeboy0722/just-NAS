/* Live browser verification through the certificate-pinned loopback VM proxy. */
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'..'),out=path.join(root,'output/stores-review'),base='http://127.0.0.1:8765';
async function main(){
 const browser=await chromium.launch({executablePath:process.env.NAS_CHROMIUM_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe'});
 try{
  const context=await browser.newContext({viewport:{width:1366,height:900}}),page=await context.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base);await page.locator('#auth-password').fill(JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/test-credentials.json'),'utf8')).admin);await page.locator('#auth-submit').click();await page.locator('#auth-modal').waitFor({state:'hidden'});
  const status=await (await context.request.get(base+'/api/auth/status')).json();const headers={'X-CSRF-Token':status.csrf};
  async function api(p,m='GET',data){const r=await context.request.fetch(base+'/api/'+p,{method:m,headers,data});assert(r.ok(),(await r.text()).slice(0,1000));return r.json();}
  async function wait(id){for(let n=0;n<120;n++){const j=(await api('tasks')).find(v=>v.id===id);if(!['running','queued'].includes(j.state)){assert.equal(j.state,'succeeded',j.error);return;}await new Promise(r=>setTimeout(r,1000));}throw Error('task timeout');}
  async function upload(){const r=await context.request.post(base+'/api/apps/import',{headers:{...headers,'Content-Type':'application/octet-stream'},data:fs.readFileSync(path.join(root,'output/store-packages/static-site-1.0.0.jnpkg'))});assert(r.ok(),(await r.text()).slice(0,1000));return r.json();}
  const state=await api('apps');assert.equal(state.state.dataset,'storetest/.just-nas-extensions');const old=state.apps.apps['static-site'];
  const first=await upload();await wait((await api('apps/install','POST',{token:first.token,port:18081,bind_ip:old.bind_ip,read_only:true})).task_id);
  await upload();await page.evaluate(()=>NAS.navigate('apps'));await page.waitForTimeout(500);await page.locator('#sec-apps .store-card').filter({hasText:'官方簽章'}).last().getByRole('button',{name:'檢視與安裝'}).click();await page.getByRole('heading',{name:'安裝／更新 靜態網站示範'}).waitFor();assert.equal(await page.getByLabel('網頁連接埠',{exact:true}).inputValue(),'18081');assert.equal(await page.getByLabel('NAS 區網位址',{exact:true}).inputValue(),old.bind_ip);
  await page.getByLabel('網頁連接埠',{exact:true}).fill('18111');await page.evaluate(()=>window.refreshStores());assert.equal(await page.getByLabel('網頁連接埠',{exact:true}).inputValue(),'18111');await page.screenshot({path:path.join(out,'live-app-update.png'),fullPage:true});await page.getByRole('button',{name:'取消',exact:true}).last().click();assert.equal((await api('apps')).apps.apps['static-site'].port,18081);
  for(const theme of ['light','dark'])for(const width of [1920,1366,768,390]){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);
   for(const id of ['extensions','apps']){await page.evaluate(id=>NAS.navigate(id),id);await page.waitForTimeout(150);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);await page.screenshot({path:path.join(out,`live-${id}-${theme}-${width}.png`),fullPage:true});}
  }
  assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'live-browser.json'),JSON.stringify({result:'PASS',checks:['Live administrator login','Installed application update prefills custom port and IP','Background refresh retains draft','Cancel preserves deployed port','Both themes at 1920/1366/768/390 widths','No uncaught JavaScript errors'],proxy:'Pinned HTTPS VM certificate verified by ui-preview.py'},null,2));console.log('PASS live browser, application settings and 16 responsive screenshots');await api('auth/logout','POST',{});
 }finally{await browser.close();}
}
main().catch(e=>{console.error(e);process.exitCode=1;});
