// FilaBridge Dashboard - Printer Management Functions

// Printer Management Functions
function loadPrinters() {
    fetch('/api/printers')
        .then(response => response.json())
        .then(data => {
            const printerList = document.getElementById('printer-list');
            printerList.innerHTML = '';
            
            if (data.printers && Object.keys(data.printers).length > 0) {
                for (const [printerId, printer] of Object.entries(data.printers)) {
                    if (printerId === 'no_printers') continue;
                    
                    const printerCard = document.createElement('div');
                    printerCard.className = 'printer-card';
                    printerCard.innerHTML = `
                        <h3>${printer.name || 'Unknown Printer'}</h3>
                        <div class="printer-info">
                            <div><strong>Model:</strong> ${printer.model || 'Unknown'} (${printer.toolheads || 1} toolhead${printer.toolheads > 1 ? 's' : ''})</div>
                            <div><strong>IP:</strong> ${printer.ip_address || 'Not configured'}</div>
                            <div><strong>API Key:</strong> ${printer.api_key ? '••••••••' : 'Not configured'}</div>
                        </div>
                        <div class="printer-actions">
                            <button class="btn btn-small" onclick="editPrinter('${printerId}')">✏️ Edit</button>
                            <button class="btn btn-small btn-danger" onclick="deletePrinter('${printerId}')">🗑️ Delete</button>
                        </div>
                    `;
                    printerList.appendChild(printerCard);
                }
            } else {
                printerList.innerHTML = '<div class="printer-card"><p>No printers configured. Click "Add Printer" to get started.</p></div>';
            }
        })
        .catch(error => {
            console.error('Error loading printers:', error);
            document.getElementById('printer-list').innerHTML = '<div class="printer-card"><p>Error loading printers. Please refresh the page.</p></div>';
        });
}

function showAddPrinterForm() {
    document.getElementById('addPrinterModal').style.display = 'block';
    document.getElementById('addPrinterForm').reset();
    
    // Reset button state AFTER form reset with a fresh query
    // Use setTimeout to ensure DOM is updated
    setTimeout(() => {
        const submitButton = document.querySelector('#addPrinterForm button[type="submit"]');
        if (submitButton) {
            submitButton.disabled = false;
            submitButton.textContent = 'Add Printer';
        }
    }, 0);
}

function closeAddPrinterModal() {
    document.getElementById('addPrinterModal').style.display = 'none';
    
    // Ensure button state is reset when modal is closed
    const submitButton = document.querySelector('#addPrinterForm button[type="submit"]');
    if (submitButton) {
        submitButton.disabled = false;
        submitButton.textContent = 'Add Printer';
    }
}

function closeEditPrinterModal() {
    document.getElementById('editPrinterModal').style.display = 'none';
}

// Close modal when clicking outside of it
window.onclick = function(event) {
    const addModal = document.getElementById('addPrinterModal');
    const editModal = document.getElementById('editPrinterModal');
    if (event.target == addModal) {
        closeAddPrinterModal();
    } else if (event.target == editModal) {
        closeEditPrinterModal();
    }
}

function addPrinter(printerConfig) {
    return fetch('/api/printers', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(printerConfig)
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            throw new Error(data.error);
        }
        return data;
    });
}

// Handle form submission
document.getElementById('addPrinterForm').addEventListener('submit', function(e) {
    e.preventDefault();
    
    // Check if form is valid before proceeding
    if (!this.checkValidity()) {
        // Form has validation errors, don't change button state
        return;
    }
    
    const formData = new FormData(this);
    const name = formData.get('name');
    const ipAddress = formData.get('ip_address');
    const apiKey = formData.get('api_key');
    const toolheads = parseInt(formData.get('toolheads'));
    
    // Show loading state
    const submitButton = this.querySelector('button[type="submit"]');
    const originalText = submitButton.textContent;
    submitButton.disabled = true;
    submitButton.textContent = 'Detecting model...';
    
    // First detect printer model, then add printer
    detectModelAndAddPrinter(name, ipAddress, apiKey, toolheads, submitButton, originalText);
});

