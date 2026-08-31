const state = { status: null, files: [], peers: [], events: [], query: '', rendered: false };
let adminToken = sessionStorage.getItem('boxnet-token') || '';
let refreshTimer;
const $ = selector => document.querySelector(selector);
const $$ = selector => [...document.querySelectorAll(selector)];

function escapeHtml(value = '') {
  return String(value).replace(/[&<>'"]/g, character => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[character]));
}

function bytes(value) {
  if (!value) return '0 B';
  const units = ['B','KB','MB','GB','TB']; let size = value; let unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size >= 10 || unit === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unit]}`;
}

function ago(date) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(date)) / 1000));
  if (seconds < 10) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function uptime(startedAt) {
  const minutes = Math.max(0, Math.floor((Date.now() - new Date(startedAt)) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

async function api(path, options = {}) {
  options.headers = {...(options.headers || {}), Authorization: `Bearer ${adminToken}`};
  const response = await fetch(path, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    const error = new Error(body.error || `Request failed (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return response.json();
}

async function refresh(showFeedback = false) {
  $('#refresh-button').classList.add('spinning');
  try {
    const [status, files, peers, events] = await Promise.all(['/api/status','/api/files','/api/peers','/api/events'].map(path => api(path)));
    Object.assign(state, {status, files, peers, events});
    render();
    unlockApp();
    if (showFeedback) toast('Network state refreshed');
  } catch (error) {
    if (error.status === 401) lockApp();
    else setDisconnected();
    if (showFeedback && error.status !== 401) toast(error.message, true);
  } finally {
    $('#refresh-button').classList.remove('spinning');
  }
}

function render() {
  const status = state.status;
  const onlinePeers = state.peers.filter(peer => peer.online);
  $('#side-name').textContent = status.name;
  $('#crumb-name').textContent = status.name;
  $('#side-status').textContent = status.health === 'healthy' ? 'Online and healthy' : 'Replication degraded';
  $('#side-dot').className = `status-orb ${status.health}`;
  $('#node-id-short').textContent = status.id;
  animateMetric('#metric-files', status.files, value => String(Math.round(value)));
  animateMetric('#metric-peers', status.onlinePeers, value => String(Math.round(value)));
  animateMetric('#metric-coverage', status.coverage, value => `${Math.round(value)}%`);
  $('#metric-uptime').textContent = uptime(status.startedAt);
  $('#metric-size').textContent = `${bytes(status.bytes)} stored locally`;
  $('#metric-known').textContent = `${status.knownPeers} known ${status.knownPeers === 1 ? 'node' : 'nodes'}`;
  $('#metric-target').textContent = `${status.replicationFactor} copies desired`;
  $('#nav-file-count').textContent = status.files;
  $('#nav-peer-count').textContent = status.onlinePeers;
  $('#last-updated').textContent = `Updated ${new Date().toLocaleTimeString([], {hour:'2-digit', minute:'2-digit', second:'2-digit'})}`;

  const pill = $('#health-pill');
  pill.className = `health-pill ${status.health}`;
  pill.innerHTML = `<span></span><b>${status.health === 'healthy' ? 'Healthy' : 'Needs attention'}</b>`;
  const notice = $('#network-notice');
  notice.classList.toggle('degraded', status.health !== 'healthy');
  if (status.files === 0) {
    $('#notice-title').textContent = 'Secure node ready';
    $('#notice-copy').textContent = 'Publish your first file to begin building resilient replicas.';
  } else if (status.health === 'healthy') {
    $('#notice-title').textContent = 'Replica target satisfied';
    $('#notice-copy').textContent = `${onlinePeers.length + 1} active nodes are protecting content in this mesh.`;
  } else {
    $('#notice-title').textContent = 'More peers are needed';
    $('#notice-copy').textContent = `Bring ${Math.max(1, status.replicationFactor - onlinePeers.length - 1)} more peer online to restore full coverage.`;
  }
  renderFiles();
  renderPeers();
  renderEvents();
  state.rendered = true;
}

function animateMetric(selector, target, formatter) {
  const element = $(selector);
  if (matchMedia('(prefers-reduced-motion: reduce)').matches || !state.rendered) {
    element.textContent = formatter(target);
    element.dataset.value = target;
    return;
  }
  const start = Number(element.dataset.value || 0);
  if (start === target) return;
  const started = performance.now();
  const duration = 550;
  const tick = now => {
    const progress = Math.min(1, (now - started) / duration);
    const eased = 1 - Math.pow(1 - progress, 3);
    element.textContent = formatter(start + (target - start) * eased);
    if (progress < 1) requestAnimationFrame(tick);
    else element.dataset.value = target;
  };
  requestAnimationFrame(tick);
}

function renderFiles() {
  const list = state.files.filter(file => file.name.toLowerCase().includes(state.query));
  $('#file-empty').hidden = state.files.length !== 0;
  const table = $('#file-list');
  table.hidden = state.files.length === 0;
  table.innerHTML = list.length ? list.map((file, index) => `<tr style="--row:${index}"><td><div class="file-cell"><span class="file-icon"><svg><use href="#i-files"/></svg></span><span class="file-meta"><strong>${escapeHtml(file.name)}</strong><span>${file.hash.slice(0,12)}</span></span></div></td><td>${bytes(file.size)}</td><td><div class="replica"><span class="replica-dots">${Array.from({length:state.status.replicationFactor}, (_, i) => `<i class="${i < file.replicas ? 'on' : ''}"></i>`).join('')}</span>${file.replicas}/${state.status.replicationFactor}</div></td><td title="${escapeHtml(new Date(file.createdAt).toLocaleString())}">${ago(file.createdAt)}</td><td><button class="download-link" data-download="${file.hash}" data-name="${escapeHtml(file.name)}" aria-label="Download ${escapeHtml(file.name)}" title="Download"><svg><use href="#i-download"/></svg></button></td></tr>`).join('') : `<tr><td colspan="5"><div class="empty-inline">No files match “${escapeHtml(state.query)}”</div></td></tr>`;
}

function renderPeers() {
  const online = state.peers.filter(peer => peer.online).length;
  $('#peer-badge').innerHTML = `<i></i>${online} online`;
  $('#peer-list').innerHTML = state.peers.length ? state.peers.map((peer, index) => `<div class="peer-row ${peer.online ? 'online' : ''}" style="--row:${index}"><span class="peer-avatar"><svg><use href="#i-network"/></svg></span><span class="peer-info"><strong>${escapeHtml(peer.name || peer.id)}</strong><span>${escapeHtml(peer.address)} · ${peer.files} files</span></span><span class="peer-state ${peer.online ? '' : 'offline'}">${peer.online ? 'Online' : 'Offline'}</span></div>`).join('') : '<div class="empty-inline">No peer signals yet. Start another trusted node on this network.</div>';
}

function renderEvents() {
  const icons = {file:'i-files', sync:'i-refresh', warning:'i-pulse', peer:'i-network', node:'i-shield-check'};
  $('#activity-list').innerHTML = state.events.length ? state.events.slice(0,5).map((event, index) => `<div class="activity-row" style="--row:${index}"><span class="activity-mark ${escapeHtml(event.kind)}"><svg><use href="#${icons[event.kind] || 'i-pulse'}"/></svg></span><span class="activity-info"><strong>${escapeHtml(event.message)}</strong><span title="${escapeHtml(new Date(event.at).toLocaleString())}">${ago(event.at)}</span></span></div>`).join('') : '<div class="empty-inline">Activity will appear as the mesh changes.</div>';
}

function setDisconnected() {
  $('#health-pill').className = 'health-pill degraded';
  $('#health-pill').innerHTML = '<span></span><b>Disconnected</b>';
  $('#side-status').textContent = 'Signal lost';
  $('#side-dot').className = 'status-orb degraded';
  $('#last-updated').textContent = 'Connection interrupted';
}

function toast(message, error = false) {
  const element = $('#toast');
  $('#toast-copy').textContent = message;
  element.querySelector('use').setAttribute('href', error ? '#i-x' : '#i-check');
  element.className = `toast show${error ? ' error' : ''}`;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => element.className = 'toast', 3400);
}

