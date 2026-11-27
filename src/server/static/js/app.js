// GitIgnore frontend behaviors. CSS does all theming via custom properties;
// JS only sets the theme cookie, swaps the <html> class, and registers the
// service worker. Kept minimal and CSP-safe (no inline handlers).

var bootRecoveryTimer = window.setTimeout(function () {
  var labels = document.body.dataset;
  function showRecoveryError() {
    var main = document.createElement('main');
    main.className = 'error-page';
    var heading = document.createElement('h1');
    heading.textContent = labels.recoveryTitle;
    var message = document.createElement('p');
    message.textContent = labels.recoveryMessage;
    main.append(heading, message);
    document.body.replaceChildren(main);
  }

  try {
    if (window.sessionStorage.getItem('gitignore_recovered')) {
      showRecoveryError();
      return;
    }
    window.sessionStorage.setItem('gitignore_recovered', '1');
  } catch (_) {
    showRecoveryError();
    return;
  }

  var recoveryTasks = [];
  if ('serviceWorker' in navigator) {
    recoveryTasks.push(navigator.serviceWorker.getRegistrations().then(function (registrations) {
      return Promise.all(registrations.map(function (registration) { return registration.unregister(); }));
    }));
  }
  if ('caches' in window) {
    recoveryTasks.push(caches.keys().then(function (keys) {
      return Promise.all(keys.map(function (key) { return caches.delete(key); }));
    }));
  }
  Promise.all(recoveryTasks).catch(function () {}).then(function () { window.location.reload(); });
}, 10000);

function completeBootRender() {
  window.clearTimeout(bootRecoveryTimer);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', completeBootRender, { once: true });
} else {
  completeBootRender();
}

// ============================================================================
// Theme toggle
// ============================================================================
// Cycle order matches the three server-rendered theme classes.
var THEME_ORDER = ['dark', 'light', 'auto'];

function currentTheme() {
  var cls = document.documentElement.className || '';
  var match = cls.match(/theme-(dark|light|auto)/);
  return match ? match[1] : 'dark';
}

function setTheme(theme) {
	document.documentElement.classList.remove('theme-dark', 'theme-light', 'theme-auto');
	document.documentElement.classList.add('theme-' + theme);
  document.cookie = 'theme=' + theme + '; path=/; max-age=31536000; SameSite=Lax';
  var label = document.querySelector('.theme-label');
  if (label) {
    label.textContent = theme;
  }
}

function cycleTheme() {
  var next = (THEME_ORDER.indexOf(currentTheme()) + 1) % THEME_ORDER.length;
  setTheme(THEME_ORDER[next]);
}

// The checkbox and labels provide the complete no-JavaScript menu. This
// enhancement adds keyboard operation, focus containment, and desktop overflow
// detection without making the menu depend on JavaScript.
var navCheckbox = document.querySelector('.nav-checkbox');
var navPanel = document.querySelector('.nav-panel');
var navToggle = document.querySelector('.nav-toggle');
var navClose = document.querySelector('.nav-close');
var navLinks = document.querySelector('.nav-links');

if (navCheckbox && navPanel && navToggle && navClose) {
  function menuFocusableElements() {
    return Array.prototype.filter.call(navPanel.querySelectorAll(
      'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [role="button"][tabindex], [tabindex]:not([tabindex="-1"])'
    ), function (element) {
      return element.getClientRects().length > 0;
    });
  }

  function syncNavigationState() {
    var isOpen = navCheckbox.checked;
    navPanel.inert = !isOpen;
    navToggle.setAttribute('aria-expanded', String(isOpen));
    if (isOpen) {
      var first = menuFocusableElements()[0];
      if (first) first.focus();
    } else if (navPanel.contains(document.activeElement)) {
      navToggle.focus();
    }
  }

  function toggleNavigationFromKeyboard(event, shouldOpen) {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    navCheckbox.checked = shouldOpen;
    navCheckbox.dispatchEvent(new Event('change', { bubbles: true }));
  }

  navPanel.inert = !navCheckbox.checked;
  navToggle.setAttribute('aria-expanded', String(navCheckbox.checked));
  navCheckbox.addEventListener('change', syncNavigationState);
  navToggle.addEventListener('keydown', function (event) {
    toggleNavigationFromKeyboard(event, !navCheckbox.checked);
  });
  navClose.addEventListener('keydown', function (event) {
    toggleNavigationFromKeyboard(event, false);
  });
  document.addEventListener('keydown', function (event) {
    if (!navCheckbox.checked) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      navCheckbox.checked = false;
      navCheckbox.dispatchEvent(new Event('change', { bubbles: true }));
      return;
    }
    if (event.key !== 'Tab') return;
    var focusable = menuFocusableElements();
    if (focusable.length === 0) {
      event.preventDefault();
      navToggle.focus();
      return;
    }
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  function updateNavigationLayout() {
    if (!navLinks) return;
    var desktop = window.matchMedia('(min-width: 768px)').matches;
    document.documentElement.classList.remove('nav-collapsed');
    if (desktop && navLinks.scrollWidth > navLinks.clientWidth + 1) {
      document.documentElement.classList.add('nav-collapsed');
    }
  }

  updateNavigationLayout();
  window.addEventListener('resize', updateNavigationLayout, { passive: true });
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(updateNavigationLayout);
  }
}

