/* Real browser upload failure under a quota set by the whitelisted Python harness. */
const {chromium}=require('playwright'),fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'..');
(async()=>{const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe'});try{
 const page=await browser.newPage();let status=null;page.on('response',r=>{if(r.url().includes('/isos?name=full-browser-fixture.iso'))status=r.status()});
 await page.goto('http://127.0.0.1:8765/#apps');await page.locator('#auth-password').fill(JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/test-credentials.json'),'utf8')).admin);await page.locator('#auth-submit').click();await page.locator('#auth-modal').waitFor({state:'hidden'});
 await page.goto('http://127.0.0.1:8765/apps/vm-manager/');await page.getByRole('button',{name:'安裝媒體',exact:true}).click();
 await page.locator('#iso-file').setInputFiles(path.join(root,'output/vm-test/full-browser-fixture.iso'));
 await page.waitForFunction(()=>!document.querySelector('#notice').textContent.startsWith('上傳 ')&&document.querySelector('#upload-progress').hidden,{},{timeout:60000});
 const message=await page.locator('#notice').innerText();
 fs.writeFileSync(path.join(root,'output/vm-test/browser-upload-full.json'),JSON.stringify({http_status:status,message,result:status===409&&message.includes('空間不足')?'PASS':'FAIL'},null,2));
 await page.screenshot({path:path.join(root,'output/vm-manager-review/upload-full.png'),fullPage:true});
 assert.equal(status,409);assert.match(message,/空間不足/);console.log('PASS browser receives explicit full-storage error; no success state');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
