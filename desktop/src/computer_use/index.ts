// Computer Use integration for Gleam Desktop
import { ipcRenderer } from 'electron';
import { createClient } from './client';

interface ComputerUseAPI {
  takeScreenshot(): Promise<string>;
  click(x: number, y: number): Promise<void>;
  typeText(text: string): Promise<void>;
  openApp(appName: string): Promise<void>;
  isAvailable(): Promise<boolean>;
}

let client: ReturnType<typeof createClient> | null = null;

export const computerUse: ComputerUseAPI = {
  async takeScreenshot(): Promise<string> {
    if (!client) client = createClient();
    
    return new Promise((resolve, reject) => {
      ipcRenderer.invoke('computer-use:screenshot', {})
        .then(result => resolve(result.data))
        .catch(reject);
    });
  },

  async click(x: number, y: number): Promise<void> {
    if (!client) client = createClient();
    
    return new Promise((resolve, reject) => {
      ipcRenderer.invoke('computer-use:click', { x, y })
        .then(() => resolve())
        .catch(reject);
    });
  },

  async typeText(text: string): Promise<void> {
    if (!client) client = createClient();
    
    return new Promise((resolve, reject) => {
      ipcRenderer.invoke('computer-use:type', { text })
        .then(() => resolve())
        .catch(reject);
    });
  },

  async openApp(appName: string): Promise<void> {
    if (!client) client = createClient();
    
    return new Promise((resolve, reject) => {
      ipcRenderer.invoke('computer-use:open_app', { app_name: appName })
        .then(() => resolve())
        .catch(reject);
    });
  },

  async isAvailable(): Promise<boolean> {
    try {
      const result = await ipcRenderer.invoke('computer-use:health');
      return result.available;
    } catch {
      return false;
    }
  }
};