document.querySelectorAll('[data-action="theme-toggle"]').forEach(function (btn) {
  btn.addEventListener('click', cycleTheme);
  // Enter and Space cycle the theme for keyboard users.
  btn.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      cycleTheme();
    }
  });
});

document.querySelectorAll('.site-banner-dismiss').forEach(function (form) {
  form.addEventListener('submit', function (event) {
    if (!window.fetch || !window.URLSearchParams) return;
    event.preventDefault();
    fetch(form.action, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'X-Requested-With': 'fetch' },
      body: new URLSearchParams(new FormData(form)).toString()
    }).then(function (response) {
      if (response.ok) {
        var banner = form.closest('.site-banner');
        if (banner) banner.remove();
      } else {
        form.submit();
      }
    }).catch(function () { form.submit(); });
  });
});

// Clipboard is an optional enhancement; the export text remains selectable
// when clipboard access is unavailable or JavaScript is disabled.
document.querySelectorAll('[data-copy]').forEach(function (btn) {
  btn.addEventListener('click', function () {
    var target = document.getElementById(btn.dataset.copy);
    if (!target) return;
    var text = target.textContent || '';
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        btn.textContent = btn.dataset.copiedLabel || btn.textContent;
      }).catch(function () {});
    }
  });
});

// Toast notifications are visitor-facing action feedback; server-side form
// messages remain visible when JavaScript is unavailable.
var TOAST_DURATIONS = { success: 3000, info: 3000, warning: 5000, error: 0 };
var toastQueue = [];
var activeToasts = [];
var toastSequence = 0;

function dismissToast(id) {
  var index = activeToasts.findIndex(function (entry) { return entry.id === id; });
  if (index < 0) return;
  var entry = activeToasts[index];
  activeToasts.splice(index, 1);
  window.clearTimeout(entry.timer);
  entry.node.classList.remove('toast-visible');
  entry.node.addEventListener('transitionend', function () { entry.node.remove(); }, { once: true });
  window.setTimeout(function () { entry.node.remove(); }, 250);
  renderQueuedToasts();
}

function renderToast(message, type, duration) {
  var container = document.getElementById('toast-container');
  if (!container) return null;
  var id = ++toastSequence;
  var toast = document.createElement('div');
  toast.className = 'toast toast-' + type;
  toast.setAttribute('role', type === 'error' ? 'alert' : 'status');
  toast.setAttribute('aria-live', type === 'error' ? 'assertive' : 'polite');
  toast.dataset.toastId = String(id);

  var icon = document.createElement('span');
  icon.className = 'toast-icon';
  icon.setAttribute('aria-hidden', 'true');
  icon.textContent = { success: '✓', error: '✗', warning: '⚠', info: 'ℹ' }[type];
  var text = document.createElement('span');
  text.className = 'toast-message';
  text.textContent = message;
  var close = document.createElement('button');
  close.className = 'toast-close';
  close.type = 'button';
  close.setAttribute('aria-label', document.body.dataset.toastDismiss || 'Dismiss');
  close.textContent = '×';
  var progress = document.createElement('span');
  progress.className = 'toast-progress';
  progress.setAttribute('aria-hidden', 'true');
  toast.append(icon, text, close, progress);
  container.prepend(toast);

  var entry = { id: id, node: toast, timer: null, remaining: duration, started: 0 };
  activeToasts.unshift(entry);
  function startTimer() {
    if (entry.remaining <= 0 || entry.timer) return;
    entry.started = Date.now();
    toast.style.setProperty('--toast-duration', entry.remaining + 'ms');
    progress.style.animation = 'none';
    void progress.offsetHeight;
    progress.style.animation = '';
    entry.timer = window.setTimeout(function () { dismissToast(id); }, entry.remaining);
  }
  function pauseTimer() {
    if (!entry.timer) return;
    window.clearTimeout(entry.timer);
    entry.timer = null;
    entry.remaining = Math.max(0, entry.remaining - (Date.now() - entry.started));
  }
  close.addEventListener('click', function (event) { event.stopPropagation(); dismissToast(id); });
  toast.addEventListener('click', function () { dismissToast(id); });
  toast.addEventListener('mouseenter', pauseTimer);
  toast.addEventListener('mouseleave', startTimer);
  requestAnimationFrame(function () { toast.classList.add('toast-visible'); });
  startTimer();
  return id;
}