function lockApp(message = '') {
  clearInterval(refreshTimer);
  $('#app-shell').setAttribute('aria-hidden', 'true');
  $('#app-shell').classList.remove('app-ready');
  const auth = $('#auth-screen');
  auth.hidden = false;
  auth.classList.remove('leaving');
  document.body.classList.add('auth-open');
  document.body.classList.remove('booting');
  if (message) showAuthError(message);
  startMesh();
  setTimeout(() => $('#login-token').focus(), 120);
}

function unlockApp() {
  const shell = $('#app-shell');
  shell.setAttribute('aria-hidden', 'false');
  shell.classList.add('app-ready');
  document.body.classList.remove('auth-open', 'booting');
  const auth = $('#auth-screen');
  if (!auth.hidden && !auth.classList.contains('leaving')) {
    auth.classList.add('leaving');
    setTimeout(() => { auth.hidden = true; stopMesh(); }, 720);
  }
  clearInterval(refreshTimer);
  refreshTimer = setInterval(refresh, 5000);
}

function showAuthError(message) {
  const error = $('#auth-error');
  error.textContent = message;
  error.classList.add('visible');
  const auth = $('#auth-screen');
  auth.classList.remove('denied');
  void auth.offsetWidth;
  auth.classList.add('denied');
}

const uploadDialog = $('#upload-dialog');
const fileInput = $('#file-input');
const dropZone = $('#drop-zone');
$$('[data-open-upload]').forEach(button => button.addEventListener('click', () => uploadDialog.showModal()));
fileInput.addEventListener('change', () => {
  if (!fileInput.files[0]) return;
  $('#drop-title').textContent = fileInput.files[0].name;
  $('#drop-copy').textContent = `${bytes(fileInput.files[0].size)} · ready to verify`;
});
['dragenter','dragover'].forEach(type => dropZone.addEventListener(type, event => { event.preventDefault(); dropZone.classList.add('dragging'); }));
['dragleave','drop'].forEach(type => dropZone.addEventListener(type, event => { event.preventDefault(); dropZone.classList.remove('dragging'); }));
dropZone.addEventListener('drop', event => {
  if (!event.dataTransfer.files.length) return;
  fileInput.files = event.dataTransfer.files;
  fileInput.dispatchEvent(new Event('change'));
});

