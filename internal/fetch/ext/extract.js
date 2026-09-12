// Дублирует селекторы из dns.go / market.go / ozon.go: страницу читает
// расширение в обычном Chrome, без CDP — иначе Ozon показывает
// «Похоже, нет соединения».
// Скрипт на document_end: цена уходит, как только нужный узел появился
// в DOM, не дожидаясь картинок и idle. На Ozon ждём ценник
// «с Ozon Картой» / «с банками Ozon банка», а не первый крупный число.

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

function titleLooksLikeHTTPBan(title) {
  var t = String(title || '').toLowerCase().replace(/^\s+|\s+$/g, '');
  if (!t) return false;
  if (t === '403' || t === '401' || t === 'forbidden') return true;
  if (t.indexOf('403 forbidden') !== -1 || t.indexOf('401 unauthorized') !== -1) return true;
  if (t.indexOf('error 403') !== -1 || t.indexOf('error 401') !== -1) return true;
  if (t.indexOf('http 403') !== -1 || t.indexOf('http 401') !== -1) return true;
  if (t.indexOf('403 error') !== -1 || t.indexOf('401 error') !== -1) return true;
  if (t.indexOf('access forbidden') !== -1) return true;
  return httpCodeTitlePrefix(t);
}

function httpCodeTitlePrefix(t) {
  var codes = ['403', '401'];
  for (var i = 0; i < codes.length; i++) {
    var code = codes[i];
    if (t.indexOf(code) !== 0) continue;
    var rest = t.slice(code.length).replace(/^\s+/, '');
    if (!rest) return true;
    var ch = rest.charAt(0);
    if (ch === '-' || ch === '–' || ch === '|' || ch === ':' || ch === '/') return true;
  }
  return false;
}

