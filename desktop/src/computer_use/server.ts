import { execFile, execFileSync } from 'node:child_process';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const MOCK_SCREENSHOT =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

export class ComputerUseServer {
  private mode: 'mcp' | 'direct' | 'mock';
  private readonly cuaBinary: string;

  constructor() {
    this.cuaBinary = process.env.CUA_BINARY || 'cua';
    this.mode = this.detectMode();
  }

  private detectMode(): 'mcp' | 'direct' | 'mock' {
    if (this.isCUABinaryAvailable()) {
      return 'direct';
    }

    // Check for MCP server
    if (this.isMCPAvailable()) {
      return 'mcp';
    }

    return 'mock';
  }

  private isCUABinaryAvailable(): boolean {
    try {
      execFileSync(this.cuaBinary, ['--version'], { stdio: 'ignore' });
      return true;
    } catch {
      return false;
    }
  }

  private isMCPAvailable(): boolean {
    // Attempt to ping MCP endpoint
    return false; // TODO: HTTP check
  }

  async takeScreenshot(): Promise<string> {
    if (this.mode === 'mock') return MOCK_SCREENSHOT;
    if (this.mode === 'direct') {
      const { stdout } = await execFileAsync(this.cuaBinary, [
        'screenshot',
        '--format',
        'base64',
      ]);
      return stdout.trim();
    }
    return await this.mcpScreenshot();
  }

  async click(x: number, y: number): Promise<void> {
    if (this.mode === 'mock') {
      console.log(`MOCK CLICK: ${x}, ${y}`);
      return;
    }
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, [
        'click',
        '--x',
        String(x),
        '--y',
        String(y),
      ]);
      return;
    }
    await this.mcpClick(x, y);
  }

  async typeText(text: string): Promise<void> {
    if (this.mode === 'mock') {
      console.log(`MOCK TYPE: "${text}"`);
      return;
    }
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, ['type', '--text', text]);
      return;
    }
    await this.mcpTypeText(text);
  }

  async openApp(appName: string): Promise<void> {
    if (this.mode === 'mock') {
      console.log(`MOCK OPEN APP: ${appName}`);
      return;
    }
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, ['launch', appName]);
      return;
    }
    await this.mcpOpenApp(appName);
  }

  isAvailable(): boolean {
    return this.mode !== 'mock';
  }

  getMode(): string {
    return this.mode;
  }

  private async mcpScreenshot(): Promise<string> {
    const result = await this.mcpRequest('screenshot', { format: 'base64' });
    return result.data;
  }

  private async mcpClick(x: number, y: number): Promise<void> {
    await this.mcpRequest('click', { x, y });
  }

  private async mcpTypeText(text: string): Promise<void> {
    await this.mcpRequest('type_text', { text });
  }

  private async mcpOpenApp(appName: string): Promise<void> {
    await this.mcpRequest('open_app', { app_name: appName });
  }

  private async mcpRequest(method: string, params: unknown): Promise<any> {
    const resp = await fetch('http://localhost:8796/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: `cua:${method}`,
        params,
      }),
    });
    if (!resp.ok) throw new Error(`CUA MCP returned HTTP ${resp.status}`);
    const result = await resp.json();
    if (result.error) throw new Error(result.error.message || 'CUA MCP failed');
    return result.result;
  }
}
