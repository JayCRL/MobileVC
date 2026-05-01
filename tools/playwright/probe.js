const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1200 } });
  await page.goto('http://127.0.0.1:19081', { waitUntil: 'networkidle' });
  await page.waitForTimeout(4000);

  const inputs = await page.locator('input, textarea').evaluateAll((els) =>
    els.map((el, index) => ({
      index,
      tag: el.tagName,
      type: el.getAttribute('type') || '',
      placeholder: el.getAttribute('placeholder') || '',
      aria: el.getAttribute('aria-label') || '',
      value: 'value' in el ? el.value : '',
    })),
  );

  const buttons = await page.locator('button').evaluateAll((els) =>
    els.map((el, index) => ({
      index,
      text: (el.textContent || '').trim(),
      aria: el.getAttribute('aria-label') || '',
      disabled: el.hasAttribute('disabled'),
    })),
  );

  const text = (await page.locator('body').innerText()).slice(0, 5000);
  console.log(JSON.stringify({ inputs, buttons, text }, null, 2));
  await browser.close();
})();
