import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { ServerEntry } from '../types';

const REGISTRY_PATH = path.join(os.homedir(), '.forge', 'debug-servers.json');

interface RegistryFile {
  servers: ServerEntry[];
}

export function readRegistry(): ServerEntry[] {
  try {
    const data = fs.readFileSync(REGISTRY_PATH, 'utf-8');
    const reg: RegistryFile = JSON.parse(data);
    return reg.servers ?? [];
  } catch {
    return [];
  }
}

export function watchRegistry(onChange: (servers: ServerEntry[]) => void): fs.FSWatcher | undefined {
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
  } catch {
    return undefined;
  }
}
