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

async function getTab() {
  var tabs = await chrome.tabs.query({ currentWindow: true, active: true });
  if (tabs && tabs.length) {
    return tabs[0];
  }
  tabs = await chrome.tabs.query({ currentWindow: true });
  if (tabs && tabs.length) {
    return tabs[0];
  }
  return await chrome.tabs.create({ url: 'about:blank' });
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
      if (!job || !job.url) {
        continue;
      }
      await applyCity(job);
      var tab = await getTab();
      await chrome.tabs.update(tab.id, { url: job.url });
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
