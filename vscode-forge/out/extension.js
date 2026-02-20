"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.activate = activate;
exports.deactivate = deactivate;
const vscode = __importStar(require("vscode"));
const client_1 = require("./client");
const discovery_1 = require("./discovery");
const serversProvider_1 = require("./providers/serversProvider");
const servicesProvider_1 = require("./providers/servicesProvider");
const routesProvider_1 = require("./providers/routesProvider");
const extensionsProvider_1 = require("./providers/extensionsProvider");
const healthProvider_1 = require("./providers/healthProvider");
const metricsPanel_1 = require("./webviews/metricsPanel");
// Map of debugAddr → active client
const clients = new Map();
let activeClient;
function activate(context) {
    // --- Providers ---
    const serversProvider = new serversProvider_1.ServersProvider();
    const servicesProvider = new servicesProvider_1.ServicesProvider();
    const routesProvider = new routesProvider_1.RoutesProvider();
    const extensionsProvider = new extensionsProvider_1.ExtensionsProvider();
    const healthProvider = new healthProvider_1.HealthProvider();
    // --- Tree views ---
    vscode.window.registerTreeDataProvider('forge.servers', serversProvider);
    vscode.window.registerTreeDataProvider('forge.services', servicesProvider);
    vscode.window.registerTreeDataProvider('forge.routes', routesProvider);
    vscode.window.registerTreeDataProvider('forge.extensions', extensionsProvider);
    vscode.window.registerTreeDataProvider('forge.health', healthProvider);
    // --- Discovery ---
    const discovery = new discovery_1.ServerDiscovery();
    function syncClients(entries) {
        // Remove clients for servers that are gone.
        for (const [addr, client] of clients) {
            if (!entries.some(e => e.debug_addr === addr)) {
                client.disconnect();
                clients.delete(addr);
            }
        }
        // Add clients for new servers.
        for (const entry of entries) {
            if (!clients.has(entry.debug_addr)) {
                const client = new client_1.ForgeDebugClient(entry);
                client.connect();
                client.on('stateChanged', () => {
                    serversProvider.updateClients(clients);
                    if (activeClient === client) {
                        // Providers are already subscribed via setClient(); fire comes from there.
                    }
                });
                clients.set(entry.debug_addr, client);
            }
        }
        // Auto-select the first client if none active.
        if (!activeClient || !clients.has(activeClient.entry.debug_addr)) {
            activeClient = clients.values().next().value;
            setActiveClientOnProviders(activeClient);
        }
        serversProvider.updateClients(clients);
    }
    function setActiveClientOnProviders(client) {
        servicesProvider.setClient(client);
        routesProvider.setClient(client);
        extensionsProvider.setClient(client);
        healthProvider.setClient(client);
    }
    discovery.onChange(syncClients);
    discovery.start();
    // Initial sync.
    syncClients(discovery.current());
    // --- Commands ---
    context.subscriptions.push(vscode.commands.registerCommand('forge.refresh', async () => {
        if (activeClient) {
            await activeClient.fetchSnapshot();
        }
        else {
            syncClients(discovery.current());
        }
    }), vscode.commands.registerCommand('forge.openMetrics', () => {
        if (!activeClient) {
            vscode.window.showInformationMessage('No forge server connected. Run forge dev first.');
            return;
        }
        metricsPanel_1.MetricsPanel.show(activeClient, context.extensionUri);
    }), vscode.commands.registerCommand('forge.connect', async () => {
        const addr = await vscode.window.showInputBox({
            prompt: 'Enter forge debug server address (e.g. localhost:9080)',
            placeHolder: 'localhost:9080',
        });
        if (!addr) {
            return;
        }
        const entry = {
            pid: 0, app_name: addr, app_version: '',
            debug_addr: addr, app_addr: '', workspace_dir: '', started_at: '',
        };
        const client = new client_1.ForgeDebugClient(entry);
        client.connect();
        clients.set(addr, client);
        activeClient = client;
        setActiveClientOnProviders(client);
        serversProvider.updateClients(clients);
    }), vscode.commands.registerCommand('forge.copyRoutePath', (item) => {
        const route = item?.route;
        if (route?.path) {
            vscode.env.clipboard.writeText(route.path);
            vscode.window.showInformationMessage(`Copied: ${route.path}`);
        }
    }));
    // --- Cleanup ---
    context.subscriptions.push({
        dispose() {
            discovery.stop();
            for (const client of clients.values()) {
                client.disconnect();
            }
            clients.clear();
        },
    });
}
function deactivate() {
    for (const client of clients.values()) {
        client.disconnect();
    }
    clients.clear();
}
//# sourceMappingURL=extension.js.map