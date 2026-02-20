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
exports.RoutesProvider = exports.RouteItem = void 0;
const vscode = __importStar(require("vscode"));
const METHOD_COLORS = {
    GET: 'charts.green', POST: 'charts.blue', PUT: 'charts.yellow',
    PATCH: 'charts.orange', DELETE: 'charts.red',
};
class RouteItem extends vscode.TreeItem {
    constructor(route) {
        super(`${route.method} ${route.path}`, vscode.TreeItemCollapsibleState.None);
        this.route = route;
        this.description = route.summary ?? route.tags?.join(', ') ?? '';
        this.tooltip = [
            route.summary,
            route.description,
            route.tags?.length ? `Tags: ${route.tags.join(', ')}` : '',
        ].filter(Boolean).join('\n');
        this.iconPath = new vscode.ThemeIcon('arrow-right', new vscode.ThemeColor(METHOD_COLORS[route.method] ?? 'foreground'));
        this.contextValue = 'forgeRoute';
    }
}
exports.RouteItem = RouteItem;
class RoutesProvider {
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
        const routes = this.activeClient?.state.snapshot?.routes ?? [];
        if (routes.length === 0) {
            const empty = new vscode.TreeItem('No routes registered');
            empty.iconPath = new vscode.ThemeIcon('info');
            return [empty];
        }
        return routes.map(r => new RouteItem(r));
    }
}
exports.RoutesProvider = RoutesProvider;
//# sourceMappingURL=routesProvider.js.map