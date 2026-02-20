import * as vscode from 'vscode';
import * as fs from 'fs';
import { ServerEntry } from './types';
import { readRegistry, watchRegistry } from './utils/registry';
import { isPidAlive } from './utils/pid';

export class ServerDiscovery {
  private watcher?: fs.FSWatcher;
  private listeners: Array<(servers: ServerEntry[]) => void> = [];

  start(): void {
    this.watcher = watchRegistry((servers) => {
      const live = this.filterLive(servers);
      this.listeners.forEach(l => l(live));
    });
  }

  stop(): void {
    this.watcher?.close();
  }

  current(): ServerEntry[] {
    return this.filterLive(readRegistry());
  }

  onChange(listener: (servers: ServerEntry[]) => void): void {
    this.listeners.push(listener);
  }

  private filterLive(servers: ServerEntry[]): ServerEntry[] {
    const showAll = vscode.workspace.getConfiguration('forge').get<boolean>('showAllServers', false);
    const workspaceDirs = (vscode.workspace.workspaceFolders ?? []).map(f => f.uri.fsPath);

    return servers.filter(s => {
      if (!isPidAlive(s.pid)) { return false; }
      if (showAll) { return true; }
      return workspaceDirs.some(d => s.workspace_dir.startsWith(d) || d.startsWith(s.workspace_dir));
    });
  }
}
