import * as vscode from 'vscode';
import { ForgeDebugClient } from '../client';

class ServiceItem extends vscode.TreeItem {
  constructor(name: string) {
    super(name, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon('symbol-class');
    this.contextValue = 'forgeService';
  }
}

export class ServicesProvider implements vscode.TreeDataProvider<ServiceItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<ServiceItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private activeClient?: ForgeDebugClient;

  setClient(client: ForgeDebugClient | undefined): void {
    this.activeClient = client;
    client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
    this._onDidChangeTreeData.fire(undefined);
  }

  getTreeItem(e: ServiceItem): vscode.TreeItem { return e; }

  getChildren(): ServiceItem[] {
    const services = this.activeClient?.state.snapshot?.services ?? [];
    if (services.length === 0) {
      const empty = new vscode.TreeItem('No services registered');
      empty.iconPath = new vscode.ThemeIcon('info');
      return [empty as ServiceItem];
    }
    return services.map(s => new ServiceItem(s));
  }
}
