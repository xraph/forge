import * as vscode from 'vscode';
import { ForgeDebugClient } from '../client';

export class MetricsPanel {
  private static instance?: MetricsPanel;
  private panel: vscode.WebviewPanel;
  private disposables: vscode.Disposable[] = [];

  private constructor(
    panel: vscode.WebviewPanel,
    private client: ForgeDebugClient
  ) {
    this.panel = panel;
    panel.onDidDispose(() => this.dispose(), null, this.disposables);

    client.on('metricsUpdated', () => {
      this.panel.webview.postMessage({
        type: 'metrics',
        raw: client.state.metricsRaw ?? '',
      });
    });

    this.panel.webview.html = this.buildHtml();
  }

  static show(client: ForgeDebugClient, extensionUri: vscode.Uri): MetricsPanel {
    if (MetricsPanel.instance) {
      MetricsPanel.instance.panel.reveal();
      MetricsPanel.instance.client = client;
      return MetricsPanel.instance;
    }
    const panel = vscode.window.createWebviewPanel(
      'forgeMetrics',
      'Forge Metrics',
      vscode.ViewColumn.Two,
      { enableScripts: true, retainContextWhenHidden: true }
    );
    MetricsPanel.instance = new MetricsPanel(panel, client);
    return MetricsPanel.instance;
  }

  private dispose() {
    MetricsPanel.instance = undefined;
    this.disposables.forEach(d => d.dispose());
  }

  private buildHtml(): string {
    return /* html */`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Forge Metrics</title>
  <style>
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground);
           background: var(--vscode-editor-background); padding: 16px; margin: 0; }
    h2 { font-size: 14px; margin: 0 0 8px; color: var(--vscode-foreground); }
    .metric { display: flex; justify-content: space-between; padding: 4px 0;
              border-bottom: 1px solid var(--vscode-panel-border); font-size: 12px; }
    .metric-name { color: var(--vscode-symbolIcon-variableForeground); }
    .metric-value { font-weight: bold; }
    #raw { font-family: monospace; font-size: 11px; white-space: pre-wrap;
           max-height: 300px; overflow-y: auto; margin-top: 16px;
           background: var(--vscode-textCodeBlock-background); padding: 8px; }
    #empty { color: var(--vscode-descriptionForeground); font-style: italic; }
  </style>
</head>
<body>
  <h2>Forge Metrics</h2>
  <div id="metrics"><span id="empty">Waiting for metrics...</span></div>
  <details>
    <summary style="cursor:pointer;font-size:11px;margin-top:8px">Raw Prometheus</summary>
    <div id="raw"></div>
  </details>
  <script>
    const vscode = acquireVsCodeApi();
    const metricsEl = document.getElementById('metrics');
    const rawEl = document.getElementById('raw');

    window.addEventListener('message', event => {
      const { type, raw } = event.data;
      if (type !== 'metrics') { return; }
      rawEl.textContent = raw;

      // Parse simple metric lines: metric_name{...} value
      const parsed = [];
      for (const line of raw.split('\\n')) {
        if (line.startsWith('#') || !line.trim()) { continue; }
        const m = line.match(/^([a-zA-Z_:][a-zA-Z0-9_:]*(?:\\{[^}]*\\})?)\\s+([\\d.eE+\\-]+(?:\\s+\\d+)?)/);
        if (m) { parsed.push({ name: m[1], value: m[2].split(' ')[0] }); }
      }

      if (parsed.length === 0) {
        metricsEl.innerHTML = '<span id="empty">No metrics received yet</span>';
        return;
      }

      metricsEl.innerHTML = parsed.slice(0, 50).map(p =>
        '<div class="metric">' +
          '<span class="metric-name">' + escHtml(p.name) + '</span>' +
          '<span class="metric-value">' + escHtml(p.value) + '</span>' +
        '</div>'
      ).join('');
    });

    function escHtml(s) {
      return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
    }
  </script>
</body>
</html>`;
  }
}
