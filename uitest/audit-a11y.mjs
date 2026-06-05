import { chromium } from 'playwright';
import { AxeBuilder } from '@axe-core/playwright';
const b = await chromium.launch();
const ctx = await b.newContext({ viewport:{width:900,height:760} });
const p = await ctx.newPage();
await p.goto('http://localhost:8765/',{waitUntil:'load'}); await p.waitForTimeout(2000);
await p.$eval('input',el=>{el.value='rmit';el.dispatchEvent(new Event('input',{bubbles:true}));}).catch(()=>{});
await p.waitForTimeout(2000);
const results = await new AxeBuilder({ page: p }).analyze();
console.log('=== A11Y VIOLATIONS:', results.violations.length, '===');
for (const v of results.violations) {
  console.log(`  [${v.impact}] ${v.id}: ${v.help.slice(0,65)}`);
  console.log(`     ${v.nodes.length} nodes, vd: ${v.nodes[0].html.slice(0,75)}`);
}
if(!results.violations.length) console.log('  (sạch a11y)');
await b.close();
