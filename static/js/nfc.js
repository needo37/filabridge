// FilaBridge Dashboard - NFC Management Functions

// NFC Management Functions
function switchNfcTab(tabName, clickedElement) {
    console.log('Switching to NFC tab:', tabName);
    // Hide all NFC tab contents
    document.querySelectorAll('.nfc-tab-content').forEach(tab => {
        tab.classList.remove('active');
    });
    
    // Remove active class from all NFC tabs
    document.querySelectorAll('.nfc-tab').forEach(tab => {
        tab.classList.remove('active');
    });
    
    // Show selected tab content
    document.getElementById(tabName + '-tab').classList.add('active');
    
    // Add active class to clicked tab
    if (clickedElement) {
        clickedElement.classList.add('active');
    } else {
        // Fallback: find the tab button by onclick attribute
        const tabButtons = document.querySelectorAll('.nfc-tab');
        tabButtons.forEach(btn => {
            if (btn.getAttribute('onclick').includes(tabName)) {
                btn.classList.add('active');
            }
        });
    }
    
    // Load data for specific tabs
    if (tabName === 'spool-tags') {
        console.log('Loading spool tags...');
        loadSpoolTags();
    } else if (tabName === 'filament-tags') {
        console.log('Loading filament tags...');
        loadFilamentTags();
    } else if (tabName === 'location-tags') {
        console.log('Loading location tags...');
        loadLocationTags();
    }
}

async function loadNfcData() {
    await loadSpoolTags();
    await loadFilamentTags();
    await loadLocationTags();
}

async function loadSpoolTags() {
    try {
        console.log('Loading spool tags...');
        const response = await fetch('/api/nfc/urls');
        const data = await response.json();
        console.log('NFC URLs data:', data);
        
        const container = document.getElementById('spool-list-container');
        const spoolUrls = data.urls.filter(url => url.type === 'spool');
        console.log('Spool URLs:', spoolUrls);
        
        if (spoolUrls.length === 0) {
            container.innerHTML = '<p>No spools available</p>';
            return;
        }
        
        container.innerHTML = '';
        
        spoolUrls.forEach(url => {
            const item = document.createElement('div');
            item.className = 'nfc-list-item';
            item.dataset.value = url.spool_id;
            item.dataset.color = url.color_hex;
            item.dataset.url = url.url;
            item.dataset.qr = url.qr_code_base64;
            
            const colorHex = url.color_hex || '#ccc';
            item.innerHTML = `
                <div class="color-swatch" style="background-color: ${colorHex}"></div>
                <div class="item-info">
                    <div class="item-name">[${url.spool_id}] ${url.spool_name}</div>
                    <div class="item-details">${url.material} - ${url.brand}${url.remaining_weight != null ? ` - ${Math.round(url.remaining_weight)}g remaining` : ''}</div>
                </div>
            `;
            
            // Add click handler
            item.addEventListener('click', () => {
                // Remove selected class from all items
                container.querySelectorAll('.nfc-list-item').forEach(i => i.classList.remove('selected'));
                // Add selected class to clicked item
                item.classList.add('selected');
                // Show QR code
                displaySpoolQR(url);
            });
            
            container.appendChild(item);
        });
        
        // Initialize search functionality
        initializeSpoolSearch(spoolUrls);
        
    } catch (error) {
        console.error('Error loading spool tags:', error);
        document.getElementById('spool-list-container').innerHTML = '<p>Error loading spools</p>';
    }
}

async function loadFilamentTags() {
    try {
        console.log('Loading filament tags...');
        const response = await fetch('/api/nfc/urls');
        const data = await response.json();
        console.log('NFC URLs data:', data);
        
        const container = document.getElementById('filament-list-container');
        const filamentUrls = data.urls.filter(url => url.type === 'filament');
        console.log('Filament URLs:', filamentUrls);
        
        if (filamentUrls.length === 0) {
            container.innerHTML = '<p>No filaments available</p>';
            return;
        }
        
        container.innerHTML = '';
        
        filamentUrls.forEach(url => {
            const item = document.createElement('div');
            item.className = 'nfc-list-item';
            item.dataset.value = url.filament_id;
            item.dataset.color = url.color_hex;
            item.dataset.url = url.url;
            item.dataset.qr = url.qr_code_base64;
            
            const colorHex = url.color_hex || '#ccc';
            item.innerHTML = `
                <div class="color-swatch" style="background-color: ${colorHex}"></div>
                <div class="item-info">
                    <div class="item-name">${url.filament_name}</div>
                    <div class="item-details">${url.material} - ${url.brand}</div>
                </div>
            `;
            
            // Add click handler
            item.addEventListener('click', () => {
                // Remove selected class from all items
                container.querySelectorAll('.nfc-list-item').forEach(i => i.classList.remove('selected'));
                // Add selected class to clicked item
                item.classList.add('selected');
                // Show QR code
                displayFilamentQR(url);
            });
            
            container.appendChild(item);
        });
        
        // Initialize search functionality
        initializeFilamentSearch(filamentUrls);
        
    } catch (error) {
        console.error('Error loading filament tags:', error);
        document.getElementById('filament-list-container').innerHTML = '<p>Error loading filaments</p>';
    }
}

