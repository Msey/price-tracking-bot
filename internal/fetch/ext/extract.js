// Дублирует селекторы из dns.go / market.go / ozon.go: страницу читает
// расширение в обычном Chrome, без CDP — иначе Ozon показывает
// «Похоже, нет соединения».
// Скрипт на document_end: цена уходит, как только узел появился в DOM,
// не дожидаясь картинок и idle.

function ldjson() {
  var scripts = document.querySelectorAll('script[type="application/ld+json"]');
  var ld = [];
  for (var i = 0; i < scripts.length; i++) {
    ld.push(scripts[i].textContent || '');
  }
  return ld;
}

// title + URL + видимый текст. Полный innerHTML не берём: на карточке
// это мегабайты и задерживает съём цены.
function haystack() {
  var t = (document.title || '') + '\n' + (location.href || '');
  var root = document.body || document.documentElement;
  if (root) {
    var text = root.innerText || root.textContent || '';
    if (text.length > 8000) {
      text = text.slice(0, 8000);
    }
    t += '\n' + text;
  }
  return t;
}

function hasNode(sel) {
  try {
    return !!document.querySelector(sel);
  } catch (e) {
    return false;
  }
}

function extractDNS() {
  var title = document.title || '';
  var body = haystack();
  var lowTitle = title.toLowerCase();
  var low = body.toLowerCase();
  var qrator = lowTitle.indexOf('403') !== -1 || lowTitle.indexOf('401') !== -1
    || lowTitle.indexOf('forbidden') !== -1
    || body.indexOf('Доступ к сайту') !== -1
    || low.indexOf('доступ запрещен') !== -1;
  var price = document.querySelector('div.product-buy__price');
  return {
    qrator: qrator,
    ldjson: ldjson(),
    cssPrice: price ? (price.textContent || '') : '',
    title: title
  };
}

function extractMarket() {
  var low = haystack().toLowerCase();
  var box = document.querySelector('[data-auto="snippet-price-current"]')
    || document.querySelector('[data-auto="price-value"]');
  var priceEl = box ? (box.querySelector('span') || box) : null;
  var h1 = document.querySelector('h1[data-auto="productCardTitle"]') || document.querySelector('h1');
  var challenge = low.indexOf('smartcaptcha') !== -1
    || low.indexOf('showcaptcha') !== -1
    || low.indexOf('checkboxcaptcha') !== -1
    || low.indexOf('are you not a robot') !== -1
    || low.indexOf('confirm that you are not a robot') !== -1
    || hasNode('[class*="smartcaptcha"], [class*="SmartCaptcha"], [id*="smartcaptcha"]');
  return {
    challenge: challenge,
    ldjson: ldjson(),
    cssPrice: priceEl ? (priceEl.textContent || '') : '',
    name: h1 ? (h1.textContent || '').trim() : '',
    title: document.title || ''
  };
}

function extractOzon() {
  var title = document.title || '';
  var body = haystack();
  var low = body.toLowerCase();
  var box = document.querySelector('[data-widget="webPrice"] .tsHeadline600Large')
    || document.querySelector('.tsHeadline600Large');
  var h1 = document.querySelector('h1');
  var blocked = title.indexOf('нет соединения') !== -1
    || title.toLowerCase().indexOf('antibot challenge') !== -1
    || low.indexOf('fab_chig') !== -1
    || body.indexOf('нет соединения') !== -1;
  var challenge = !blocked && (low.indexOf('px-captcha') !== -1 || hasNode('#px-captcha, [id*="px-captcha"]'));
  if (!blocked && !(box && String(box.textContent || '').replace(/\s/g, ''))) {
    window.scrollTo(0, 480);
  }
  return {
    challenge: challenge,
    blocked: blocked,
    ldjson: ldjson(),
    cssPrice: box ? (box.textContent || '') : '',
    name: h1 ? (h1.textContent || '').trim() : '',
    title: title
  };
}

function extract() {
  var host = (location.hostname || '').toLowerCase();
  if (host.indexOf('dns-shop') !== -1) {
    return extractDNS();
  }
  if (host.indexOf('ozon') !== -1) {
    return extractOzon();
  }
  if (host.indexOf('market.yandex') !== -1) {
    return extractMarket();
  }
  return { title: document.title || '' };
}

var lastKey = null;
var scheduled = false;

function bitsKey(bits) {
  var ld = bits.ldjson && bits.ldjson.length ? bits.ldjson[0].slice(0, 80) : '';
  return (bits.cssPrice || '') + '\0' + ld + '\0' + (bits.title || '') + '\0'
    + !!bits.challenge + !!bits.blocked + !!bits.qrator;
}

function report() {
  if (window.top !== window) {
    return;
  }
  var bits = extract();
  var key = bitsKey(bits);
  if (key === lastKey) {
    return;
  }
  lastKey = key;
  var payload = {
    type: 'bits',
    href: location.href,
    bits: bits
  };
  try {
    chrome.runtime.sendMessage(payload);
  } catch (e) {}
  if (typeof EXT_ORIGIN === 'string' && typeof EXT_TOKEN === 'string') {
    fetch(EXT_ORIGIN + '/ext/result', {
      method: 'POST',
      headers: {
        Authorization: 'Bearer ' + EXT_TOKEN,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(payload)
    }).catch(function () {});
  }
}

function schedule() {
  if (scheduled) {
    return;
  }
  scheduled = true;
  setTimeout(function () {
    scheduled = false;
    report();
  }, 50);
}

function start() {
  report();
  if (document.documentElement && typeof MutationObserver === 'function') {
    var obs = new MutationObserver(schedule);
    obs.observe(document.documentElement, { childList: true, subtree: true, characterData: true });
  }
  setInterval(report, 1000);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', start);
} else {
  start();
}
