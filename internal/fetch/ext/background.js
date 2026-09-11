function headers() {
  return {
    Authorization: 'Bearer ' + EXT_TOKEN,
    'Content-Type': 'application/json'
  };
}

function sleep(ms) {
  return new Promise(function (resolve) {
    setTimeout(resolve, ms);
  });
}

function tabURL(t) {
  return (t && (t.pendingUrl || t.url)) || '';
}

function isShopURL(u) {
  u = (u || '').toLowerCase();
  return u.indexOf('dns-shop') !== -1
    || u.indexOf('ozon.ru') !== -1
    || u.indexOf('market.yandex') !== -1;
}

function isEmptyTab(u) {
  u = (u || '').toLowerCase();
  return !u || u === 'about:blank' || u.indexOf('chrome://newtab') === 0 || u.indexOf('chrome://new-tab-page') === 0;
}

function pickTab(tabs) {
  var shop = null;
  var empty = null;
  var active = null;
  for (var i = 0; i < tabs.length; i++) {
    var u = tabURL(tabs[i]);
    if (!shop && isShopURL(u)) {
      shop = tabs[i];
    }
    if (!empty && isEmptyTab(u)) {
      empty = tabs[i];
    }
    if (!active && tabs[i].active) {
      active = tabs[i];
    }
  }
  return shop || empty || active || tabs[0] || null;
}

async function closeTabsExcept(keepId) {
  var tabs = await chrome.tabs.query({});
  var ids = [];
  for (var i = 0; i < tabs.length; i++) {
    if (tabs[i].id && tabs[i].id !== keepId) {
      ids.push(tabs[i].id);
    }
  }
  if (ids.length) {
    try {
      await chrome.tabs.remove(ids);
    } catch (e) {}
  }
}

async function openJob(url) {
  var tabs = await chrome.tabs.query({});
  var keep = pickTab(tabs);
  if (keep) {
    try {
      await chrome.tabs.update(keep.id, { url: url, active: true });
    } catch (e) {
      keep = await chrome.tabs.create({ url: url });
    }
    await closeTabsExcept(keep.id);
    return;
  }
  await chrome.tabs.create({ url: url });
}

async function closeShopTabs() {
  var tabs = await chrome.tabs.query({});
  var ids = [];
  for (var i = 0; i < tabs.length; i++) {
    if (tabs[i].id) {
      ids.push(tabs[i].id);
    }
  }
  if (ids.length) {
    try {
      await chrome.tabs.remove(ids);
    } catch (e) {}
  }
}

async function applyCity(job) {
  if (job.site !== 'dns' || !job.city) {
    return;
  }
  await chrome.cookies.set({
    url: 'https://www.dns-shop.ru/',
    name: 'city_path',
    value: job.city,
    domain: '.dns-shop.ru',
    path: '/',
    secure: true
  });
}

async function ping() {
  try {
    await fetch(EXT_ORIGIN + '/ext/ping', { headers: headers() });
  } catch (e) {}
}

async function loop() {
  await ping();
  while (true) {
    try {
      var r = await fetch(EXT_ORIGIN + '/ext/wait-job', { headers: headers() });
      if (r.status === 204) {
        continue;
      }
      if (!r.ok) {
        await sleep(1000);
        continue;
      }
      var job = await r.json();
      if (!job) {
        continue;
      }
      if (job.action === 'close') {
        await closeShopTabs();
        continue;
      }
      if (!job.url) {
        continue;
      }
      await applyCity(job);
      await openJob(job.url);
    } catch (e) {
      await sleep(1000);
    }
  }
}

chrome.runtime.onMessage.addListener(function (msg) {
  if (!msg || msg.type !== 'bits') {
    return;
  }
  fetch(EXT_ORIGIN + '/ext/result', {
    method: 'POST',
    headers: headers(),
    body: JSON.stringify(msg)
  }).catch(function () {});
});

var loopStarted = false;
function startLoop() {
  if (loopStarted) {
    return;
  }
  loopStarted = true;
  loop();
}

chrome.runtime.onInstalled.addListener(startLoop);
chrome.runtime.onStartup.addListener(startLoop);
startLoop();
