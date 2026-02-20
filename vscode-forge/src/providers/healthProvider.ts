import * as vscode from 'vscode';
import { DebugCheckResult } from '../types';
import { ForgeDebugClient } from '../client';

const STATUS_ICON: Record<string, string> = {
  healthy: 'pass', degraded: 'warning', unhealthy: 'error', unknown: 'question',
};
const STATUS_COLOR: Record<string, string> = {
  healthy: 'charts.green', degraded: 'charts.yellow',
  unhealthy: 'charts.red', unknown: 'foreground',
};

class CheckItem extends vscode.TreeItem {
  constructor(name: string, result: DebugCheckResult) {
    super(name, vscode.TreeItemCollapsibleState.None);
    const status = result.status.toLowerCase();
    this.description = result.response_ms != null ? `${result.response_ms}ms` : '';
    this.tooltip = result.message ?? status;
    this.iconPath = new vscode.ThemeIcon(
      STATUS_ICON[status] ?? 'circle-outline',
      new vscode.ThemeColor(STATUS_COLOR[status] ?? 'foreground')
    );
    this.contextValue = 'forgeHealthCheck';
  }
}

class OverallItem extends vscode.TreeItem {
  constructor(overall: string) {
    const status = overall.toLowerCase();
    super(`Overall: ${overall}`, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon(
      STATUS_ICON[status] ?? 'circle-outline',
      new vscode.ThemeColor(STATUS_COLOR[status] ?? 'foreground')
    );
  }
}

export class HealthProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<vscode.TreeItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private activeClient?: ForgeDebugClient;

  setClient(client: ForgeDebugClient | undefined): void {
    this.activeClient = client;
    client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
    this._onDidChangeTreeData.fire(undefined);
  }

  getTreeItem(e: vscode.TreeItem): vscode.TreeItem { return e; }

  getChildren(): vscode.TreeItem[] {
    const health = this.activeClient?.state.health;
    if (!health) {
      const empty = new vscode.TreeItem('No health data — start forge dev');
      empty.iconPath = new vscode.ThemeIcon('info');
      return [empty];
    }
    const items: vscode.TreeItem[] = [new OverallItem(health.overall)];
    for (const [name, result] of Object.entries(health.checks)) {
      items.push(new CheckItem(name, result));
    }
    return items;
  }
}