function renderQueuedToasts() {
  while (activeToasts.length < 5 && toastQueue.length) {
    var next = toastQueue.shift();
    renderToast(next.message, next.type, next.duration);
  }
}

function showToast(message, type) {
  type = Object.prototype.hasOwnProperty.call(TOAST_DURATIONS, type) ? type : 'info';
  var bodyData = document.body.dataset;
  var configuredDuration = Number(bodyData.toastDuration);
  var configuredWarningDuration = Number(bodyData.toastWarningDuration);
  if (!Number.isFinite(configuredDuration) || configuredDuration < 0) configuredDuration = 3;
  if (!Number.isFinite(configuredWarningDuration) || configuredWarningDuration < 0) configuredWarningDuration = 5;
  var duration = type === 'warning'
    ? configuredWarningDuration * 1000
    : type === 'error' ? 0 : configuredDuration * 1000;
  if (activeToasts.length >= 5) {
    toastQueue.push({ message: String(message), type: type, duration: duration });
    return null;
  }
  return renderToast(String(message), type, duration);
}

document.addEventListener('keydown', function (event) {
  if (event.key === 'Escape' && activeToasts.length) dismissToast(activeToasts[0].id);
});
window.showToast = showToast;
window.dismissToast = dismissToast;

var flashMessage = document.querySelector('.flash-message');
if (flashMessage) {
  showToast(flashMessage.textContent || '', flashMessage.dataset.type || 'success');
  flashMessage.remove();
}

// ============================================================================
// Service worker registration (PWA offline support)
// ============================================================================
if ('serviceWorker' in navigator) {
  var updateAccepted = false;
  var updateReloaded = false;
  var waitingWorker = null;

  function reloadAfterUpdate() {
    if (updateReloaded) return;
    updateReloaded = true;
    window.location.reload();
  }

  navigator.serviceWorker.addEventListener('controllerchange', function () {
    if (updateAccepted) reloadAfterUpdate();
  });

  function showUpdatePrompt(worker) {
    if (document.querySelector('.update-banner')) return;
    waitingWorker = worker;
    var labels = document.body.dataset;
    var banner = document.createElement('aside');
    banner.className = 'update-banner';
    banner.setAttribute('role', 'status');
    var message = document.createElement('span');
    message.textContent = labels.updateAvailable;
    var apply = document.createElement('button');
    apply.type = 'button';
    apply.className = 'btn btn-primary';
    apply.textContent = labels.updateNow;
    apply.addEventListener('click', function () {
      updateAccepted = true;
      if (!waitingWorker) {
        reloadAfterUpdate();
        return;
      }
      waitingWorker.postMessage({ type: 'SKIP_WAITING' });
      window.setTimeout(reloadAfterUpdate, 5000);
    });
    var later = document.createElement('button');
    later.type = 'button';
    later.className = 'btn';
    later.textContent = labels.updateLater;
    later.addEventListener('click', function () { banner.remove(); });
    banner.append(message, apply, later);
    document.body.appendChild(banner);
  }

  window.addEventListener('load', function () {
    navigator.serviceWorker.register('/sw.js', { scope: '/' }).then(function (registration) {
      if (registration.waiting && navigator.serviceWorker.controller) showUpdatePrompt(registration.waiting);
      registration.addEventListener('updatefound', function () {
        var installing = registration.installing;
        if (!installing) return;
        installing.addEventListener('statechange', function () {
          if (installing.state === 'installed' && navigator.serviceWorker.controller) showUpdatePrompt(installing);
        });
      });
      window.setInterval(function () { registration.update().catch(function () {}); }, 60 * 60 * 1000);
    }).catch(function () {
      // Registration failure is non-fatal: the site works without offline caching.
    });
  });
}
