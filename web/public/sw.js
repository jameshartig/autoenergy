self.addEventListener('push', function (event) {
  let payload = {};
  if (event.data) {
    try {
      payload = event.data.json();
    } catch (e) {
      payload = { title: 'RateRudder', body: event.data.text() };
    }
  }

  const title = payload.title || 'RateRudder Update';
  const options = {
    body: payload.body || '',
    icon: payload.icon || '/logo_192.png',
    badge: payload.badge || '/badge_96.png',
    tag: payload.tag || 'raterudder-notification',
    renotify: true,
    data: payload.data || {}
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
  event.notification.close();

  const data = event.notification.data || {};
  const targetUrl = data.url || '/#forecast';
  const id = data.id || data.logID || '';

  const clickPromise = id
    ? fetch(`/api/notifications/click?id=${encodeURIComponent(id)}`, {
        method: 'POST',
        credentials: 'same-origin'
      }).catch(function () {})
    : Promise.resolve();

  const navPromise = clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (windowClients) {
    for (let i = 0; i < windowClients.length; i++) {
      const client = windowClients[i];
      if (client.url && 'focus' in client) {
        if ('navigate' in client) {
          client.navigate(targetUrl);
        }
        return client.focus();
      }
    }
    if (clients.openWindow) {
      return clients.openWindow(targetUrl);
    }
  });

  event.waitUntil(Promise.all([clickPromise, navPromise]));
});
