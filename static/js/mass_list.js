// static/js/mass-list.js

// ============================================================
// Configuration
// ============================================================
const API_URL = '/api/plot';
const HISTORY_KEY = 'massListHistory';
const MAX_HISTORY = 10;

// ============================================================
// DOM Elements
// ============================================================
const form = document.getElementById('mass-list-form');
const submitBtn = document.getElementById('submit-btn');
const plotImage = document.getElementById('plot-image');
const plotContainer = document.getElementById('plot-container');
const noPlotMessage = document.getElementById('no-plot-message');
const loadingSpinner = document.getElementById('loading-spinner');
const plotInfo = document.getElementById('plot-info');
const statusBadge = document.getElementById('status-badge');
const downloadBtn = document.getElementById('download-btn');
const viewJsonBtn = document.getElementById('view-json-btn');
const jsonContainer = document.getElementById('json-container');
const jsonResponse = document.getElementById('json-response');
const historyList = document.getElementById('history-list');
const noHistory = document.getElementById('no-history');
const clearHistoryBtn = document.getElementById('clear-history-btn');

// ============================================================
// Form Submission
// ============================================================
form.addEventListener('submit', async function(e) {
    e.preventDefault();
    
    // Collect form data
    const requestData = collectFormData();
    
    // Validate
    if (!requestData.spectra_name) {
        showError('Please enter a spectra name');
        return;
    }
    
    // Show loading
    showLoading();
    
    try {
        const response = await fetch(API_URL, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(requestData)
        });
        
        if (!response.ok) {
            const errorText = await response.text();
            throw new Error(errorText || `HTTP error! status: ${response.status}`);
        }
        
        // Check if response is JSON or image
        const contentType = response.headers.get('content-type');
        
        if (contentType && contentType.includes('application/json')) {
            // Handle JSON response
            const jsonData = await response.json();
            showJSONResponse(jsonData);
        } else {
            // Handle image response
            const blob = await response.blob();
            showImageResponse(blob, requestData);
        }
        
        // Add to history
        addToHistory(requestData, response);
        
    } catch (error) {
        console.error('Error:', error);
        showError(`Failed to generate plot: ${error.message}`);
        updateStatus('error', 'Error');
    } finally {
        hideLoading();
    }
});

// ============================================================
// Collect Form Data
// ============================================================
function collectFormData() {
    let bruttoDict = {};
    try {
        const bruttoText = document.getElementById('brutto-dict').value;
        const parsed = JSON.parse(bruttoText);
        
        // Преобразуем в формат для protobuf
        for (const [key, value] of Object.entries(parsed)) {
            if (Array.isArray(value) && value.length >= 2) {
                // Если массив [min, max]
                bruttoDict[key] = {
                    min: value[0],
                    max: value[1]
                };
            } else if (typeof value === 'object' && value.min !== undefined) {
                // Если уже объект {min, max}
                bruttoDict[key] = {
                    min: value.min,
                    max: value.max
                };
            }
        }
    } catch (e) {
        console.warn('Failed to parse brutto_dict:', e);
        bruttoDict = {};
    }
    
    return {
        spectra_name: document.getElementById('spectra-name').value.trim(),
        spectra_path: document.getElementById('spectra-path').value.trim(),
        low_percentile: parseFloat(document.getElementById('low-percentile').value) || 5,
        high_percentile: parseFloat(document.getElementById('high-percentile').value) || 95,
        rel_error: parseFloat(document.getElementById('rel-error').value) || 0.5,
        charge_max: parseInt(document.getElementById('charge-max').value) || 1,
        brutto_dict: bruttoDict, // ✅ Теперь в формате {min, max}
        protocole: document.getElementById('protocol').value,
        width: parseInt(document.getElementById('width').value) || 10,
        height: parseInt(document.getElementById('height').value) || 6,
        dpi: parseInt(document.getElementById('dpi').value) || 100,
        format: document.getElementById('format').value,
        options: {}
    };
}


