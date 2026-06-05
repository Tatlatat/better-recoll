import { chromium } from 'playwright';
const b = await chromium.launch();
const sizes = [[1440,900],[1280,800],[768,600],[480,800],[360,640]];
for (const [W,H] of sizes) {
  const p = await b.newPage({ viewport:{width:W,height:H} });
  const errs=[]; p.on('pageerror',e=>errs.push(e.message));
  await p.goto('http://localhost:8765/',{waitUntil:'load'}); await p.waitForTimeout(1800);
  await p.$eval('input',el=>{el.value='rmit';el.dispatchEvent(new Event('input',{bubbles:true}));}).catch(()=>{});
  await p.waitForTimeout(2000);
  const r = await p.evaluate((W)=>{
    const all=[...document.querySelectorAll('*')]; let overflow=0,tiny=0,offL=0;
    for(const el of all){const rc=el.getBoundingClientRect();
      if(rc.width>0&&rc.right>W+2)overflow++;
      if(rc.width>0&&rc.left<-2)offL++;
      const fs=parseFloat(getComputedStyle(el).fontSize);
      if(fs>0&&fs<10&&el.textContent.trim()&&!el.children.length)tiny++;}
    // input có chiếm đủ rộng không
    const inp=document.querySelector('input'); const ir=inp?inp.getBoundingClientRect():{width:0};
    return {overflow,offLeft:offL,tinyText:tiny,inputW:Math.round(ir.width)};
  }, W);
  await p.screenshot({path:`/tmp/r-${W}.png`});
  console.log(`${W}x${H}: overflow=${r.overflow} offLeft=${r.offLeft} tinyText=${r.tinyText} inputW=${r.inputW} ${errs.length?'JS-ERR:'+errs[0]:''}`);
  await p.close();
}
await b.close();
