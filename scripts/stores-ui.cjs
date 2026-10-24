const {chromium}=require('playwright');
const http=require('http'),fs=require('fs'),path=require('path'),assert=require('assert/strict');
const root=path.resolve(__dirname,'..'),out=path.join(root,'output/stores-review');fs.mkdirSync(out,{recursive:true});
const manifest={id:'static-site',name:'靜態網站示範',version:'1.0.0',kind:'app',installed_bytes:5000000,license:'MIT',permissions:['selected-content-dataset','lan-port'],dependencies:{'container-engine':'1.0.0'}};
const state={mounted:true,ips:['192.168.56.101'],state:{dataset:'tank/.just-nas-extensions',installed:{engine:{package:{manifest:{id:'container-engine',name:'Docker 容器引擎',version:'1.0.0',kind:'runtime'}},enabled:true,status:'active'}}},apps:{apps:{}},imports:[]};
const fixtures={
 'auth/status':{initialized:true,authenticated:true,persistent:true,csrf:'test-fixture'},
 'system/status':{cpu_usage_pct:1,load_avg:[0,0,0],ram_usage_pct:20,ram_used:200,ram_total:1000,uptime_hours:1,os_info:'Alpine',arch:'x86_64',hostname:'just-nas'},
 'storage/pools':[{name:'tank',health:'ONLINE',size:10000000000,allocated:100,free:9999999900}],
 'storage/datasets':[{name:'tank/files',mountpoint:'/mnt/tank/files',used:0,available:1000000000}],
 'network':{settings:{},interfaces:[]},'backup':{},'extensions':state,'apps':{...state,imports:[{token:'a'.repeat(48),package:{manifest,official:true}}]}
};
async function main(){
 const server=http.createServer((q,r)=>{const file=path.join(root,'web',q.url==='/'?'index.html':q.url.split('?')[0]);try{r.setHeader('Content-Type',file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'text/html');r.end(fs.readFileSync(file));}catch{r.writeHead(404);r.end();}});await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({executablePath:process.env.NAS_CHROMIUM_PATH||'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});const checks=[];
 try{
  const page=await browser.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/api/**',route=>{const endpoint=new URL(route.request().url()).pathname.slice(5);if(endpoint.endsWith('/catalog'))return route.fulfill({status:503,json:{error:'離線測試：官方目錄尚未上架，可匯入離線包'}});if(route.request().method()!=='GET')return route.fulfill({json:{task_id:'test-task'}});return route.fulfill({json:fixtures[endpoint]||[]});});
  await page.goto('http://127.0.0.1:'+server.address().port);await page.locator('#nav-apps').waitFor({state:'visible'});
  for(const theme of ['light','dark'])for(const width of [1920,1366,768,390]){
   await page.setViewportSize({width,height:900});await page.evaluate(t=>{document.documentElement.dataset.theme=t;},theme);
   for(const id of ['extensions','apps']){
    await page.evaluate(id=>NAS.navigate(id),id);await page.locator('#sec-'+id+' .store-card').first().waitFor();
    const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);assert.equal(overflow,false,theme+' '+width+' '+id+' overflows');
    await page.screenshot({path:path.join(out,`${id}-${theme}-${width}.png`),fullPage:true});checks.push(`${theme} ${width}px ${id}: no page overflow`);
   }
  }
  await page.setViewportSize({width:1366,height:900});await page.evaluate(()=>NAS.navigate('apps'));await page.getByRole('button',{name:'檢視與安裝'}).click();await page.getByRole('heading',{name:'安裝／更新 靜態網站示範'}).waitFor();
  await page.getByLabel('網頁連接埠').fill('18081');await page.evaluate(()=>window.refreshStores());assert.equal(await page.getByLabel('網頁連接埠').inputValue(),'18081');checks.push('background refresh retains open draft');
  await page.getByRole('button',{name:'取消',exact:true}).last().click();assert.equal(await page.locator('.modal-backdrop.active:not(#auth-modal)').count(),0);checks.push('cancel closes without installation');
  await page.getByRole('button',{name:'線上目錄'}).last().click();await page.getByText('離線測試：官方目錄尚未上架，可匯入離線包').waitFor();checks.push('catalog failure displayed without fake success');
  assert.deepEqual(errors,[]);checks.push('no uncaught JavaScript errors');fs.writeFileSync(path.join(out,'browser-fixtures.json'),JSON.stringify({type:'mock API browser regression, not VM verification',checks},null,2));console.log('PASS',checks.length,'browser checks');
 }finally{await browser.close();server.close();}
}
main().catch(e=>{console.error(e);process.exitCode=1;});