// ============================================================
// Display Functions
// ============================================================
function showLoading() {
    loadingSpinner.style.display = 'block';
    noPlotMessage.style.display = 'none';
    plotContainer.style.display = 'none';
    jsonContainer.style.display = 'none';
    submitBtn.disabled = true;
    submitBtn.innerHTML = '<span class="spinner-border spinner-border-sm" role="status"></span> Processing...';
    updateStatus('loading', 'Processing');
}

function hideLoading() {
    loadingSpinner.style.display = 'none';
    submitBtn.disabled = false;
    submitBtn.innerHTML = '<i class="fas fa-play"></i> Generate Mass List';
}

function showImageResponse(blob, requestData) {
    const url = URL.createObjectURL(blob);
    plotImage.src = url;
    plotContainer.style.display = 'block';
    noPlotMessage.style.display = 'none';
    jsonContainer.style.display = 'none';
    
    // Update info
    const sizeKB = (blob.size / 1024).toFixed(2);
    const format = requestData.format.toUpperCase();
    const timestamp = new Date().toLocaleTimeString();
    plotInfo.textContent = `${format} • ${sizeKB} KB • ${timestamp}`;
    
    // Setup download
    downloadBtn.onclick = function() {
        const link = document.createElement('a');
        link.href = url;
        link.download = `mass_list_${requestData.spectra_name}_${Date.now()}.${requestData.format}`;
        link.click();
    };
    
    // Setup JSON view
    viewJsonBtn.onclick = function() {
        jsonContainer.style.display = jsonContainer.style.display === 'none' ? 'block' : 'none';
    };
    
    updateStatus('success', 'Done');
}

function showJSONResponse(data) {
    jsonContainer.style.display = 'block';
    plotContainer.style.display = 'none';
    noPlotMessage.style.display = 'none';
    
    jsonResponse.textContent = JSON.stringify(data, null, 2);
    
    if (data.image_data) {
        // Decode base64 image
        const img = new Image();
        img.src = `data:${data.mime_type};base64,${data.image_data}`;
        img.onload = function() {
            plotImage.src = img.src;
            plotContainer.style.display = 'block';
        };
    }
    
    updateStatus('success', 'Done');
}

function showError(message) {
    noPlotMessage.style.display = 'block';
    noPlotMessage.innerHTML = `
        <i class="fas fa-exclamation-triangle fa-5x text-danger"></i>
        <p class="mt-3 text-danger">${message}</p>
        <button class="btn btn-outline-danger btn-sm" onclick="location.reload()">
            <i class="fas fa-redo"></i> Try Again
        </button>
    `;
    updateStatus('error', 'Error');
}

function updateStatus(type, text) {
    const colors = {
        'loading': 'bg-warning text-dark',
        'success': 'bg-success text-white',
        'error': 'bg-danger text-white',
        'ready': 'bg-light text-dark'
    };
    statusBadge.className = `badge ${colors[type] || colors.ready}`;
    statusBadge.textContent = text;
}

// ============================================================
// History Management
// ============================================================
function addToHistory(requestData, response) {
    const history = getHistory();
    const entry = {
        id: Date.now(),
        timestamp: new Date().toISOString(),
        spectra_name: requestData.spectra_name,
        format: requestData.format,
        status: response.ok ? 'success' : 'error',
        url: response.url
    };
    
    history.unshift(entry);
    if (history.length > MAX_HISTORY) {
        history.pop();
    }
    
    localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
    renderHistory();
}

function getHistory() {
    try {
        return JSON.parse(localStorage.getItem(HISTORY_KEY)) || [];
    } catch {
        return [];
    }
}

function renderHistory() {
    const history = getHistory();
    
    if (history.length === 0) {
        noHistory.style.display = 'block';
        historyList.style.display = 'none';
        clearHistoryBtn.style.display = 'none';
        return;
    }
    
    noHistory.style.display = 'none';
    historyList.style.display = 'block';
    clearHistoryBtn.style.display = 'inline-block';
    
    historyList.innerHTML = history.map(item => `
        <div class="history-item d-flex justify-content-between align-items-center p-2 border-bottom">
            <div>
                <strong>${item.spectra_name}</strong>
                <small class="text-muted ms-2">
                    ${new Date(item.timestamp).toLocaleString()}
                </small>
                <span class="badge bg-secondary ms-2">${item.format}</span>
            </div>
            <span class="badge ${item.status === 'success' ? 'bg-success' : 'bg-danger'}">
                ${item.status}
            </span>
        </div>
    `).join('');
}

