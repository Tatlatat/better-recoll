import { chromium } from 'playwright';
const b = await chromium.launch();
const ctx = await b.newContext({ viewport:{width:900,height:760} });
const p = await ctx.newPage();
const errs=[]; p.on('pageerror',e=>errs.push(e.message));
await p.goto('http://localhost:8765/',{waitUntil:'load'}); await p.waitForTimeout(2000);
async function tryQuery(q){
  await p.$eval('input',(el,v)=>{el.value=v;el.dispatchEvent(new Event('input',{bubbles:true}));},q);
  await p.waitForTimeout(1500);
  return p.evaluate(()=>({cards:document.querySelectorAll('#exact-list > *, #suggest-list > *').length, overflow:[...document.querySelectorAll('*')].filter(e=>e.getBoundingClientRect().right>905).length}));
}
// edge cases
const tests = {
  'XSS': '<script>alert(1)</script><img src=x onerror=alert(2)>',
  'siêu dài': 'a'.repeat(500),
  'unicode': '日本語 émoji 🎉 ñ',
  'ký tự đặc biệt': '"\'&<>{}[]',
  'khoảng trắng': '     ',
};
for(const [name,q] of Object.entries(tests)){
  const r = await tryQuery(q);
  console.log(`${name}: cards=${r.cards} overflow=${r.overflow}`);
}
// click vào kết quả rmit → mở detail
await tryQuery('rmit');
const card = await p.$('#exact-list > *');
if(card){ await card.click(); await p.waitForTimeout(800);
  const detail = await p.evaluate(()=>{const d=[...document.querySelectorAll('dialog')].find(x=>x.open); return d?d.innerText.slice(0,50):'no detail opened';});
  console.log('click card → detail:', detail.replace(/\s+/g,' '));
}
console.log('JS errors:', errs.length?errs.slice(0,3):'none');
await b.close();
