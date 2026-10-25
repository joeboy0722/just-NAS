/* Drive only the authenticated guest console. Input specs and credentials stay ignored/local. */
const {chromium}=require('playwright'),fs=require('fs'),path=require('path');
const root=path.resolve(__dirname,'..');
async function typeGuest(page,text){
 const shifted={'@':'Digit2','|':'Backslash','>':'Period','&':'Digit7','_':'Minus','~':'Backquote',':':'Semicolon','?':'Slash','"':'Quote','!':'Digit1','#':'Digit3','$':'Digit4','%':'Digit5','^':'Digit6','*':'Digit8','(':'Digit9',')':'Digit0','+':'Equal','{':'BracketLeft','}':'BracketRight','<':'Comma'};
 for(const c of text){if(shifted[c])await page.keyboard.press('Shift+'+shifted[c]);else if(/[A-Z]/.test(c))await page.keyboard.press('Shift+Key'+c);else await page.keyboard.type(c);await page.waitForTimeout(60)}
}
(async()=>{
 const spec=JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/console-input.json'),'utf8'));
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe'});
 try{
  const page=await browser.newPage({viewport:{width:1366,height:1000}});
  await page.goto('http://127.0.0.1:8765/#apps');
  await page.locator('#auth-password').fill(JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/test-credentials.json'),'utf8')).admin);
  await page.locator('#auth-submit').click();await page.locator('#auth-modal').waitFor({state:'hidden'});
  await page.goto('http://127.0.0.1:8765/apps/vm-manager/');
  const card=page.locator('article').filter({has:page.getByRole('heading',{name:spec.vm,exact:true})});
  await card.getByRole('button',{name:'開啟主控台',exact:true}).click();
  await page.getByText('已連線 · TCG',{exact:true}).waitFor({timeout:30000});
  const canvas=page.locator('#screen canvas');await canvas.waitFor();await canvas.focus();
  for(const a of spec.actions){
   if(a.key)await page.keyboard.press(a.key);
   else if(a.text!==undefined)await typeGuest(page,a.text);
   else if(a.credential){const c=JSON.parse(fs.readFileSync(path.join(root,'output/vm-test/test-guest-credentials.json'),'utf8'));if(!['user','password'].includes(a.credential))throw Error('Invalid private field');await typeGuest(page,c[a.credential])}
   else if(a.click){const b=await canvas.boundingBox(),r=await canvas.evaluate(e=>({w:e.width,h:e.height}));await page.mouse.click(b.x+a.click[0]/r.w*b.width,b.y+a.click[1]/r.h*b.height)}
   else if(a.wait_ms)await page.waitForTimeout(Math.min(a.wait_ms,10000));
   else throw Error('Unknown console input');
   await page.waitForTimeout(200);
  }
  await page.waitForTimeout(3000);await page.screenshot({path:path.join(root,'output/vm-manager-review/console-input-'+spec.vm.replaceAll(' ','-')+'.png'),fullPage:true});
  console.log('PASS authenticated guest console input; private values omitted');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
