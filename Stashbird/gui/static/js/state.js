const state = {
    currentView: 'files',
    currentFolder: null,
    currentPath: '',
    folders: [],
    peers: [],
    files: [],
    pollTimer: null,
    viewMode: localStorage.getItem('stashbird-viewMode') || 'list',
};

