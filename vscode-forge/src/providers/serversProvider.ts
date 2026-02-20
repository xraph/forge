import * as vscode from 'vscode';
import { ServerEntry } from '../types';
import { ForgeDebugClient } from '../client';

export class ServerItem extends vscode.TreeItem {
  constructor(
    public readonly entry: ServerEntry,
    public readonly client: ForgeDebugClient
  ) {
    const connected = client.state.connected;
    super(entry.app_name, vscode.TreeItemCollapsibleState.None);
    this.description = `${entry.app_version} · ${entry.debug_addr}`;
    this.tooltip = `PID: ${entry.pid}\nApp: ${entry.app_addr}\nDebug: ${entry.debug_addr}\nWorkspace: ${entry.workspace_dir}`;
    this.iconPath = new vscode.ThemeIcon(connected ? 'circle-filled' : 'circle-outline');
    this.contextValue = 'forgeServer';
  }
}

export class ServersProvider implements vscode.TreeDataProvider<ServerItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<ServerItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private items: ServerItem[] = [];

  updateClients(clients: Map<string, ForgeDebugClient>): void {
    this.items = Array.from(clients.values()).map(
      c => new ServerItem(c.entry, c)
    );
    this._onDidChangeTreeData.fire(undefined);
  }

  getTreeItem(element: ServerItem): vscode.TreeItem { return element; }

  getChildren(): ServerItem[] { return this.items; }
}