async function loadLocationTags() {
    try {
        console.log('Loading location tags...');
        const response = await fetch('/api/nfc/urls');
        const data = await response.json();
        console.log('NFC URLs data:', data);
        
        const container = document.getElementById('location-list-container');
        const locationUrls = data.urls.filter(url => url.type === 'location');
        console.log('Location URLs:', locationUrls);
        
        if (locationUrls.length === 0) {
            container.innerHTML = '<p>No locations available</p>';
            return;
        }
        
        container.innerHTML = '';
        
        locationUrls.forEach(url => {
            const item = document.createElement('div');
            item.className = 'nfc-list-item';
            item.dataset.value = url.display_name;
            item.dataset.url = url.url;
            item.dataset.qr = url.qr_code_base64;
            
            // Determine icon based on location type
            let icon = '📦'; // Storage icon for storage locations
            let iconHtml = icon;
            if (url.location_type === 'printer') {
                iconHtml = '<img src="/static/images/3d-printer-icon.png" alt="3D Printer" style="width: 20px; height: 20px;">';
            }
            
            item.innerHTML = `
                <div class="location-icon">${iconHtml}</div>
                <div class="item-info">
                    <div class="item-name">${url.display_name}</div>
                </div>
                <div class="location-actions">
                    ${renderLocationActions(url)}
                </div>
            `;
            
            // Add click handler
            item.addEventListener('click', (e) => {
                // Don't trigger if clicking on action buttons
                if (e.target.closest('.location-actions')) {
                    return;
                }
                
                // Remove selected class from all items
                container.querySelectorAll('.nfc-list-item').forEach(i => i.classList.remove('selected'));
                // Add selected class to clicked item
                item.classList.add('selected');
                // Show QR code
                displayLocationQR({
                    name: url.display_name,
                    is_printer_location: url.location_type === 'printer',
                    url: url.url,
                    qr_code_base64: url.qr_code_base64,
                    description: url.description || ''
                });
            });
            
            container.appendChild(item);
        });
        
        // Initialize search functionality
        initializeLocationSearch(locationUrls);
        
    } catch (error) {
        console.error('Error loading location tags:', error);
        document.getElementById('location-list-container').innerHTML = '<p>Error loading locations</p>';
    }
}

// Render inline actions for FilaBridge-managed locations
function renderLocationActions(url) {
    try {
        // Only show actions for non-printer locations (printer locations are virtual)
        if (url.location_type === 'printer') return '';
        
        const nameAttr = (url.display_name || '').replace(/'/g, "\\'").replace(/"/g, '&quot;');
        
        // Show rename for all FilaBridge locations
        let actions = `<a href="javascript:void(0)" onclick="event.preventDefault(); event.stopPropagation(); renameLocation('${nameAttr}');">Rename</a>`;
        
        // Show delete for local-only locations (not synced to Spoolman)
        if (url.is_local_only) {
            actions += ` • <a href="javascript:void(0)" onclick="event.preventDefault(); event.stopPropagation(); deleteLocation('${nameAttr}');" style="color: #ff6b6b;">Delete</a>`;
        } else {
            actions += ` <span style="color: #666; font-size: 0.9em;">(Synced to Spoolman)</span>`;
        }
        
        return `<span style="margin-left:8px; font-weight:normal;">${actions}</span>`;
    } catch (error) {
        console.error('Error rendering location actions:', error);
        return '';
    }
}

// Copy URL to clipboard
async function copyUrlToClipboard(urlElementId, buttonElement) {
    try {
        const urlElement = document.getElementById(urlElementId);
        const url = urlElement.textContent;
        
        if (!url) {
            console.warn('No URL to copy');
            return;
        }
        
        // Use the Clipboard API
        await navigator.clipboard.writeText(url);
        
        // Visual feedback - change icon temporarily
        const icon = buttonElement.querySelector('.nfc-copy-icon');
        const originalIcon = icon.textContent;
        icon.textContent = '✓';
        buttonElement.style.background = 'rgba(76, 175, 80, 0.3)';
        
        // Reset after 2 seconds
        setTimeout(() => {
            icon.textContent = originalIcon;
            buttonElement.style.background = '';
        }, 2000);
        
    } catch (err) {
        console.error('Failed to copy URL:', err);
        // Fallback for older browsers
        const urlElement = document.getElementById(urlElementId);
        const url = urlElement.textContent;
        const textArea = document.createElement('textarea');
        textArea.value = url;
        textArea.style.position = 'fixed';
        textArea.style.opacity = '0';
        document.body.appendChild(textArea);
        textArea.select();
        try {
            document.execCommand('copy');
            const icon = buttonElement.querySelector('.nfc-copy-icon');
            const originalIcon = icon.textContent;
            icon.textContent = '✓';
            buttonElement.style.background = 'rgba(76, 175, 80, 0.3)';
            setTimeout(() => {
                icon.textContent = originalIcon;
                buttonElement.style.background = '';
            }, 2000);
        } catch (fallbackErr) {
            console.error('Fallback copy failed:', fallbackErr);
            alert('Failed to copy URL. Please copy manually.');
        }
        document.body.removeChild(textArea);
    }
}

