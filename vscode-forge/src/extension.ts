import * as vscode from 'vscode';
import { ForgeDebugClient } from './client';
import { ServerDiscovery } from './discovery';
import { ServersProvider } from './providers/serversProvider';
import { ServicesProvider } from './providers/servicesProvider';
import { RoutesProvider } from './providers/routesProvider';
import { ExtensionsProvider } from './providers/extensionsProvider';
import { HealthProvider } from './providers/healthProvider';
import { MetricsPanel } from './webviews/metricsPanel';
import { ServerEntry } from './types';

// Map of debugAddr → active client
const clients = new Map<string, ForgeDebugClient>();
let activeClient: ForgeDebugClient | undefined;

export function activate(context: vscode.ExtensionContext): void {
  // --- Providers ---
  const serversProvider    = new ServersProvider();
  const servicesProvider   = new ServicesProvider();
  const routesProvider     = new RoutesProvider();
  const extensionsProvider = new ExtensionsProvider();
  const healthProvider     = new HealthProvider();

  // --- Tree views ---
  vscode.window.registerTreeDataProvider('forge.servers',    serversProvider);
  vscode.window.registerTreeDataProvider('forge.services',   servicesProvider);
  vscode.window.registerTreeDataProvider('forge.routes',     routesProvider);
  vscode.window.registerTreeDataProvider('forge.extensions', extensionsProvider);
  vscode.window.registerTreeDataProvider('forge.health',     healthProvider);

  // --- Discovery ---
  const discovery = new ServerDiscovery();

  function syncClients(entries: ServerEntry[]): void {
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
        const client = new ForgeDebugClient(entry);
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
      activeClient = clients.values().next().value as ForgeDebugClient | undefined;
      setActiveClientOnProviders(activeClient);
    }

    serversProvider.updateClients(clients);
  }

  function setActiveClientOnProviders(client: ForgeDebugClient | undefined): void {
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
  context.subscriptions.push(
    vscode.commands.registerCommand('forge.refresh', async () => {
      if (activeClient) {
        await activeClient.fetchSnapshot();
      } else {
        syncClients(discovery.current());
      }
    }),

    vscode.commands.registerCommand('forge.openMetrics', () => {
      if (!activeClient) {
        vscode.window.showInformationMessage('No forge server connected. Run forge dev first.');
        return;
      }
      MetricsPanel.show(activeClient, context.extensionUri);
    }),

    vscode.commands.registerCommand('forge.connect', async () => {
      const addr = await vscode.window.showInputBox({
        prompt: 'Enter forge debug server address (e.g. localhost:9080)',
        placeHolder: 'localhost:9080',
      });
      if (!addr) { return; }
      const entry: ServerEntry = {
        pid: 0, app_name: addr, app_version: '',
        debug_addr: addr, app_addr: '', workspace_dir: '', started_at: '',
      };
      const client = new ForgeDebugClient(entry);
      client.connect();
      clients.set(addr, client);
      activeClient = client;
      setActiveClientOnProviders(client);
      serversProvider.updateClients(clients);
    }),

    vscode.commands.registerCommand('forge.copyRoutePath', (item: unknown) => {
      const route = (item as { route?: { path?: string } })?.route;
      if (route?.path) {
        vscode.env.clipboard.writeText(route.path);
        vscode.window.showInformationMessage(`Copied: ${route.path}`);
      }
    }),
  );

  // --- Cleanup ---
  context.subscriptions.push({
    dispose(): void {
      discovery.stop();
      for (const client of clients.values()) { client.disconnect(); }
      clients.clear();
    },
  });
}

export function deactivate(): void {
  for (const client of clients.values()) { client.disconnect(); }
  clients.clear();
}