function extractDNS() {
  var title = document.title || '';
  var body = haystack();
  var low = body.toLowerCase();
  var qrator = titleLooksLikeHTTPBan(title)
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

function ozonBankLabel(s) {
  s = String(s || '').toLowerCase().replace(/\s+/g, ' ').replace(/^\s+|\s+$/g, '');
  if (s.indexOf('друг') !== -1) {
    return false;
  }
  var pay = s.indexOf('ozon карт') !== -1
    || s.indexOf('ozon банк') !== -1
    || s.indexOf('банком ozon') !== -1
    || s.indexOf('банками ozon') !== -1;
  if (!pay) {
    return false;
  }
  // «Ozon Банк» в меню не берём — нужна подпись у ценника.
  return s.indexOf('с ') !== -1 || s.indexOf('картой') !== -1;
}

function nodeText(el) {
  return el ? String(el.textContent || '').replace(/\s+/g, ' ').trim() : '';
}

function isolatePrice(s) {
  s = String(s || '')
    .replace(/[\u00a0\u202f\u2007\u2009\u200a\u2060\ufeff]/g, ' ')
    .replace(/&nbsp;/g, ' ')
    .replace(/(\d)[.,](\d{2})\b/g, '$1')
    .replace(/\s+/g, ' ')
    .trim();
  var m = s.match(/(\d[\d ]{0,14})\s*(₽|руб)/i);
  if (m) {
    return m[1].replace(/\s+/g, ' ').trim() + ' ₽';
  }
  return '';
}

function isolateHeadlinePrice(s) {
  var p = isolatePrice(s);
  if (p) {
    return p;
  }
  s = String(s || '')
    .replace(/[\u00a0\u202f\u2007\u2009\u200a\u2060\ufeff]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  if (/^\d[\d ]{0,14}$/.test(s)) {
    return s + ' ₽';
  }
  return '';
}

function nearestOzonHeadline(el) {
  var n = el;
  for (var up = 0; up < 8 && n; up++) {
    var sib = n.previousElementSibling;
    while (sib) {
      var fromSib = isolatePrice(nodeText(sib));
      if (fromSib) {
        return fromSib;
      }
      if (sib.classList && /tsHeadline/i.test(sib.className || '')) {
        fromSib = isolateHeadlinePrice(nodeText(sib));
        if (fromSib) {
          return fromSib;
        }
      }
      var h = sib.querySelector ? sib.querySelector('[class*="tsHeadline"]') : null;
      var fromH = isolateHeadlinePrice(nodeText(h));
      if (fromH) {
        return fromH;
      }
      sib = sib.previousElementSibling;
    }
    var fromSelf = isolateHeadlinePrice(nodeText(n));
    if (n.classList && /tsHeadline/i.test(n.className || '') && fromSelf) {
      return fromSelf;
    }
    if (n.getAttribute && n.getAttribute('data-widget')) {
      return '';
    }
    n = n.parentElement;
  }
  return '';
}

function ozonSearchRoots() {
  var roots = [];
  var w = document.querySelector('[data-widget="webPrice"]');
  if (w) roots.push(w);
  var all = document.querySelectorAll('[data-widget]');
  for (var i = 0; i < all.length && roots.length < 8; i++) {
    var name = String(all[i].getAttribute('data-widget') || '').toLowerCase();
    if (name.indexOf('price') !== -1 && all[i] !== w) {
      roots.push(all[i]);
    }
  }
  return roots;
}

function ozonWidgetPrice() {
  var roots = ozonSearchRoots();
  for (var r = 0; r < roots.length; r++) {
    var heads = roots[r].querySelectorAll('[class*="tsHeadline"]');
    for (var i = 0; i < heads.length; i++) {
      var p = isolateHeadlinePrice(nodeText(heads[i]));
      if (p) {
        return p;
      }
    }
    var fromRoot = isolatePrice(nodeText(roots[r]));
    if (fromRoot) {
      return fromRoot;
    }
  }
  return '';
}

// Крупный ценник рядом с подписью «с Ozon Картой» / «с банками Ozon банка».
// Первый tsHeadline на странице часто другая цена (без карты, JSON-LD).
function ozonBankPrice() {
  var roots = ozonSearchRoots();
  if (document.body) {
    roots.push(document.body);
  }
  for (var r = 0; r < roots.length; r++) {
    var nodes = roots[r].querySelectorAll('span, div, p, a, label');
    for (var i = 0; i < nodes.length; i++) {
      var t = nodeText(nodes[i]);
      if (t.length < 4 || t.length > 80 || !ozonBankLabel(t)) {
        continue;
      }
      if (nodes[i].querySelector && nodes[i].querySelector('[class*="tsHeadline"]')) {
        continue;
      }
      var price = isolatePrice(nearestOzonHeadline(nodes[i]));
      if (price) {
        return price;
      }
    }
  }
  return '';
}

var ozonWidgetSince = 0;

function extractOzon() {
  var title = document.title || '';
  var body = haystack();
  var low = body.toLowerCase();
  var bank = ozonBankPrice();
  var widget = ozonWidgetPrice();
  if (widget && !ozonWidgetSince) {
    ozonWidgetSince = Date.now();
  }
  // Банк — сразу. Иначе через 6 с берём первое число с ₽ в блоке цены.
  var css = bank;
  if (!css && widget && Date.now() - ozonWidgetSince >= 6000) {
    css = widget;
  }
  var h1 = document.querySelector('h1');
  var blocked = title.indexOf('нет соединения') !== -1
    || title.toLowerCase().indexOf('antibot challenge') !== -1
    || low.indexOf('fab_chig') !== -1
    || body.indexOf('нет соединения') !== -1;
  var challenge = !blocked && (low.indexOf('px-captcha') !== -1 || hasNode('#px-captcha, [id*="px-captcha"]'));
  if (!blocked && !css) {
    window.scrollTo(0, 480);
  }
  return {
    challenge: challenge,
    blocked: blocked,
    ldjson: ldjson(),
    cssPrice: css,
    skipLdjson: true,
    bankGraceMs: 8000,
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
