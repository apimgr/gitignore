// GitIgnore frontend behaviors. CSS does all theming via custom properties;
// JS only sets the theme cookie, swaps the <html> class, and registers the
// service worker. Kept minimal and CSP-safe (no inline handlers).

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
  document.documentElement.className = 'theme-' + theme;
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

// ============================================================================
// Service worker registration (PWA offline support)
// ============================================================================
if ('serviceWorker' in navigator) {
  window.addEventListener('load', function () {
    navigator.serviceWorker.register('/sw.js').catch(function () {
      // Registration failure is non-fatal: the site works without offline caching.
    });
  });
}
