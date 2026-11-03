import { ipcRenderer } from 'electron';

interface ComputerUseAPI {
  takeScreenshot(): Promise<string>;
  click(x: number, y: number): Promise<void>;
  typeText(text: string): Promise<void>;
  openApp(appName: string): Promise<void>;
  isAvailable(): Promise<boolean>;
}

type IpcResult = {
  success?: boolean;
  data?: string;
  error?: string;
  available?: boolean;
};

async function invoke(channel: string, args?: unknown): Promise<IpcResult> {
  const result = (await ipcRenderer.invoke(channel, args)) as IpcResult;
  if (result.error) throw new Error(result.error);
  return result;
}

export const computerUse: ComputerUseAPI = {
  async takeScreenshot(): Promise<string> {
    const result = await invoke('computer-use:screenshot');
    if (typeof result.data !== 'string')
      throw new Error('invalid screenshot response');
    return result.data;
  },

  async click(x: number, y: number): Promise<void> {
    await invoke('computer-use:click', { x, y });
  },

  async typeText(text: string): Promise<void> {
    await invoke('computer-use:type', { text });
  },

  async openApp(appName: string): Promise<void> {
    await invoke('computer-use:open_app', { app_name: appName });
  },

  async isAvailable(): Promise<boolean> {
    try {
      const result = await invoke('computer-use:health');
      return result.available === true;
    } catch {
      return false;
    }
  },
};
