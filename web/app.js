const state = { status: null, files: [], peers: [], events: [], query: '' };
const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];

function escapeHtml(value = '') {
  return String(value).replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));
}
function bytes(value) {
  if (!value) return '0 B';
  const units = ['B','KB','MB','GB','TB']; let size = value; let unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size >= 10 || unit === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unit]}`;
}
function ago(date) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(date)) / 1000));
  if (seconds < 10) return 'just now'; if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds/60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds/3600)}h ago`;
  return `${Math.floor(seconds/86400)}d ago`;
}
function uptime(startedAt) {
  const minutes = Math.max(0, Math.floor((Date.now() - new Date(startedAt)) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60); if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}
async function api(path, options) {
  const response = await fetch(path, options);
  if (!response.ok) { const body = await response.json().catch(() => ({})); throw new Error(body.error || `Request failed (${response.status})`); }
  return response.json();
}
async function refresh(showFeedback = false) {
  $('#refresh-button').classList.add('spinning');
  try {
    const [status, files, peers, events] = await Promise.all(['/api/status','/api/files','/api/peers','/api/events'].map(path => api(path)));
    Object.assign(state, { status, files, peers, events }); render();
    if (showFeedback) toast('Network state refreshed');
  } catch (error) {
    setDisconnected(); if (showFeedback) toast(error.message, true);
  } finally { $('#refresh-button').classList.remove('spinning'); }
}
function render() {
  const s = state.status; const online = state.peers.filter(p => p.online);
  $('#side-name').textContent = s.name; $('#crumb-name').textContent = s.name;
  $('#side-status').textContent = s.health === 'healthy' ? 'Online and healthy' : 'Replication degraded';
  $('#side-dot').className = `status-dot ${s.health}`; $('#node-id-short').textContent = s.id.slice(0, 20);
  $('#nav-file-count').textContent = s.files; $('#nav-peer-count').textContent = s.onlinePeers;
  $('#metric-files').textContent = s.files; $('#metric-size').textContent = `${bytes(s.bytes)} stored locally`;
  $('#metric-peers').textContent = s.onlinePeers; $('#metric-known').textContent = `${s.knownPeers} known ${s.knownPeers === 1 ? 'node' : 'nodes'}`;
  $('#metric-coverage').textContent = `${Math.round(s.coverage)}%`; $('#metric-target').textContent = `${s.replicationFactor} copies desired`;
  $('#metric-uptime').textContent = uptime(s.startedAt);
  const pill = $('#health-pill'); pill.className = `health-pill ${s.health}`; pill.innerHTML = `<span></span><b>${s.health === 'healthy' ? 'Healthy' : 'Degraded'}</b>`;
  const notice = $('#network-notice'); notice.classList.toggle('degraded', s.health !== 'healthy');
  $('#notice-title').textContent = s.health === 'healthy' ? 'Your files are available' : 'More peers are needed';
  $('#notice-copy').textContent = s.health === 'healthy' ? `${online.length + 1} nodes can keep serving data if one disconnects.` : `This node is operating locally. Bring ${Math.max(1, s.replicationFactor - online.length - 1)} more peer online to meet the replica target.`;
  renderFiles(); renderPeers(); renderEvents();
}
function renderFiles() {
  const list = state.files.filter(file => file.name.toLowerCase().includes(state.query));
  $('#file-empty').hidden = state.files.length !== 0;
  const tbody = $('#file-list'); tbody.hidden = state.files.length === 0;
  tbody.innerHTML = list.length ? list.map(file => `<tr><td><div class="file-cell"><span class="file-icon"><svg><use href="#i-file"/></svg></span><span class="file-meta"><strong>${escapeHtml(file.name)}</strong><span>${file.hash.slice(0,12)}</span></span></div></td><td>${bytes(file.size)}</td><td><div class="replica"><span class="replica-dots">${Array.from({length:state.status.replicationFactor},(_,i)=>`<i class="${i<file.replicas?'on':''}"></i>`).join('')}</span>${file.replicas}/${state.status.replicationFactor}</div></td><td title="${escapeHtml(new Date(file.createdAt).toLocaleString())}">${ago(file.createdAt)}</td><td><a class="download-link" href="/api/files/${file.hash}/download" aria-label="Download ${escapeHtml(file.name)}" title="Download"><svg><use href="#i-download"/></svg></a></td></tr>`).join('') : `<tr><td colspan="5"><div class="empty-inline">No files match “${escapeHtml(state.query)}”</div></td></tr>`;
}
function renderPeers() {
  const online = state.peers.filter(p => p.online).length; $('#peer-badge').textContent = `${online} online`;
  $('#peer-list').innerHTML = state.peers.length ? state.peers.map(peer => `<div class="peer-row"><span class="peer-avatar"><svg><use href="#i-nodes"/></svg></span><span class="peer-info"><strong>${escapeHtml(peer.name || peer.id)}</strong><span>${escapeHtml(peer.address)} · ${peer.files} files</span></span><span class="peer-state ${peer.online?'':'offline'}">${peer.online?'Online':'Offline'}</span></div>`).join('') : '<div class="empty-inline">No peers found yet. Keep this page open while another node joins the LAN.</div>';
}
function renderEvents() {
  $('#activity-list').innerHTML = state.events.length ? state.events.slice(0,5).map(event => `<div class="activity-row"><span class="activity-mark ${escapeHtml(event.kind)}"></span><span class="activity-info"><strong>${escapeHtml(event.message)}</strong><span title="${escapeHtml(new Date(event.at).toLocaleString())}">${ago(event.at)}</span></span></div>`).join('') : '<div class="empty-inline">No activity recorded yet.</div>';
}
function setDisconnected() {
  $('#health-pill').className = 'health-pill degraded'; $('#health-pill').innerHTML = '<span></span><b>Disconnected</b>';
  $('#side-status').textContent = 'Dashboard disconnected'; $('#side-dot').className = 'status-dot degraded';
}
function toast(message, error = false) {
  const el = $('#toast'); el.textContent = message; el.className = `toast show${error?' error':''}`;
  clearTimeout(toast.timer); toast.timer = setTimeout(() => el.className = 'toast', 3200);
}

const dialog = $('#upload-dialog'), input = $('#file-input'), zone = $('#drop-zone');
$$('[data-open-upload]').forEach(button => button.addEventListener('click', () => dialog.showModal()));
input.addEventListener('change', () => { if (input.files[0]) { $('#drop-title').textContent = input.files[0].name; $('#drop-copy').textContent = bytes(input.files[0].size); } });
['dragenter','dragover'].forEach(type => zone.addEventListener(type, event => { event.preventDefault(); zone.classList.add('dragging'); }));
['dragleave','drop'].forEach(type => zone.addEventListener(type, event => { event.preventDefault(); zone.classList.remove('dragging'); }));
zone.addEventListener('drop', event => { if (event.dataTransfer.files.length) { input.files = event.dataTransfer.files; input.dispatchEvent(new Event('change')); } });
$('#upload-form').addEventListener('submit', event => {
  event.preventDefault(); if (!input.files[0]) return;
  const data = new FormData(); data.append('file', input.files[0]); const xhr = new XMLHttpRequest();
  $('#upload-progress').hidden = false; $('#publish-button').disabled = true;
  xhr.upload.onprogress = event => { if (event.lengthComputable) { const value = Math.round(event.loaded/event.total*100); $('progress').value = value; $('#progress-value').textContent = `${value}%`; } };
  xhr.onload = async () => { $('#publish-button').disabled = false; if (xhr.status >= 200 && xhr.status < 300) { dialog.close(); input.value = ''; $('#drop-title').textContent = 'Drop a file here or choose one'; $('#drop-copy').textContent = 'Up to 1 GB · verified with SHA-256'; $('#upload-progress').hidden = true; toast('File published to the mesh'); await refresh(); } else { let message='Upload failed'; try{message=JSON.parse(xhr.responseText).error||message}catch{} toast(message,true); } };
  xhr.onerror = () => { $('#publish-button').disabled = false; toast('Upload failed: node unreachable', true); };
  xhr.open('POST','/api/files'); xhr.send(data);
});
$('#refresh-button').addEventListener('click', () => refresh(true));
$('#sync-button').addEventListener('click', async () => { try { await api('/api/sync',{method:'POST'}); toast('Peer synchronization started'); setTimeout(()=>refresh(),1000); } catch(error){toast(error.message,true)} });
$('#file-search').addEventListener('input', event => { state.query = event.target.value.trim().toLowerCase(); renderFiles(); });
$('.mobile-menu').addEventListener('click', () => $('.sidebar').classList.toggle('open'));
document.addEventListener('click', event => { if (event.target.closest('.nav-item') && innerWidth <= 700) $('.sidebar').classList.remove('open'); });
refresh(); setInterval(refresh, 5000);
