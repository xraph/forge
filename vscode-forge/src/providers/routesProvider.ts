import * as vscode from 'vscode';
import { DebugRoute } from '../types';
import { ForgeDebugClient } from '../client';

const METHOD_COLORS: Record<string, string> = {
  GET: 'charts.green', POST: 'charts.blue', PUT: 'charts.yellow',
  PATCH: 'charts.orange', DELETE: 'charts.red',
};

export class RouteItem extends vscode.TreeItem {
  constructor(public readonly route: DebugRoute) {
    super(`${route.method} ${route.path}`, vscode.TreeItemCollapsibleState.None);
    this.description = route.summary ?? route.tags?.join(', ') ?? '';
    this.tooltip = [
      route.summary,
      route.description,
      route.tags?.length ? `Tags: ${route.tags.join(', ')}` : '',
    ].filter(Boolean).join('\n');
    this.iconPath = new vscode.ThemeIcon(
      'arrow-right',
      new vscode.ThemeColor(METHOD_COLORS[route.method] ?? 'foreground')
    );
    this.contextValue = 'forgeRoute';
  }
}

export class RoutesProvider implements vscode.TreeDataProvider<RouteItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<RouteItem | undefined>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private activeClient?: ForgeDebugClient;

  setClient(client: ForgeDebugClient | undefined): void {
    this.activeClient = client;
    client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
    this._onDidChangeTreeData.fire(undefined);
  }

  getTreeItem(e: RouteItem): vscode.TreeItem { return e; }

  getChildren(): RouteItem[] {
    const routes = this.activeClient?.state.snapshot?.routes ?? [];
    if (routes.length === 0) {
      const empty = new vscode.TreeItem('No routes registered');
      empty.iconPath = new vscode.ThemeIcon('info');
      return [empty as RouteItem];
    }
    return routes.map(r => new RouteItem(r));
  }
}