$('#upload-form').addEventListener('submit', event => {
  if (event.submitter?.value === 'cancel') return;
  event.preventDefault();
  if (!fileInput.files[0]) { toast('Choose a file before publishing', true); return; }
  const data = new FormData(); data.append('file', fileInput.files[0]);
  const xhr = new XMLHttpRequest();
  $('#upload-progress').hidden = false;
  $('#publish-button').disabled = true;
  xhr.upload.onprogress = progress => {
    if (!progress.lengthComputable) return;
    const value = Math.round(progress.loaded / progress.total * 100);
    $('#upload-progress progress').value = value;
    $('#progress-value').textContent = `${value}%`;
  };
  xhr.onload = async () => {
    $('#publish-button').disabled = false;
    if (xhr.status >= 200 && xhr.status < 300) {
      uploadDialog.close(); resetUpload(); toast('File published and verified'); await refresh();
    } else if (xhr.status === 401) {
      uploadDialog.close(); adminToken = ''; sessionStorage.removeItem('boxnet-token'); lockApp('Your session expired. Unlock the node again.');
    } else {
      let message = 'Upload failed'; try { message = JSON.parse(xhr.responseText).error || message; } catch {}
      toast(message, true);
    }
  };
  xhr.onerror = () => { $('#publish-button').disabled = false; toast('Upload failed: node unreachable', true); };
  xhr.open('POST','/api/files');
  xhr.setRequestHeader('Authorization', `Bearer ${adminToken}`);
  xhr.send(data);
});

function resetUpload() {
  fileInput.value = '';
  $('#drop-title').textContent = 'Drop a file into the mesh';
  $('#drop-copy').textContent = 'or click to browse · up to 1 GB';
  $('#upload-progress').hidden = true;
  $('#upload-progress progress').value = 0;
  $('#progress-value').textContent = '0%';
}

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  const candidate = $('#login-token').value.trim();
  if (!candidate) { showAuthError('Enter the dashboard token to continue.'); return; }
  adminToken = candidate;
  $('#auth-submit').classList.add('loading');
  $('#auth-submit').disabled = true;
  $('#auth-error').classList.remove('visible');
  try {
    await api('/api/status');
    sessionStorage.setItem('boxnet-token', adminToken);
    $('#login-token').value = '';
    await refresh();
    toast('Secure session established');
  } catch (error) {
    adminToken = '';
    sessionStorage.removeItem('boxnet-token');
    showAuthError(error.status === 401 ? 'That token does not match this node.' : 'This node could not be reached securely.');
  } finally {
    $('#auth-submit').classList.remove('loading');
    $('#auth-submit').disabled = false;
  }
});

$('#toggle-token').addEventListener('click', () => {
  const input = $('#login-token');
  const visible = input.type === 'text';
  input.type = visible ? 'password' : 'text';
  $('#toggle-token use').setAttribute('href', visible ? '#i-eye' : '#i-eye-off');
  $('#toggle-token').setAttribute('aria-label', visible ? 'Show token' : 'Hide token');
  input.focus();
});

$('#auth-help').addEventListener('click', () => {
  const copy = $('#auth-help-copy');
  copy.hidden = !copy.hidden;
  $('#auth-help').classList.toggle('open', !copy.hidden);
});

$('#logout-button').addEventListener('click', () => {
  adminToken = '';
  sessionStorage.removeItem('boxnet-token');
  lockApp('Dashboard locked. Your node is still running.');
});

$('#file-list').addEventListener('click', async event => {
  const button = event.target.closest('[data-download]');
  if (!button) return;
  button.classList.add('spinning');
  try {
    const response = await fetch(`/api/files/${button.dataset.download}/download`, {headers:{Authorization:`Bearer ${adminToken}`}});
    if (!response.ok) throw new Error('Download failed');
    const url = URL.createObjectURL(await response.blob());
    const link = document.createElement('a'); link.href = url; link.download = button.dataset.name; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    toast(`Downloaded ${button.dataset.name}`);
  } catch (error) { toast(error.message, true); }
  finally { button.classList.remove('spinning'); }
});

