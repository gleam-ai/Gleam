// Computer Use server implementation (native bindings)
import { exec } from 'child_process';
import { promisify } from 'util';
import * as fs from 'fs';

const execAsync = promisify(exec);

export class ComputerUseServer {
  private mode: 'mcp' | 'direct' | 'mock';
  private cuaBinary: string;
  
  constructor() {
    this.mode = this.detectMode();
    this.cuaBinary = process.env.CUA_BINARY || 'cua';
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
      execSync(`which ${this.cuaBinary}`);
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
    if (this.mode === 'mock') {
      return await this.mockScreenshot();
    }

    if (this.mode === 'direct') {
      const { stdout } = await execAsync(`${this.cuaBinary} screenshot --format base64`);
      return stdout.trim();
    }

    // MCP mode via HTTP
    return await this.mcpScreenshot();
  }

  async click(x: number, y: number): Promise<void> {
    if (this.mode === 'mock') {
      console.log(`MOCK CLICK: ${x}, ${y}`);
      return;
    }

    if (this.mode === 'direct') {
      await execAsync(`${this.cuaBinary} click --x ${x} --y ${y}`);
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
      const escapedText = text.replace(/"/g, '\\"');
      await execAsync(`${this.cuaBinary} type --text "${escapedText}"`);
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
      await execAsync(`${this.cuaBinary} launch "${appName}"`);
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
    const resp = await fetch('http://localhost:8796/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: 'cua:screenshot',
        params: { format: 'base64' }
      })
    });
    
    const result = await resp.json();
    return result.result.data;
  }

  private async mcpClick(x: number, y: number): Promise<void> {
    await fetch('http://localhost:8796/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: 'cua:click',
        params: { x, y }
      })
    });
  }

  private async mcpTypeText(text: string): Promise<void> {
    await fetch('http://localhost:8796/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: 'cua:type_text',
        params: { text }
      })
    });
  }

  private async mcpOpenApp(appName: string): Promise<void> {
    await fetch('http://localhost:8796/rpc', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        jsonrpc: '2.0',
        id: '1',
        method: 'cua:open_app',
        params: { app_name: appName }
      })
    });
  }

  private async mockScreenshot(): Promise<string> {
    // Generate a mock placeholder image
    const canvas = document.createElement('canvas');
    canvas.width = 1920;
    canvas.height = 1080;
    const ctx = canvas.getContext('2d');
    ctx?.fillRect(0, 0, 1920, 1080, '#ccc');
    return canvas.toDataURL('image/png');
  }
}