// Display QR code for selected spool
function displaySpoolQR(spoolData) {
    console.log('Displaying spool QR:', spoolData);
    
    // Hide no-selection message
    document.getElementById('spool-no-selection').style.display = 'none';
    
    // Show QR display
    const display = document.getElementById('spool-qr-display');
    display.style.display = 'block';
    
    // Update content
    document.getElementById('spool-selected-name').textContent = `[${spoolData.spool_id}] ${spoolData.spool_name}`;
    document.getElementById('spool-selected-details').innerHTML = ``;
    document.getElementById('spool-qr-large').src = `data:image/png;base64,${spoolData.qr_code_base64}`;
    document.getElementById('spool-url-text').textContent = spoolData.url;
}

// Display QR code for selected filament
function displayFilamentQR(filamentData) {
    console.log('Displaying filament QR:', filamentData);
    
    // Hide no-selection message
    document.getElementById('filament-no-selection').style.display = 'none';
    
    // Show QR display
    const display = document.getElementById('filament-qr-display');
    display.style.display = 'block';
    
    // Update content
    document.getElementById('filament-selected-name').textContent = filamentData.filament_name;
    document.getElementById('filament-selected-details').innerHTML = ``;
    document.getElementById('filament-qr-large').src = `data:image/png;base64,${filamentData.qr_code_base64}`;
    document.getElementById('filament-url-text').textContent = filamentData.url;
}

// Display QR code for selected location
function displayLocationQR(locationData) {
    console.log('Displaying location QR:', locationData);
    
    // Hide no-selection message
    document.getElementById('location-no-selection').style.display = 'none';
    
    // Show QR display
    const display = document.getElementById('location-qr-display');
    display.style.display = 'block';
    
    // Update content
    document.getElementById('location-selected-name').textContent = locationData.name;
    document.getElementById('location-selected-details').innerHTML = `
        <strong>Type:</strong> ${locationData.is_printer_location ? 'Printer Location' : 'Custom Location'}<br>
        ${locationData.description ? `<strong>Description:</strong> ${locationData.description}<br>` : ''}
    `;
    document.getElementById('location-qr-large').src = `data:image/png;base64,${locationData.qr_code_base64}`;
    document.getElementById('location-url-text').textContent = locationData.url;
}

// Initialize search functionality for spools
function initializeSpoolSearch(spoolUrls) {
    const searchInput = document.getElementById('spool-search');
    const container = document.getElementById('spool-list-container');
    
    searchInput.addEventListener('input', (e) => {
        const searchTerm = e.target.value.toLowerCase();
        const items = container.querySelectorAll('.nfc-list-item');
        
        items.forEach(item => {
            const name = item.querySelector('.item-name').textContent.toLowerCase();
            const details = item.querySelector('.item-details').textContent.toLowerCase();
            
            if (name.includes(searchTerm) || details.includes(searchTerm)) {
                item.style.display = 'flex';
            } else {
                item.style.display = 'none';
            }
        });
    });
}

// Initialize search functionality for filaments
function initializeFilamentSearch(filamentUrls) {
    const searchInput = document.getElementById('filament-search');
    const container = document.getElementById('filament-list-container');
    
    searchInput.addEventListener('input', (e) => {
        const searchTerm = e.target.value.toLowerCase();
        const items = container.querySelectorAll('.nfc-list-item');
        
        items.forEach(item => {
            const name = item.querySelector('.item-name').textContent.toLowerCase();
            const details = item.querySelector('.item-details').textContent.toLowerCase();
            
            if (name.includes(searchTerm) || details.includes(searchTerm)) {
                item.style.display = 'flex';
            } else {
                item.style.display = 'none';
            }
        });
    });
}

// Initialize search functionality for locations
function initializeLocationSearch(locationUrls) {
    const searchInput = document.getElementById('location-search');
    const container = document.getElementById('location-list-container');
    
    searchInput.addEventListener('input', (e) => {
        const searchTerm = e.target.value.toLowerCase();
        const items = container.querySelectorAll('.nfc-list-item');
        
        items.forEach(item => {
            const name = item.querySelector('.item-name').textContent.toLowerCase();
            const details = item.querySelector('.item-details').textContent.toLowerCase();
            
            if (name.includes(searchTerm) || details.includes(searchTerm)) {
                item.style.display = 'flex';
            } else {
                item.style.display = 'none';
            }
        });
    });
}