// Handle edit form submission
document.getElementById('editPrinterForm').addEventListener('submit', function(e) {
    e.preventDefault();
    
    const formData = new FormData(this);
    const printerId = formData.get('printerId');
    const name = formData.get('name');
    const model = formData.get('model');
    const ipAddress = formData.get('ip_address');
    const apiKey = formData.get('api_key');
    const toolheads = parseInt(formData.get('toolheads'));
    
    // Show loading state
    const submitButton = this.querySelector('button[type="submit"]');
    const originalText = submitButton.textContent;
    submitButton.disabled = true;
    submitButton.textContent = 'Updating...';
    
    // Create printer config
    const printerConfig = {
        name: name,
        model: model,
        ip_address: ipAddress,
        api_key: apiKey,
        toolheads: toolheads
    };
    
    // Update the printer
    fetch(`/api/printers/${printerId}`, {
        method: 'PUT',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(printerConfig)
    })
    .then(response => response.json())
    .then(data => {
        if (data.error) {
            throw new Error(data.error);
        }
        
        // Success - close modal and refresh
        closeEditPrinterModal();
        loadPrinters();
    })
    .catch(error => {
        // Reset button state
        submitButton.disabled = false;
        submitButton.textContent = originalText;
        alert('Error updating printer: ' + error.message);
    });
});

function detectModelAndAddPrinter(name, ipAddress, apiKey, toolheads, submitButton, originalText) {
    // Detect printer model only
    fetch('/api/detect_printer', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
            ip_address: ipAddress,
            api_key: apiKey
        })
    })
    .then(response => response.json())
    .then(data => {
        // Check if there was an error (but still proceed if detection failed)
        if (data.error) {
            throw new Error(data.error);
        }
        
        // Show warning if detection failed but still proceed
        if (!data.detected && data.warning) {
            console.warn('Printer detection failed:', data.warning);
        }
        
        // Create printer config with detected model (or "Unknown" if detection failed)
        const printerConfig = {
            name: name,
            model: data.model || "Unknown",
            ip_address: ipAddress,
            api_key: apiKey,
            toolheads: toolheads
        };
        
        // Add the printer
        return addPrinter(printerConfig);
    })
    .then(() => {
        // Success - close modal and refresh
        closeAddPrinterModal();
        loadPrinters();
    })
    .catch(error => {
        // Reset button state
        submitButton.disabled = false;
        submitButton.textContent = originalText;
        alert('Error adding printer: ' + error.message);
    });
}

function editPrinter(printerId) {
    // Get the current printer data
    fetch('/api/printers')
        .then(response => response.json())
        .then(data => {
            const printer = data.printers[printerId];
            if (!printer) {
                alert('Printer not found');
                return;
            }
            
            // Populate the edit form with current data
            document.getElementById('editPrinterId').value = printerId;
            document.getElementById('editPrinterName').value = printer.name || '';
            document.getElementById('editPrinterModel').value = printer.model || '';
            document.getElementById('editPrinterIP').value = printer.ip_address || '';
            document.getElementById('editPrinterAPIKey').value = printer.api_key || '';
            document.getElementById('editPrinterToolheads').value = printer.toolheads || 1;
            
            // Show the edit modal
            document.getElementById('editPrinterModal').style.display = 'block';
        })
        .catch(error => {
            console.error('Error loading printer data:', error);
            alert('Error loading printer data');
        });
}

function deletePrinter(printerId) {
    if (confirm('Are you sure you want to delete this printer?')) {
        fetch(`/api/printers/${printerId}`, {
            method: 'DELETE'
        })
        .then(response => response.json())
        .then(data => {
            if (data.error) {
                alert('Error deleting printer: ' + data.error);
            } else {
                alert('Printer deleted successfully!');
                loadPrinters();
            }
        })
        .catch(error => {
            alert('Error deleting printer: ' + error.message);
        });
    }
}
