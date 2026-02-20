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
exports.HealthProvider = void 0;
const vscode = __importStar(require("vscode"));
const STATUS_ICON = {
    healthy: 'pass', degraded: 'warning', unhealthy: 'error', unknown: 'question',
};
const STATUS_COLOR = {
    healthy: 'charts.green', degraded: 'charts.yellow',
    unhealthy: 'charts.red', unknown: 'foreground',
};
class CheckItem extends vscode.TreeItem {
    constructor(name, result) {
        super(name, vscode.TreeItemCollapsibleState.None);
        const status = result.status.toLowerCase();
        this.description = result.response_ms != null ? `${result.response_ms}ms` : '';
        this.tooltip = result.message ?? status;
        this.iconPath = new vscode.ThemeIcon(STATUS_ICON[status] ?? 'circle-outline', new vscode.ThemeColor(STATUS_COLOR[status] ?? 'foreground'));
        this.contextValue = 'forgeHealthCheck';
    }
}
class OverallItem extends vscode.TreeItem {
    constructor(overall) {
        const status = overall.toLowerCase();
        super(`Overall: ${overall}`, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon(STATUS_ICON[status] ?? 'circle-outline', new vscode.ThemeColor(STATUS_COLOR[status] ?? 'foreground'));
    }
}
class HealthProvider {
    constructor() {
        this._onDidChangeTreeData = new vscode.EventEmitter();
        this.onDidChangeTreeData = this._onDidChangeTreeData.event;
    }
    setClient(client) {
        this.activeClient = client;
        client?.on('stateChanged', () => this._onDidChangeTreeData.fire(undefined));
        this._onDidChangeTreeData.fire(undefined);
    }
    getTreeItem(e) { return e; }
    getChildren() {
        const health = this.activeClient?.state.health;
        if (!health) {
            const empty = new vscode.TreeItem('No health data — start forge dev');
            empty.iconPath = new vscode.ThemeIcon('info');
            return [empty];
        }
        const items = [new OverallItem(health.overall)];
        for (const [name, result] of Object.entries(health.checks)) {
            items.push(new CheckItem(name, result));
        }
        return items;
    }
}
exports.HealthProvider = HealthProvider;
//# sourceMappingURL=healthProvider.js.map