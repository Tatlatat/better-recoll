// Đôi mắt định lượng của tôi (người mù làm UI).
// Chụp ảnh + tự kiểm tra theo nguyên tắc Refactoring UI, in ra LỖI cụ thể.
// Dùng: node audit.mjs <url> <outPng> [theme-action]
import { chromium } from 'playwright';

const url = process.argv[2] || 'http://localhost:8765/';
const out = process.argv[3] || '/tmp/ui.png';
const W = 900, H = 700;

const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: W, height: H } });
const jsErrors = [];
p.on('pageerror', e => jsErrors.push(e.message));
p.on('console', m => { if (m.type() === 'error') jsErrors.push('console: ' + m.text()); });

await p.goto(url, { waitUntil: 'load' });
await p.waitForTimeout(2500); // chờ Tailwind/DaisyUI render

const report = await p.evaluate(({ W, H }) => {
  const problems = [];
  const all = [...document.querySelectorAll('*')];

  // 1. Element TRÀN ngoài viewport (overflow ngang)
  let overflowX = 0;
  for (const el of all) {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && (r.right > W + 2 || r.left < -2)) overflowX++;
  }
  if (overflowX > 0) problems.push(`OVERFLOW: ${overflowX} element tràn ngang ngoài màn hình`);

  // 2. Element KHỔNG LỒ bất thường (như icon đồng hồ to đùng) — chiếm >70% màn hình mà không phải container gốc
  const huge = [];
  for (const el of all) {
    const r = el.getBoundingClientRect();
    const tag = el.tagName.toLowerCase();
    if (['html','body','div','main'].includes(tag)) continue;
    if (r.width > W * 0.7 && r.height > H * 0.5) {
      huge.push(`${tag}.${(el.className||'').toString().slice(0,30)} (${Math.round(r.width)}x${Math.round(r.height)})`);
    }
  }
  if (huge.length) problems.push(`KHỔNG LỒ: ${huge.join(', ')}`);

  // 3. SVG icon quá to (icon đáng ra nhỏ)
  const bigSvg = [];
  for (const svg of document.querySelectorAll('svg')) {
    const r = svg.getBoundingClientRect();
    if (r.width > 120 || r.height > 120) bigSvg.push(`svg ${Math.round(r.width)}x${Math.round(r.height)}`);
  }
  if (bigSvg.length) problems.push(`ICON TO: ${bigSvg.join(', ')}`);

  // 4. Contrast thấp: chữ màu gần giống nền (đọc text + màu)
  const lowContrast = [];
  for (const el of all) {
    if (!el.textContent.trim() || el.children.length) continue;
    const cs = getComputedStyle(el);
    const fg = cs.color, bg = cs.backgroundColor;
    // chỉ check element có text trực tiếp
    if (fg === bg) lowContrast.push(el.tagName);
  }
  if (lowContrast.length) problems.push(`CONTRAST: ${lowContrast.length} element chữ trùng màu nền`);

  // 5. Modal căn giữa? tìm element có 'modal' class
  const modal = document.querySelector('.modal, [class*="modal"], [class*="onboard"]');
  let modalInfo = 'không có modal hiện';
  if (modal) {
    const r = modal.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) {
      const cx = r.left + r.width/2, cy = r.top + r.height/2;
      const centered = Math.abs(cx - W/2) < 80;
      modalInfo = `modal tại x-center=${Math.round(cx)} (màn hình giữa=${W/2}) — ${centered ? 'CĂN GIỮA ok' : 'LỆCH!'}`;
      if (!centered) problems.push(`MODAL LỆCH: ${modalInfo}`);
    }
  }

  // tóm tắt nội dung thấy được
  const visibleText = document.body.innerText.replace(/\s+/g,' ').slice(0, 250);

  return {
    problems: problems.length ? problems : ['(không phát hiện lỗi layout tự động)'],
    totalElements: all.length,
    modalInfo,
    visibleText
  };
}, { W, H });

const buf = await p.screenshot();
const fs = await import('fs');
fs.writeFileSync(out, buf);

console.log('=== UI AUDIT ===');
console.log('screenshot:', out, '(' + buf.length + ' bytes)');
console.log('JS errors:', jsErrors.length ? jsErrors.slice(0,5) : 'none');
console.log('elements:', report.totalElements);
console.log('PROBLEMS:');
report.problems.forEach(p => console.log('  - ' + p));
console.log('modal:', report.modalInfo);
console.log('visible text:', report.visibleText);
await b.close();
