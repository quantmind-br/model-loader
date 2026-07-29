// Shared editor page logic for the profile (base.gohtml) and backend
// (backend.gohtml) editors. Per-page routes come from body dataset:
//   data-save-path   — the save POST path ("/save" or "/backend/save")
//   data-cancel-path — the cancel POST path ("/cancel" or "/backend/cancel")

function fuzzyMatch(q, text) {
  if (!q) return true;
  q = q.toLowerCase();
  text = text.toLowerCase();
  var qi = 0;
  for (var ti = 0; ti < text.length && qi < q.length; ti++) {
    if (text.charAt(ti) === q.charAt(qi)) qi++;
  }
  return qi === q.length;
}

function toastHost() {
  return {
    items: [],
    init() {
      var self = this;
      window.addEventListener('toast', function (e) { self.push(e.detail.msg, e.detail.type || 'info'); });
    },
    push(msg, type) {
      var id = Date.now() + Math.random();
      this.items.push({ id: id, msg: msg, type: type });
      var self = this;
      setTimeout(function () { self.items = self.items.filter(function (x) { return x.id !== id; }); }, 2600);
    }
  };
}

// Read the server-rendered #issues partial and decorate matching fields inline.
function decorateIssues() {
  document.querySelectorAll('.field.has-error, .field.has-warning').forEach(function (f) {
    f.classList.remove('has-error');
    f.classList.remove('has-warning');
    var slot = f.querySelector('.field-error');
    if (slot) slot.textContent = '';
  });
  var issues = document.querySelectorAll('#issues .issue[data-field]');
  issues.forEach(function (el) {
    var field = el.getAttribute('data-field');
    if (!field) return;
    var cls = el.classList.contains('error') ? 'has-error'
      : el.classList.contains('warn') ? 'has-warning'
        : '';
    if (!cls) return;
    var wrap = document.querySelector('.field[data-field="' + CSS.escape(field) + '"]');
    if (!wrap) return;
    wrap.classList.add(cls);
    var slot = wrap.querySelector('.field-error');
    if (slot && !slot.textContent) slot.textContent = el.textContent.replace(/^[^:]*:\s*/, '');
  });
}

document.addEventListener('htmx:afterSwap', function (e) {
  if (e.target && e.target.id === 'issues') {
    decorateIssues();
    if (e.detail && e.detail.requestConfig && e.detail.requestConfig.path === document.body.dataset.savePath) {
      var n = document.querySelectorAll('#issues .issue.error').length;
      if (n > 0) {
        var msg = n === 1 ? '1 error — fix it before saving' : n + ' errors — fix them before saving';
        window.dispatchEvent(new CustomEvent('toast', { detail: { msg: msg, type: 'error' } }));
      }
    }
  }
});

// Surface non-2xx responses (htmx ignores their bodies for swaps) as a toast
// so failures from remaining http.Error paths are never silent.
document.addEventListener('htmx:responseError', function (e) {
  var msg = (e.detail && e.detail.xhr && e.detail.xhr.responseText) || 'Request failed';
  window.dispatchEvent(new CustomEvent('toast', { detail: { msg: msg, type: 'error' } }));
});

// Requests aborted by the global htmx timeout (htmx-config meta) never reach
// responseError — surface them explicitly.
document.body.addEventListener('htmx:timeout', function () {
  window.dispatchEvent(new CustomEvent('toast', { detail: { msg: 'Request timed out — try again', type: 'error' } }));
});

// A successful save/cancel navigates away via HX-Redirect; that departure is
// intentional and must not trip the unsaved-changes prompt.
var leavingIntentionally = false;
document.body.addEventListener('htmx:beforeOnLoad', function (e) {
  if (e.detail.xhr.getResponseHeader('HX-Redirect')) leavingIntentionally = true;
});
window.addEventListener('beforeunload', function (e) {
  if (leavingIntentionally) return;
  var data = window.Alpine ? Alpine.$data(document.body) : null;
  if (data && data.dirty) { e.preventDefault(); e.returnValue = ''; }
});

// Single source of truth for "discard unsaved edits?": htmx fires htmx:confirm
// for every request (Cancel button and the Escape-key htmx.ajax alike), so the
// dirty prompt lives here instead of being duplicated per trigger.
document.body.addEventListener('htmx:confirm', function (e) {
  if (e.detail.path !== document.body.dataset.cancelPath) return;
  var data = window.Alpine ? Alpine.$data(document.body) : null;
  if (data && data.dirty) {
    e.preventDefault();
    if (confirm('Discard unsaved changes?')) e.detail.issueRequest();
  }
});
