// Hardware Device Management UI

const API_BASE = '/api/hardware';

// Device icons mapping
const DEVICE_ICONS = {
  xiaohong: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="5" y="2" width="14" height="20" rx="2"/><path d="M12 18h.01"/></svg>`,
  qianwen_a2: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 1a3 3 0 00-3 3v8a3 3 0 006 0V4a3 3 0 00-3-3z"/><path d="M19 10v2a7 7 0 01-14 0v-2"/><line x1="12" y1="19" x2="12" y2="23"/><line x1="8" y1="23" x2="16" y2="23"/></svg>`,
};

const CAPABILITY_LABELS = {
  audio_in: '麦克风',
  audio_out: '扬声器',
  sensor: '传感器',
  display: '显示屏',
  button: '按键',
  storage: '存储',
  network: '网络',
  usb: 'USB',
  ble: '蓝牙',
  transcribe: '语音转写',
};

// State
let devices = [];
let instances = [];
let selectedWifi = null;

// Initialize
document.addEventListener('DOMContentLoaded', () => {
  loadDevices();
  setupEventListeners();
});

function setupEventListeners() {
  document.getElementById('refresh-btn').addEventListener('click', loadDevices);

  // Modal close buttons
  document.querySelectorAll('[data-close]').forEach(btn => {
    btn.addEventListener('click', () => {
      document.getElementById('xiaohong-modal').classList.remove('active');
      resetSetupSteps();
    });
  });

  // Xiaohong setup flow
  document.getElementById('scan-wifi-btn').addEventListener('click', scanWiFi);
  document.getElementById('connect-wifi-btn').addEventListener('click', connectWiFi);
  document.getElementById('retry-btn').addEventListener('click', () => {
    goToStep(1);
  });
}

async function loadDevices() {
  try {
    const [devicesRes, instancesRes] = await Promise.all([
      fetch(`${API_BASE}/devices`),
      fetch(`${API_BASE}/instances`),
    ]);

    devices = await devicesRes.json();
    instances = await instancesRes.json();

    renderDeviceGrid();
    renderConnectedList();
  } catch (err) {
    console.error('Failed to load devices:', err);
  }
}

function renderDeviceGrid() {
  const grid = document.getElementById('device-grid');
  grid.innerHTML = '';

  devices.devices.forEach(device => {
    const isConnected = instances.instances.some(i => i.id === device.id);
    const card = document.createElement('div');
    card.className = 'device-card';
    card.innerHTML = `
      <div class="device-card-header">
        <div class="device-icon">
          ${DEVICE_ICONS[device.id] || '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9 9h6v6H9z"/></svg>'}
        </div>
        <div class="device-info">
          <h3>${device.name}</h3>
          <p>${device.description}</p>
        </div>
      </div>
      <div class="device-capabilities">
        ${device.capabilities.map(cap => `<span class="capability-tag">${CAPABILITY_LABELS[cap] || cap}</span>`).join('')}
      </div>
      <div class="device-actions">
        ${isConnected
          ? `<button class="btn btn-secondary" onclick="disconnectDevice('${device.id}')">断开</button>`
          : `<button class="btn btn-primary" onclick="connectDevice('${device.id}')">${device.id === 'xiaohong' ? '一键配置' : '连接'}</button>`
        }
      </div>
    `;
    grid.appendChild(card);
  });
}

function renderConnectedList() {
  const list = document.getElementById('connected-list');

  if (instances.instances.length === 0) {
    list.innerHTML = `
      <div class="empty-state">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9 9h6v6H9z"/></svg>
        <p>暂无已连接设备</p>
      </div>
    `;
    return;
  }

  list.innerHTML = '';
  instances.instances.forEach(inst => {
    const item = document.createElement('div');
    item.className = 'connected-item';
    item.innerHTML = `
      <div class="connected-item-info">
        <div class="status-indicator"></div>
        <span class="connected-item-name">${inst.name}</span>
      </div>
      <div class="connected-item-actions">
        <button class="btn btn-ghost" onclick="showDeviceInfo('${inst.id}')">详情</button>
        <button class="btn btn-secondary" onclick="disconnectDevice('${inst.id}')">断开</button>
      </div>
    `;
    list.appendChild(item);
  });
}

