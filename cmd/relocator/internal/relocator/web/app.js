const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

let timer = null;

async function fetchJSON(path, opts) {
  const resp = await fetch(path, opts);
  if (!resp.ok) throw new Error(`${path} ${resp.status}`);
  return resp.json();
}

function fmt(n) { return new Intl.NumberFormat().format(n || 0); }

async function refresh() {
  try {
    const [state, events] = await Promise.all([
      fetchJSON('/api/state'),
      fetchJSON('/api/events?limit=100'),
    ]);
    renderTotals(state.totals);
    renderBuckets(state.buckets);
    renderEvents(events);
    $('#last-refresh').textContent = new Date().toLocaleTimeString();
  } catch (err) {
    $('#last-refresh').textContent = 'error: ' + err.message;
  }
}

function renderTotals(totals) {
  $$('[data-total]').forEach((el) => {
    el.textContent = fmt(totals[el.dataset.total]);
  });
}

function renderBuckets(buckets) {
  const root = $('#buckets');
  root.innerHTML = '';
  for (const b of buckets) {
    const el = document.createElement('div');
    el.className = 'bucket';
    el.innerHTML = `
      <header>
        <div>
          <h3>${escapeHTML(b.config.name)}</h3>
          <div class="sub">${escapeHTML(b.config.endpoint)} · ${escapeHTML(b.config.bucket)}${b.config.prefix ? '/' + escapeHTML(b.config.prefix) : ''}</div>
        </div>
        <button data-poll="${escapeHTML(b.config.name)}">poll</button>
      </header>
      <div class="counts">
        <span>downloaded</span><b>${fmt(b.stats.downloaded)}</b><span>bytes</span>
        <span>extracted</span><b>${fmt(b.stats.extracted)}</b><span>${humanBytes(b.stats.bytesDownloaded)}</span>
        <span>posted</span><b>${fmt(b.stats.posted)}</b><span>in-flight: ${b.config.inFlight}</span>
        <span>deleted</span><b>${fmt(b.stats.deleted)}</b><span>skipped: ${fmt(b.stats.skipped)}</span>
        <span>dl-fail</span><b>${fmt(b.stats.downloadFailed)}</b><span>extract-fail: ${fmt(b.stats.extractFailed)}</span>
        <span>pw-fail</span><b>${fmt(b.stats.passwordFailed)}</b><span>post-fail: ${fmt(b.stats.postFailed)}</span>
      </div>
      ${b.stats.lastError ? `<div class="err">${escapeHTML(b.stats.lastError)}</div>` : ''}
    `;
    root.appendChild(el);
  }
  $$('[data-poll]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      try { await fetch(`/api/buckets/${encodeURIComponent(btn.dataset.poll)}/poll`, { method: 'POST' }); }
      finally { btn.disabled = false; setTimeout(refresh, 500); }
    });
  });
}

function renderEvents(events) {
  const tbody = $('#events tbody');
  tbody.innerHTML = '';
  for (const e of events) {
    const tr = document.createElement('tr');
    if (e.level === 'error') tr.className = 'error';
    else if (e.level === 'warn') tr.className = 'warn';
    tr.innerHTML = `
      <td><code>${new Date(e.time).toLocaleTimeString()}</code></td>
      <td>${escapeHTML(e.level)}</td>
      <td>${escapeHTML(e.bucket || '')}</td>
      <td><code>${escapeHTML(e.object || '')}</code></td>
      <td>${escapeHTML(e.message || '')}</td>
    `;
    tbody.appendChild(tr);
  }
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

function humanBytes(b) {
  if (!b) return '0 B';
  const units = ['B','KB','MB','GB','TB'];
  let i = 0;
  while (b >= 1024 && i < units.length - 1) { b /= 1024; i++; }
  return b.toFixed(1) + ' ' + units[i];
}

function startAuto() {
  if (timer) clearInterval(timer);
  if ($('#auto').checked) timer = setInterval(refresh, 5000);
}

$('#refresh').addEventListener('click', refresh);
$('#auto').addEventListener('change', startAuto);
refresh();
startAuto();
