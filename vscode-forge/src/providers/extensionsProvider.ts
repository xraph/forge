import * as vscode from 'vscode';
import { DebugExtInfo } from '../types';
import { ForgeDebugClient } from '../client';

class ExtItem extends vscode.TreeItem {
  constructor(ext: DebugExtInfo) {
    super(ext.name, vscode.TreeItemCollapsibleState.None);
    this.description = ext.version;
    this.tooltip = [
      ext.description,
      ext.dependencies.length ? `Deps: ${ext.dependencies.join(', ')}` : '',
      `Healthy: ${ext.healthy}`,
    ].filter(Boolean).join('\n');
    this.iconPath = new vscode.ThemeIcon(
      ext.healthy ? 'extensions' : 'extensions-warning',
      ext.healthy
        ? new vscode.ThemeColor('charts.green')
        : new vscode.ThemeColor('charts.red')
    );
    this.contextValue = 'forgeExtension';
  }
}

export class ExtensionsProvider implements vscode.TreeDataProvider<ExtItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<ExtItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private activeClient?: ForgeDebugClient;

  setClient(client: ForgeDebugClient | undefined): void {
    this.activeClient = client;
    client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
    this._onDidChangeTreeData.fire(undefined);
  }

  getTreeItem(e: ExtItem): vscode.TreeItem { return e; }

  getChildren(): ExtItem[] {
    const exts = this.activeClient?.state.snapshot?.extensions ?? [];
    if (exts.length === 0) {
      const empty = new vscode.TreeItem('No extensions registered');
      empty.iconPath = new vscode.ThemeIcon('info');
      return [empty as ExtItem];
    }
    return exts.map(e => new ExtItem(e));
  }
}