// Location Management Functions
async function addLocation() {
    const nameEl = document.getElementById('newLocationName');
    const name = (nameEl.value || '').trim();
    if (!name) { alert('Please enter a location name'); return; }
    try {
        const url = apiUrl('/api/locations');
        console.log('POST', url, { name });
        const res = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            mode: 'same-origin', credentials: 'same-origin',
            body: JSON.stringify({ name })
        });
        if (!res.ok) throw new Error(await res.text());
        nameEl.value = '';
        await loadLocationTags();
    } catch (e) { console.error(e); alert(e.message || 'Network error'); }
}

async function renameLocation(currentName) {
    const newName = prompt('Rename location', currentName || '');
    if (!newName || newName.trim() === '' || newName === currentName) return;
    try {
        const url = apiUrl(`/api/locations/${encodeURIComponent(currentName)}`);
        console.log('PUT', url, { name: newName.trim() });
        const res = await fetch(url, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            mode: 'same-origin', credentials: 'same-origin',
            body: JSON.stringify({ name: newName.trim() })
        });
        if (!res.ok) {
            const errorText = await res.text();
            throw new Error(errorText);
        }
        const result = await res.json();
        console.log('Rename result:', result);
        await loadLocationTags();
        if (result.message) {
            alert(result.message);
        }
    } catch (e) { 
        console.error('Rename error:', e); 
        alert(e.message || 'Network error'); 
    }
}

async function deleteLocation(name) {
    try {
        console.log('deleteLocation called with name:', name);
        const url = apiUrl(`/api/locations/${encodeURIComponent(name)}`);
        console.log('DELETE', url);
        const res = await fetch(url, {
            method: 'DELETE',
            headers: { 'Accept': 'application/json' },
            mode: 'same-origin', credentials: 'same-origin'
        });
        if (!res.ok) {
            const errorText = await res.text();
            throw new Error(errorText);
        }
        const result = await res.json();
        console.log('Delete result:', result);
        await loadLocationTags();
    } catch (e) { 
        console.error('Delete error:', e); 
        alert(e.message || 'Network error'); 
    }
}


// QR Code Modal Functions
function showQrCode(url, title, qrCodeBase64) {
    // Create modal if it doesn't exist
    let modal = document.getElementById('nfc-qr-modal');
    if (!modal) {
        modal = document.createElement('div');
        modal.id = 'nfc-qr-modal';
        modal.className = 'nfc-qr-modal';
        modal.innerHTML = `
            <div class="nfc-qr-content">
                <h3 id="qr-title"></h3>
                <div class="nfc-qr-modal-code" id="qr-code"></div>
                <div class="nfc-url" id="qr-url"></div>
                <div class="nfc-instructions">
                    <h4>Instructions:</h4>
                    <ol>
                        <li>Open NFC Tools Pro on your phone</li>
                        <li>Scan this QR code to copy the URL</li>
                        <li>Write the URL to your NFC tag</li>
                    </ol>
                </div>
                <button class="btn" onclick="closeQrModal()">Close</button>
            </div>
        `;
        document.body.appendChild(modal);
    }
    
    // Update modal content
    document.getElementById('qr-title').textContent = title;
    document.getElementById('qr-url').textContent = url;
    
    // Display real QR code or placeholder
    const qrCodeDiv = document.getElementById('qr-code');
    if (qrCodeBase64 && qrCodeBase64 !== '') {
        qrCodeDiv.innerHTML = `<img src="data:image/png;base64,${qrCodeBase64}" alt="QR Code" style="width: 256px; height: 256px; border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.15);">`;
    } else {
        // Fallback placeholder if QR code generation failed
        qrCodeDiv.innerHTML = `<div style="width: 256px; height: 256px; background: #f0f0f0; display: flex; align-items: center; justify-content: center; border: 2px dashed #ccc; border-radius: 8px;">
            <div style="text-align: center;">
                <div style="font-size: 48px; margin-bottom: 10px;">⚠️</div>
                <div style="font-size: 12px; color: #666;">QR Code Error</div>
                <div style="font-size: 10px; color: #999;">Copy URL manually</div>
            </div>
        </div>`;
    }
    
    // Show modal
    modal.style.display = 'block';
}

function closeQrModal() {
    const modal = document.getElementById('nfc-qr-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Close modal when clicking outside
window.onclick = function(event) {
    const modal = document.getElementById('nfc-qr-modal');
    if (event.target === modal) {
        closeQrModal();
    }
}

