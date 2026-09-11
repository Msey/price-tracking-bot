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

async function openJob(url) {
  var tabs = await chrome.tabs.query({ currentWindow: true, active: true });
  if (tabs && tabs.length) {
    await chrome.tabs.update(tabs[0].id, { url: url });
    return;
  }
  tabs = await chrome.tabs.query({});
  if (tabs && tabs.length) {
    await chrome.tabs.update(tabs[0].id, { url: url });
    return;
  }
  await chrome.tabs.create({ url: url });
}

function isShopURL(u) {
  u = (u || '').toLowerCase();
  return u.indexOf('dns-shop') !== -1 || u.indexOf('ozon.ru') !== -1 || u.indexOf('market.yandex') !== -1;
}

function isEmptyTab(u) {
  u = (u || '').toLowerCase();
  return !u || u === 'about:blank' || u.indexOf('chrome://newtab') === 0 || u.indexOf('chrome://new-tab-page') === 0;
}

async function closeShopWindows() {
  var wins = await chrome.windows.getAll({ populate: true });
  for (var i = 0; i < wins.length; i++) {
    var tabs = wins[i].tabs || [];
    var shop = false;
    var onlyEmpty = true;
    for (var j = 0; j < tabs.length; j++) {
      var u = tabs[j].url || '';
      if (isShopURL(u)) {
        shop = true;
      }
      if (!isEmptyTab(u) && !isShopURL(u) && u.indexOf('chrome://extensions') === -1) {
        onlyEmpty = false;
      }
    }
    if (shop || onlyEmpty) {
      try {
        await chrome.windows.remove(wins[i].id);
      } catch (e) {}
    }
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
        await closeShopWindows();
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
