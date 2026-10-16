import { chromium } from '/opt/npm-tools/node_modules/playwright/index.mjs';
import path from 'node:path';
const dir = path.dirname(new URL(import.meta.url).pathname);
const [a,b] = process.argv.slice(2).map(Number);
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
await page.goto('file://' + dir + '/film.html');
await page.waitForFunction(() => window.READY === true);
for (let i=a;i<b;i++){ await page.evaluate(t=>seek(t), i/30); await page.screenshot({path:`${dir}/frames/f${String(i).padStart(5,'0')}.jpg`,type:'jpeg',quality:95}); }
await browser.close();
