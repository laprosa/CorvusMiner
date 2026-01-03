// Main JavaScript functionality for CorvusMiner Panel

document.addEventListener('DOMContentLoaded', function() {
    console.log('CorvusMiner Panel initialized');
    
    // Initialize any dynamic elements
    initializeElements();
});

function initializeElements() {
    // Auto-refresh miners data (example)
    // setInterval(refreshMinersData, 30000); // Refresh every 30 seconds
}

async function refreshMinersData() {
    try {
        const response = await fetch('/api/miners');
        const miners = await response.json();
        updateMinersTable(miners);
    } catch (error) {
        console.error('Error refreshing miners data:', error);
    }
}

function updateMinersTable(miners) {
    const tbody = document.getElementById('minersTableBody');
    if (!tbody) return;
    
    tbody.innerHTML = miners.map(miner => `
        <tr data-miner-id="${miner.id}">
            <td>${miner.name}</td>
            <td>${miner.ip}</td>
            <td>${miner.port}</td>
            <td><span class="status ${miner.status}">${miner.status}</span></td>
            <td>${miner.hashrate}</td>
            <td>${miner.shares}</td>
            <td>
                <button class="btn btn-danger btn-sm delete-miner" data-id="${miner.id}">Delete</button>
            </td>
        </tr>
    `).join('');
    
    // Reattach delete handlers
    document.querySelectorAll('.delete-miner').forEach(btn => {
        btn.addEventListener('click', handleDeleteMiner);
    });
}

async function handleDeleteMiner(e) {
    const minerId = e.target.getAttribute('data-id');
    if (confirm('Are you sure you want to delete this miner?')) {
        try {
            const response = await fetch('/api/miners/delete', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/x-www-form-urlencoded'
                },
                body: 'id=' + minerId
            });
            
            if (response.ok) {
                alert('Miner deleted successfully!');
                refreshMinersData();
            } else {
                alert('Error deleting miner');
            }
        } catch (error) {
            alert('Error: ' + error.message);
        }
    }
}

// Utility function for API calls
async function apiCall(endpoint, method = 'GET', data = null) {
    const options = {
        method: method,
        headers: {
            'Content-Type': 'application/json'
        }
    };
    
    if (data) {
        options.body = JSON.stringify(data);
    }
    
    try {
        const response = await fetch(endpoint, options);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('API call error:', error);
        throw error;
    }
}
