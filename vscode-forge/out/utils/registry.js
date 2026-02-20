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
exports.readRegistry = readRegistry;
exports.watchRegistry = watchRegistry;
const fs = __importStar(require("fs"));
const os = __importStar(require("os"));
const path = __importStar(require("path"));
const REGISTRY_PATH = path.join(os.homedir(), '.forge', 'debug-servers.json');
function readRegistry() {
    try {
        const data = fs.readFileSync(REGISTRY_PATH, 'utf-8');
        const reg = JSON.parse(data);
        return reg.servers ?? [];
    }
    catch {
        return [];
    }
}
function watchRegistry(onChange) {
    const dir = path.dirname(REGISTRY_PATH);
    try {
        fs.mkdirSync(dir, { recursive: true });
        // Write empty registry if it doesn't exist so we can watch the file.
        if (!fs.existsSync(REGISTRY_PATH)) {
            fs.writeFileSync(REGISTRY_PATH, JSON.stringify({ servers: [] }, null, 2));
        }
        return fs.watch(REGISTRY_PATH, () => {
            onChange(readRegistry());
        });
    }
    catch {
        return undefined;
    }
}
//# sourceMappingURL=registry.js.map