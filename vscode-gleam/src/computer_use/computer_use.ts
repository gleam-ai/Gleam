// Computer Use API for VS Code extension
import * as vscode from 'vscode';

export interface ComputerUseAPI {
  takeScreenshot(): Promise<string>;
  click(x: number, y: number): Promise<void>;
  typeText(text: string): Promise<void>;
  openApp(appName: string): Promise<void>;
  isAvailable(): Promise<boolean>;
  getHistory(): Promise<any[]>;
}

let api: ComputerUseAPI | null = null;

export function getComputerUseAPI(): ComputerUseAPI | undefined {
  return api;
}

export function setComputerUseAPI(newApi: ComputerUseAPI) {
  api = newApi;
}

class ComputerUseClient implements ComputerUseAPI {
  async takeScreenshot(): Promise<string> {
    return vscode.commands.executeCommand('computer-use:screenshot');
  }

  async click(x: number, y: number): Promise<void> {
    await vscode.commands.executeCommand('computer-use:click', x, y);
  }

  async typeText(text: string): Promise<void> {
    await vscode.commands.executeCommand('computer-use:type', text);
  }

  async openApp(appName: string): Promise<void> {
    await vscode.commands.executeCommand('computer-use:open_app', appName);
  }

  async isAvailable(): Promise<boolean> {
    try {
      const result: any = await vscode.commands.executeCommand('computer-use:health');
      return result.available;
    } catch {
      return false;
    }
  }

  async getHistory(): Promise<any[]> {
    return vscode.commands.executeCommand('computer-use:history');
  }
}

api = new ComputerUseClient();
