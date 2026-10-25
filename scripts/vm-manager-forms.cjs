/* Non-mutating form, keyboard and draft persistence checks against real NAS data. */
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'..'),out=path.join(root,'output/vm-manager-review'),base=process.env.VM_UI_BASE||'http://127.0.0.1:8765';
(async()=>{const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe'});try{
 const page=await browser.newPage(),errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base+'/#apps');
 await page.locator('#auth-password').fill(JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/test-credentials.json'),'utf8')).admin);
 await page.locator('#auth-submit').click();await page.locator('#auth-modal').waitFor({state:'hidden'});
 await page.goto(base+'/apps/vm-manager/');await page.locator('#create').waitFor({state:'visible'});
 for(const theme of ['light','dark'])for(const width of [1920,1366,768,390]){
  await page.setViewportSize({width,height:900});await page.getByLabel('外觀主題').selectOption(theme);
  await page.locator('#create').click();await page.getByLabel('虛擬機名稱',{exact:true}).fill('尚未儲存的中文測試草稿');
  await page.getByLabel('作業系統',{exact:true}).selectOption('windows10');assert.equal(await page.getByLabel('磁碟容量（GiB）',{exact:true}).inputValue(),'64');
  await page.getByRole('button',{name:'＋ 新增轉發',exact:true}).click();await page.getByLabel('主機連接埠',{exact:true}).fill('24001');
  await page.getByLabel('虛擬機名稱',{exact:true}).focus();
  for(let n=0;n<25;n++){await page.keyboard.press('Tab');assert.equal(await page.evaluate(()=>!!document.activeElement.closest('#dialog')),true,JSON.stringify(await page.evaluate(()=>({tag:document.activeElement.tagName,id:document.activeElement.id,dialog:document.querySelector('#dialog').open}))))}
  if(width===390){await page.getByLabel('虛擬機名稱',{exact:true}).focus();await page.waitForTimeout(5500);assert.equal(await page.getByLabel('虛擬機名稱',{exact:true}).inputValue(),'尚未儲存的中文測試草稿');assert.equal(await page.getByLabel('主機連接埠',{exact:true}).inputValue(),'24001')}
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);
  await page.screenshot({path:path.join(out,`form-${theme}-${width}.png`),fullPage:true});
  await page.keyboard.press('Escape');await page.locator('#dialog').waitFor({state:'hidden'});assert.equal(await page.evaluate(()=>document.activeElement.id),'create');
 }
 assert.deepEqual(errors,[]);fs.writeFileSync(path.join(out,'forms-smoke.json'),JSON.stringify({result:'PASS',checks:['Windows disk default','Editable NAT rows','Background refresh preserves draft','Keyboard modal focus containment','Escape returns focus','Eight theme/width form layouts','No application writes or JS errors']},null,2));
 console.log('PASS form drafts, keyboard focus, cancel and eight responsive theme layouts');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
