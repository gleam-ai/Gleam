import { execFile, execFileSync } from 'node:child_process';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
// Placeholder returned by the mock backend only. It is a 1x1 pixel and is
// always delivered together with mock: true, so nobody mistakes it for a screen.
const MOCK_SCREENSHOT =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

// Same contract as the Go tool in internal/agent/tools/cua:
//   - direct: a cua executable runs the action.
//   - mcp: a local CUA MCP service runs it (chosen through GLEAM_CUA_MODE=mcp).
//   - mock: dry run, enabled only through GLEAM_CUA_MODE=mock; every result is
//     flagged mock: true.
//   - unavailable: no backend. Actions fail. This used to fall back to the
//     mock and answer success for a click that never happened.
export type ComputerUseMode = 'direct' | 'mcp' | 'mock' | 'unavailable';

export class ComputerUseUnavailableError extends Error {
  readonly unavailable = true;

  constructor(message: string) {
    super(message);
    this.name = 'ComputerUseUnavailableError';
  }
}

export class ComputerUseServer {
  private mode: ComputerUseMode;
  private readonly cuaBinary: string;
  private readonly unavailableReason: string;

  constructor() {
    this.cuaBinary = process.env.CUA_BINARY || 'cua';
    const detected = this.detectMode();
    this.mode = detected.mode;
    this.unavailableReason = detected.reason;
  }

  private detectMode(): { mode: ComputerUseMode; reason: string } {
    switch ((process.env.GLEAM_CUA_MODE || '').trim().toLowerCase()) {
      case 'mock':
        return { mode: 'mock', reason: '' };
      case 'direct':
        return { mode: 'direct', reason: '' };
      case 'mcp':
        return { mode: 'mcp', reason: '' };
    }
    if (this.isCUABinaryAvailable()) {
      return { mode: 'direct', reason: '' };
    }
    return {
      mode: 'unavailable',
      reason:
        `no computer-use backend: executable "${this.cuaBinary}" not found ` +
        '(set CUA_BINARY, or GLEAM_CUA_MODE=mcp for a local CUA MCP service)',
    };
  }

  private isCUABinaryAvailable(): boolean {
    try {
      execFileSync(this.cuaBinary, ['--version'], { stdio: 'ignore' });
      return true;
    } catch {
      return false;
    }
  }

  private requireBackend(): void {
    if (this.mode === 'unavailable') {
      throw new ComputerUseUnavailableError(this.unavailableReason);
    }
  }

  async takeScreenshot(): Promise<{ data: string; mock?: true }> {
    this.requireBackend();
    if (this.mode === 'mock') return { data: MOCK_SCREENSHOT, mock: true };
    if (this.mode === 'direct') {
      const { stdout } = await execFileAsync(this.cuaBinary, [
        'screenshot',
        '--format',
        'base64',
      ]);
      return { data: stdout.trim() };
    }
    return { data: await this.mcpScreenshot() };
  }

  async click(x: number, y: number): Promise<{ mock?: true }> {
    this.requireBackend();
    if (this.mode === 'mock') return { mock: true };
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, [
        'click',
        '--x',
        String(x),
        '--y',
        String(y),
      ]);
      return {};
    }
    await this.mcpClick(x, y);
    return {};
  }

  async typeText(text: string): Promise<{ mock?: true }> {
    this.requireBackend();
    if (this.mode === 'mock') return { mock: true };
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, ['type', '--text', text]);
      return {};
    }
    await this.mcpTypeText(text);
    return {};
  }

  async openApp(appName: string): Promise<{ mock?: true }> {
    this.requireBackend();
    if (this.mode === 'mock') return { mock: true };
    if (this.mode === 'direct') {
      await execFileAsync(this.cuaBinary, ['launch', appName]);
      return {};
    }
    await this.mcpOpenApp(appName);
    return {};
  }

  // Only a real backend is "available"; the mock can rehearse a call and
  // nothing more.
  isAvailable(): boolean {
    return this.mode === 'direct' || this.mode === 'mcp';
  }

  isMock(): boolean {
    return this.mode === 'mock';
  }

  getMode(): ComputerUseMode {
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