clearHistoryBtn.addEventListener('click', function() {
    localStorage.removeItem(HISTORY_KEY);
    renderHistory();
});

// ============================================================
// Quick Examples
// ============================================================
function loadExample(type) {
    const examples = {
        example1: {
            spectra_name: 'Example_Spectrum_001',
            spectra_path: './data/example1.csv',
            low_percentile: 5,
            high_percentile: 95,
            rel_error: 0.5,
            charge_max: 1,
            protocole: 'non_tmds',
            brutto_dict: {
                'C': [4, 50],
                'H': [4, 80],
                'O': [0, 50],
                'N': [0, 3],
                'C_13': [0, 1],
                'S': [0, 3]
            },
            width: 10,
            height: 6,
            dpi: 100,
            format: 'png'
        },
        example2: {
            spectra_name: 'High_Res_Spectrum',
            spectra_path: './data/high_res.csv',
            low_percentile: 1,
            high_percentile: 99,
            rel_error: 0.1,
            charge_max: 2,
            protocole: 'tmds',
            brutto_dict: {
                'C': [10, 100],
                'H': [10, 150],
                'O': [0, 30],
                'N': [0, 5],
                'C_13': [0, 2],
                'S': [0, 2]
            },
            width: 12,
            height: 8,
            dpi: 150,
            format: 'png'
        },
        example3: {
            spectra_name: 'Custom_Brutto_Spectrum',
            spectra_path: './data/custom.csv',
            low_percentile: 10,
            high_percentile: 90,
            rel_error: 0.3,
            charge_max: 1,
            protocole: 'non_tmds',
            brutto_dict: {
                'C': [1, 20],
                'H': [1, 30],
                'O': [0, 10],
                'N': [0, 2],
                'S': [0, 1]
            },
            width: 10,
            height: 6,
            dpi: 100,
            format: 'svg'
        }
    };
    
    const data = examples[type];
    if (!data) return;
    
    // Fill form
    document.getElementById('spectra-name').value = data.spectra_name;
    document.getElementById('spectra-path').value = data.spectra_path;
    document.getElementById('low-percentile').value = data.low_percentile;
    document.getElementById('high-percentile').value = data.high_percentile;
    document.getElementById('rel-error').value = data.rel_error;
    document.getElementById('charge-max').value = data.charge_max;
    document.getElementById('protocol').value = data.protocole;
    document.getElementById('brutto-dict').value = JSON.stringify(data.brutto_dict, null, 4);
    document.getElementById('width').value = data.width;
    document.getElementById('height').value = data.height;
    document.getElementById('dpi').value = data.dpi;
    document.getElementById('format').value = data.format;
    
    // Auto-submit
    setTimeout(() => form.dispatchEvent(new Event('submit')), 300);
}

// ============================================================
// Utility Functions
// ============================================================
function copyJSON() {
    const text = jsonResponse.textContent;
    navigator.clipboard.writeText(text).then(() => {
        const btn = document.querySelector('#json-container .btn-light');
        const originalText = btn.innerHTML;
        btn.innerHTML = '<i class="fas fa-check"></i> Copied!';
        setTimeout(() => btn.innerHTML = originalText, 2000);
    }).catch(() => {
        // Fallback
        const textarea = document.createElement('textarea');
        textarea.value = text;
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand('copy');
        document.body.removeChild(textarea);
    });
}

// ============================================================
// Init
// ============================================================
document.addEventListener('DOMContentLoaded', function() {
    renderHistory();
    updateStatus('ready', 'Ready');
    
    // Auto-submit with default example on first load
    if (!localStorage.getItem('exampleShown')) {
        localStorage.setItem('exampleShown', 'true');
        setTimeout(() => loadExample('example1'), 500);
    }
});

// ============================================================
// Keyboard Shortcuts
// ============================================================
document.addEventListener('keydown', function(e) {
    // Ctrl+Enter to submit
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
        e.preventDefault();
        form.dispatchEvent(new Event('submit'));
    }
});