$('#refresh-button').addEventListener('click', () => refresh(true));
$('#sync-button').addEventListener('click', async () => {
  const button = $('#sync-button'); button.classList.add('syncing'); button.disabled = true;
  try { await api('/api/sync',{method:'POST'}); toast('Mesh reconciliation started'); setTimeout(() => refresh(), 900); }
  catch (error) { toast(error.message, true); }
  finally { setTimeout(() => { button.classList.remove('syncing'); button.disabled = false; }, 700); }
});
$('#file-search').addEventListener('input', event => { state.query = event.target.value.trim().toLowerCase(); renderFiles(); });
$('.mobile-menu').addEventListener('click', () => $('.sidebar').classList.toggle('open'));
document.addEventListener('click', event => { if (event.target.closest('.nav-item') && innerWidth <= 720) $('.sidebar').classList.remove('open'); });
document.addEventListener('keydown', event => {
  if (event.key === '/' && !event.target.matches('input')) { event.preventDefault(); $('#file-search').focus(); }
  if (event.key.toLowerCase() === 'u' && !event.target.matches('input') && !$('#app-shell').getAttribute('aria-hidden').includes('true')) uploadDialog.showModal();
  if (event.key === 'Escape' && $('.sidebar').classList.contains('open')) $('.sidebar').classList.remove('open');
});

const sections = $$('.content [id]');
const navItems = $$('.nav-item');
if ('IntersectionObserver' in window) {
  const observer = new IntersectionObserver(entries => entries.forEach(entry => {
    if (!entry.isIntersecting) return;
    navItems.forEach(item => item.classList.toggle('active', item.getAttribute('href') === `#${entry.target.id}`));
  }), {rootMargin:'-20% 0px -68%'});
  sections.forEach(section => observer.observe(section));
}

let meshFrame = 0;
let meshNodes = [];
let meshPointer = {x:-1000,y:-1000};
function startMesh() {
  if (meshFrame || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  const canvas = $('#mesh-canvas');
  const context = canvas.getContext('2d');
  const resize = () => {
    const ratio = Math.min(devicePixelRatio || 1, 2);
    canvas.width = innerWidth * ratio; canvas.height = innerHeight * ratio;
    canvas.style.width = `${innerWidth}px`; canvas.style.height = `${innerHeight}px`;
    context.setTransform(ratio,0,0,ratio,0,0);
    const count = Math.min(44, Math.max(20, Math.floor(innerWidth / 38)));
    meshNodes = Array.from({length:count}, (_, index) => meshNodes[index] || ({x:Math.random()*innerWidth,y:Math.random()*innerHeight,vx:(Math.random()-.5)*.18,vy:(Math.random()-.5)*.18,r:Math.random()*1.5+1}));
  };
  resize();
  canvas.onpointermove = event => { meshPointer = {x:event.clientX,y:event.clientY}; };
  canvas.onpointerleave = () => { meshPointer = {x:-1000,y:-1000}; };
  window.addEventListener('resize', resize, {passive:true});
  const draw = () => {
    if ($('#auth-screen').hidden) { meshFrame = 0; return; }
    context.clearRect(0,0,innerWidth,innerHeight);
    meshNodes.forEach((node, i) => {
      node.x += node.vx; node.y += node.vy;
      if (node.x < -20 || node.x > innerWidth + 20) node.vx *= -1;
      if (node.y < -20 || node.y > innerHeight + 20) node.vy *= -1;
      const pointerDistance = Math.hypot(node.x-meshPointer.x,node.y-meshPointer.y);
      if (pointerDistance < 150) { node.x += (node.x-meshPointer.x)/pointerDistance*.18; node.y += (node.y-meshPointer.y)/pointerDistance*.18; }
      for (let j=i+1;j<meshNodes.length;j++) {
        const other=meshNodes[j]; const distance=Math.hypot(node.x-other.x,node.y-other.y);
        if (distance < 125) { context.beginPath(); context.moveTo(node.x,node.y); context.lineTo(other.x,other.y); context.strokeStyle=`rgba(128,116,242,${(1-distance/125)*.16})`; context.lineWidth=.7; context.stroke(); }
      }
      context.beginPath(); context.arc(node.x,node.y,node.r,0,Math.PI*2); context.fillStyle=i%5===0?'rgba(115,213,173,.7)':'rgba(172,165,255,.7)'; context.fill();
    });
    meshFrame = requestAnimationFrame(draw);
  };
  meshFrame = requestAnimationFrame(draw);
}
function stopMesh() { if (meshFrame) cancelAnimationFrame(meshFrame); meshFrame = 0; }

document.body.classList.add('auth-open');
startMesh();
if (adminToken) refresh();
else lockApp();
