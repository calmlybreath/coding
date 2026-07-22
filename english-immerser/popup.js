// ============================================================
//  English Immerser — Popup Settings
// ============================================================

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

// Default settings
const DEFAULTS = {
  enabled: true,
  density: 'medium',
  apiKey: '',
  endpoint: '',
  model: ''
};

// ============================================================
//  Init — load current settings from storage
// ============================================================

async function init() {
  const settings = await chrome.storage.local.get(DEFAULTS);

  // API key
  $('#apiKey').value = settings.apiKey || '';
  $('#endpoint').value = settings.endpoint || '';
  $('#model').value = settings.model || '';

  // Enable/disable toggle
  updateToggle('toggleEnable', 'toggleDisable', settings.enabled !== false);

  // Density
  $$('.popup-density').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.value === settings.density);
  });

  // Event listeners
  $('#toggleEnable').addEventListener('click', () => {
    updateToggle('toggleEnable', 'toggleDisable', true);
  });
  $('#toggleDisable').addEventListener('click', () => {
    updateToggle('toggleEnable', 'toggleDisable', false);
  });

  $$('.popup-density').forEach(btn => {
    btn.addEventListener('click', () => {
      $$('.popup-density').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
    });
  });

  $('#saveBtn').addEventListener('click', saveSettings);
}

function updateToggle(enableId, disableId, enabled) {
  const enableBtn = $(`#${enableId}`);
  const disableBtn = $(`#${disableId}`);
  if (enabled) {
    enableBtn.classList.add('active');
    disableBtn.classList.remove('active');
  } else {
    enableBtn.classList.remove('active');
    disableBtn.classList.add('active');
  }
}

// ============================================================
//  Save
// ============================================================

async function saveSettings() {
  const apiKey = $('#apiKey').value.trim();
  const endpoint = $('#endpoint').value.trim();
  const model = $('#model').value.trim();
  const enabled = $('#toggleEnable').classList.contains('active');
  const densityBtn = document.querySelector('.popup-density.active');
  const density = densityBtn ? densityBtn.dataset.value : 'medium';

  await chrome.storage.local.set({ apiKey, endpoint, model, enabled, density });
  showStatus('✅ 设置已保存，刷新页面生效', 'success');
}

function showStatus(msg, type) {
  const el = $('#statusMsg');
  el.textContent = msg;
  el.className = 'popup-status-msg ' + type;
  setTimeout(() => { el.textContent = ''; el.className = 'popup-status-msg'; }, 3000);
}

// ============================================================
//  Start
// ============================================================

document.addEventListener('DOMContentLoaded', init);
