// Prints an HTML page from docs/pdf to PDF with headless Edge or Chrome.
// Usage: node scripts/build-pdf.mjs docs/pdf/broker-rest-api.html [out.pdf]
import { execFileSync } from 'node:child_process';
import { existsSync, statSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const browsers = [
  process.env.PDF_BROWSER,
  'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  '/usr/bin/chromium',
  '/usr/bin/google-chrome',
].filter(Boolean);

const [input, output] = process.argv.slice(2);
if (!input) {
  console.error('usage: node scripts/build-pdf.mjs <page.html> [out.pdf]');
  process.exit(2);
}
const source = path.resolve(input);
if (!existsSync(source)) {
  console.error(`not found: ${source}`);
  process.exit(2);
}
const target = path.resolve(output ?? source.replace(/\.html?$/i, '.pdf'));
const browser = browsers.find((candidate) => existsSync(candidate));
if (!browser) {
  console.error('no Edge or Chrome found; set PDF_BROWSER to the browser executable');
  process.exit(1);
}

// Page size, margins and running headers come from the page's own @page rules.
execFileSync(browser, [
  '--headless=new',
  '--disable-gpu',
  '--no-pdf-header-footer',
  '--run-all-compositor-stages-before-draw',
  '--virtual-time-budget=10000',
  `--print-to-pdf=${target}`,
  pathToFileURL(source).href,
], { stdio: 'inherit' });

if (!existsSync(target) || statSync(target).size === 0) {
  console.error(`browser produced no PDF: ${target}`);
  process.exit(1);
}
console.log(`${target} (${Math.round(statSync(target).size / 1024)} KB)`);
