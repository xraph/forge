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
exports.ExtensionsProvider = void 0;
const vscode = __importStar(require("vscode"));
class ExtItem extends vscode.TreeItem {
    constructor(ext) {
        super(ext.name, vscode.TreeItemCollapsibleState.None);
        this.description = ext.version;
        this.tooltip = [
            ext.description,
            ext.dependencies.length ? `Deps: ${ext.dependencies.join(', ')}` : '',
            `Healthy: ${ext.healthy}`,
        ].filter(Boolean).join('\n');
        this.iconPath = new vscode.ThemeIcon(ext.healthy ? 'extensions' : 'extensions-warning', ext.healthy
            ? new vscode.ThemeColor('charts.green')
            : new vscode.ThemeColor('charts.red'));
        this.contextValue = 'forgeExtension';
    }
}
class ExtensionsProvider {
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
        const exts = this.activeClient?.state.snapshot?.extensions ?? [];
        if (exts.length === 0) {
            const empty = new vscode.TreeItem('No extensions registered');
            empty.iconPath = new vscode.ThemeIcon('info');
            return [empty];
        }
        return exts.map(e => new ExtItem(e));
    }
}
exports.ExtensionsProvider = ExtensionsProvider;
//# sourceMappingURL=extensionsProvider.js.map