// Дублирует селекторы из dns.go / market.go / ozon.go: страницу читает
// расширение в обычном Chrome, без CDP — иначе Ozon показывает
// «Похоже, нет соединения».

function ldjson() {
  var scripts = document.querySelectorAll('script[type="application/ld+json"]');
  var ld = [];
  for (var i = 0; i < scripts.length; i++) {
    ld.push(scripts[i].textContent || '');
  }
  return ld;
}

function extractDNS() {
  var title = document.title || '';
  var body = (document.body && document.body.innerText) ? document.body.innerText : '';
  var lowTitle = title.toLowerCase();
  var qrator = lowTitle.indexOf('403') !== -1 || lowTitle.indexOf('401') !== -1
    || lowTitle.indexOf('forbidden') !== -1
    || body.indexOf('Доступ к сайту') !== -1
    || body.toLowerCase().indexOf('доступ запрещен') !== -1;
  var price = document.querySelector('div.product-buy__price');
  return {
    qrator: qrator,
    ldjson: ldjson(),
    cssPrice: price ? (price.textContent || '') : '',
    title: title
  };
}

function extractMarket() {
  var html = document.documentElement ? document.documentElement.innerHTML : '';
  var box = document.querySelector('[data-auto="snippet-price-current"]')
    || document.querySelector('[data-auto="price-value"]');
  var priceEl = box ? (box.querySelector('span') || box) : null;
  var h1 = document.querySelector('h1[data-auto="productCardTitle"]') || document.querySelector('h1');
  var low = html.toLowerCase();
  var challenge = low.indexOf('smartcaptcha') !== -1
    || low.indexOf('showcaptcha') !== -1
    || low.indexOf('checkboxcaptcha') !== -1
    || low.indexOf('are you not a robot') !== -1
    || low.indexOf('confirm that you are not a robot') !== -1;
  return {
    challenge: challenge,
    ldjson: ldjson(),
    cssPrice: priceEl ? (priceEl.textContent || '') : '',
    name: h1 ? (h1.textContent || '').trim() : '',
    title: document.title || ''
  };
}

function extractOzon() {
  var html = document.documentElement ? document.documentElement.innerHTML : '';
  var box = document.querySelector('[data-widget="webPrice"] .tsHeadline600Large')
    || document.querySelector('.tsHeadline600Large');
  var h1 = document.querySelector('h1');
  var title = document.title || '';
  var body = (document.body && document.body.innerText) ? document.body.innerText : '';
  var low = (html + ' ' + title + ' ' + body).toLowerCase();
  var blocked = title.indexOf('нет соединения') !== -1
    || title.toLowerCase().indexOf('antibot challenge') !== -1
    || html.indexOf('fab_chig') !== -1
    || body.indexOf('нет соединения') !== -1;
  var challenge = !blocked && low.indexOf('px-captcha') !== -1;
  if (!blocked) {
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

function report() {
  if (window.top !== window) {
    return;
  }
  var payload = {
    type: 'bits',
    href: location.href,
    bits: extract()
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

setInterval(report, 1000);
report();