async function connectDevice(deviceId) {
  if (deviceId === 'xiaohong') {
    // Show Xiaohong setup modal
    document.getElementById('xiaohong-modal').classList.add('active');
    goToStep(1);
  } else {
    // Direct connect for other devices
    try {
      const res = await fetch(`${API_BASE}/connect`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ device_id: deviceId, config: {} }),
      });
      const data = await res.json();
      if (res.ok) {
        await loadDevices();
      } else {
        alert(`连接失败：${data.error}`);
      }
    } catch (err) {
      alert(`连接失败：${err.message}`);
    }
  }
}

async function disconnectDevice(deviceId) {
  try {
    const res = await fetch(`${API_BASE}/disconnect`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: deviceId }),
    });
    if (res.ok) {
      await loadDevices();
    }
  } catch (err) {
    console.error('Disconnect failed:', err);
  }
}

async function showDeviceInfo(deviceId) {
  try {
    const res = await fetch(`${API_BASE}/info/${deviceId}`);
    const info = await res.json();
    console.log('Device info:', info);
    // TODO: Show info in a modal
  } catch (err) {
    console.error('Failed to get device info:', err);
  }
}

// Xiaohong Setup Flow
function goToStep(stepNum) {
  document.querySelectorAll('.step').forEach(s => s.classList.remove('active'));
  const step = document.querySelector(`.step[data-step="${stepNum}"]`);
  if (step) step.classList.add('active');
}

function resetSetupSteps() {
  goToStep(1);
  selectedWifi = null;
  document.getElementById('wifi-password').value = '';
  document.getElementById('wifi-list').innerHTML = '';
}

async function scanWiFi() {
  const btn = document.getElementById('scan-wifi-btn');
  btn.disabled = true;
  btn.textContent = '扫描中...';

  try {
    // First connect to xiaohong in setup mode
    const connectRes = await fetch(`${API_BASE}/connect`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        device_id: 'xiaohong',
        config: { base_url: 'http://xiaohong.local:8080' },
      }),
    });

    if (!connectRes.ok) {
      throw new Error('无法连接到小鸿，请确保设备已开机并处于配网模式');
    }

    // Scan WiFi networks
    const scanRes = await fetch(`${API_BASE}/scan-wifi`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: 'xiaohong' }),
    });

    if (!scanRes.ok) {
      throw new Error('WiFi 扫描失败');
    }

    const data = await scanRes.json();
    renderWiFiList(data.networks || []);
    goToStep(2);
  } catch (err) {
    alert('扫描失败：' + err.message);
    goToStep('error');
    document.getElementById('error-message').textContent = err.message;
  } finally {
    btn.disabled = false;
    btn.textContent = '开始扫描';
  }
}

function renderWiFiList(networks) {
  const list = document.getElementById('wifi-list');
  list.innerHTML = '';

  networks.forEach(net => {
    const item = document.createElement('div');
    item.className = 'wifi-item';
    item.innerHTML = `
      <span class="wifi-name">${net.name}</span>
      <span class="wifi-signal">${getSignalStrength(net.signal)}</span>
    `;
    item.addEventListener('click', () => {
      document.querySelectorAll('.wifi-item').forEach(i => i.classList.remove('selected'));
      item.classList.add('selected');
      selectedWifi = net.name;
    });
    list.appendChild(item);
  });
}

function getSignalStrength(signal) {
  if (signal > -50) return '强';
  if (signal > -70) return '中';
  return '弱';
}

async function connectWiFi() {
  if (!selectedWifi) {
    alert('请先选择 WiFi 网络');
    return;
  }

  const password = document.getElementById('wifi-password').value;
  if (!password) {
    alert('请输入 WiFi 密码');
    return;
  }

  goToStep(3);

  try {
    // Configure WiFi on device
    const configRes = await fetch(`${API_BASE}/configure-wifi`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        device_id: 'xiaohong',
        ssid: selectedWifi,
        password: password,
      }),
    });

    if (!configRes.ok) {
      const data = await configRes.json();
      throw new Error(data.error || 'WiFi 配置失败');
    }

    // Wait for device to connect to WiFi
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Verify connection by getting device info
    const infoRes = await fetch(`${API_BASE}/info/xiaohong`);
    if (infoRes.ok) {
      goToStep(4);
      await loadDevices();
    } else {
      throw new Error('WiFi 连接失败，请检查密码');
    }
  } catch (err) {
    document.getElementById('error-message').textContent = err.message;
    goToStep('error');
  }
}
