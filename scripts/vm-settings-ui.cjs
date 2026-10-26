/* Fully local fixtures: no NAS connection or VM mutations. */
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),http=require('http'),assert=require('assert/strict');
const root=path.resolve(__dirname,'..'),web=path.join(root,'internal/vmmanager/web'),out=path.join(root,'output/vm-settings-tests');fs.mkdirSync(out,{recursive:true});
(async()=>{
 const server=http.createServer((req,res)=>{const file={'/':'index.html','/app.js':'app.js','/style.css':'style.css'}[req.url];if(!file){res.writeHead(404);res.end();return}res.setHeader('Content-Type',file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'text/html');res.end(fs.readFileSync(path.join(web,file)))});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const base='http://127.0.0.1:'+server.address().port;
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe'});
 try {
 const page=await browser.newPage(),errors=[],writes=[];let failed=false;
 const row={protocol:'tcp',host_ip:'192.168.56.101',host_port:24001,guest_port:80};
 const common={os:'windows10',cpus:2,memory_mib:4096,disk_gib:64,firmware:'uefi',acceleration:'tcg',actual_acceleration:'tcg',forwards:[row],used_bytes:1024};
 const vms=[{...common,id:'a'.repeat(32),name:'執行中的 Windows',status:'running',process_present:true},{...common,id:'b'.repeat(32),name:'已關機的測試',status:'stopped',process_present:false}];
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/**',async route=>{const req=route.request(),url=new URL(req.url()),p=url.pathname.replace('/api/app-services/vm-manager','');let value;
 if(url.pathname==='/api/auth/status')value={authenticated:true,csrf:'fixture'};
 else if(p==='/capabilities')value={dataset:'vmtest/.just-nas-vms',cpus:4,memory_mib:8192,available_bytes:40*1024**3,ips:['192.168.56.101'],kvm:false};
 else if(p==='/vms'&&req.method()==='GET')value={vms,retained:[]};
 else if(p==='/isos'||p==='/tasks')value=[];
 else if(p==='/tasks/fixture')value=failed?{state:'failed',error:'連接埠衝突；已回復原規則'}:{state:'succeeded',progress:'已驗證並保存'};
 else if(req.method()==='PATCH') {const body=req.postDataJSON();writes.push({path:p,body});if(!failed){const vm=vms.find(v=>p.includes(v.id));Object.assign(vm,body)}value={task_id:'fixture'}}
 else throw Error('Unexpected fixture request '+req.method()+' '+p);
 await route.fulfill({json:value})});
 await page.goto(base);await page.locator('#create').waitFor({state:'visible'});
 const card=name=>page.locator('article').filter({has:page.getByRole('heading',{name,exact:true})});
 const menu=async(name,action)=>{const c=card(name);await c.locator('summary').click();await c.getByRole('button',{name:action,exact:true}).click()};
 await card('執行中的 Windows').locator('summary').click();assert.equal(await card('執行中的 Windows').getByRole('button',{name:'編輯設定',exact:true}).isDisabled(),true);assert.equal(await card('執行中的 Windows').getByRole('button',{name:'NAT 連接埠轉發',exact:true}).isEnabled(),true);await card('執行中的 Windows').locator('summary').click();
 for(const theme of ['light','dark'])for(const width of [1920,1366,768,390]){
  await page.setViewportSize({width,height:900});await page.getByLabel('外觀主題').selectOption(theme);
  await menu('已關機的測試','編輯設定');await page.getByText('進階：TCG 翻譯快取',{exact:true}).click();
  const cache=page.getByLabel('TCG 翻譯快取（MiB）',{exact:true});assert.equal(await cache.inputValue(),'64');await cache.selectOption('512');
  await page.getByLabel('加速方式',{exact:true}).selectOption('kvm');assert.equal(await cache.isDisabled(),true);assert.equal(await cache.inputValue(),'512');
  await page.getByLabel('加速方式',{exact:true}).selectOption('tcg');assert.equal(await cache.isEnabled(),true);assert.ok((await page.locator('#fields').innerText()).includes('448 MiB'));
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);
  await page.screenshot({path:path.join(out,`cache-${theme}-${width}.png`),fullPage:true});await page.locator('#cancel').click();
  await menu('執行中的 Windows','NAT 連接埠轉發');assert.equal(await page.getByLabel('虛擬 CPU',{exact:true}).count(),0);await page.getByLabel('主機連接埠',{exact:true}).fill('24009');
  if(width===390&&theme==='dark'){await page.getByLabel('主機連接埠',{exact:true}).focus();await page.waitForTimeout(5500);assert.equal(await page.getByLabel('主機連接埠',{exact:true}).inputValue(),'24009');assert.equal(await page.evaluate(()=>document.activeElement.getAttribute('aria-label')),'主機連接埠')}
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);await page.screenshot({path:path.join(out,`nat-${theme}-${width}.png`),fullPage:true});await page.keyboard.press('Escape');await page.locator('#dialog').waitFor({state:'hidden'});
 }
 assert.equal(writes.length,0,'cancel performed a write');
 await menu('已關機的測試','編輯設定');await page.getByText('進階：TCG 翻譯快取',{exact:true}).click();await page.getByLabel('TCG 翻譯快取（MiB）',{exact:true}).selectOption('256');await page.locator('#save').click();await page.locator('#dialog').waitFor({state:'hidden'});assert.equal(writes.at(-1).body.tcg_cache_mib,256);
 await menu('已關機的測試','編輯設定');await page.getByText('進階：TCG 翻譯快取',{exact:true}).click();assert.equal(await page.getByLabel('TCG 翻譯快取（MiB）',{exact:true}).inputValue(),'256');await page.locator('#cancel').click();
 await menu('執行中的 Windows','NAT 連接埠轉發');await page.getByLabel('主機連接埠',{exact:true}).fill('24002');failed=true;await page.locator('#save').click();await page.getByText('連接埠衝突；已回復原規則',{exact:true}).waitFor();assert.equal(await page.locator('#dialog').isVisible(),true);assert.equal(await page.getByLabel('主機連接埠',{exact:true}).inputValue(),'24002');failed=false;await page.locator('#save').click();await page.locator('#dialog').waitFor({state:'hidden'});
 assert.ok(writes.at(-1).path.endsWith('/forwards'));assert.deepEqual(Object.keys(writes.at(-1).body),['forwards']);assert.equal(vms[0].status,'running');assert.equal(vms[0].memory_mib,4096);
 assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'ui-results.json'),JSON.stringify({result:'PASS',checks:['Running VM NAT editable; environment locked','Cache default and saved prefill','KVM disables cache without clearing it','Additional memory explained','NAT-only PATCH cannot change VM resources','Failure retains draft; retry succeeds','Background refresh preserves focus and draft','Cancel never writes','16 light/dark responsive screenshots','No JS errors']},null,2));console.log('PASS VM settings fixture checks and 16 screenshots');
 } finally {await browser.close();await new Promise(r=>server.close(r))}
})().catch(e=>{console.error(e);process.exitCode=1});
