import { contextBridge, ipcRenderer, type IpcRendererEvent } from 'electron';

type WindowState = { maximized: boolean; fullscreen: boolean };

contextBridge.exposeInMainWorld('gleamDesktop', {
  isDesktop: true,
  platform: process.platform,
  info: (): Promise<unknown> => ipcRenderer.invoke('desktop:info'),
  edit: (action: string): Promise<void> =>
    ipcRenderer.invoke('desktop:edit', action),
  view: (action: string): Promise<void> =>
    ipcRenderer.invoke('desktop:view', action),
  window: (action: string): Promise<WindowState> =>
    ipcRenderer.invoke('desktop:window', action),
  windowState: (): Promise<WindowState> =>
    ipcRenderer.invoke('desktop:windowState'),
  onWindowState: (cb: (s: WindowState) => void): (() => void) => {
    const h = (_e: IpcRendererEvent, s: WindowState) => cb(s);
    ipcRenderer.on('desktop:window-state', h);
    return () => ipcRenderer.removeListener('desktop:window-state', h);
  },
  openExternal: (url: string): Promise<boolean> =>
    ipcRenderer.invoke('desktop:openExternal', url),
  capture: (): Promise<string> => ipcRenderer.invoke('desktop:capture'),
  newWindow: (): Promise<void> => ipcRenderer.invoke('desktop:newWindow'),
  showOpenDialog: (options: Electron.OpenDialogOptions): Promise<string[]> =>
    ipcRenderer.invoke('desktop:showOpenDialog', options),
  globalShortcut: (
    action: 'get' | 'set',
    accelerator?: string,
  ): Promise<{ ok: boolean; accelerator: string; error?: string }> =>
    ipcRenderer.invoke('desktop:globalShortcut', action, accelerator),
  update: {
    check: (): Promise<unknown> => ipcRenderer.invoke('desktop:update:check'),
    status: (): Promise<unknown> => ipcRenderer.invoke('desktop:update:status'),
    download: (): Promise<unknown> =>
      ipcRenderer.invoke('desktop:update:download'),
    restart: (): Promise<unknown> =>
      ipcRenderer.invoke('desktop:update:restart'),
    onStatus: (cb: (s: unknown) => void): (() => void) => {
      const h = (_e: IpcRendererEvent, s: unknown) => cb(s);
      ipcRenderer.on('desktop:update-status', h);
      return () => ipcRenderer.removeListener('desktop:update-status', h);
    },
  },
  tray: {
    sync: (state: {
      lang: string;
      recents: { id: string; title: string }[];
    }): Promise<unknown> => ipcRenderer.invoke('desktop:tray:sync', state),
    onAction: (cb: (a: unknown) => void): (() => void) => {
      const h = (_e: IpcRendererEvent, a: unknown) => cb(a);
      ipcRenderer.on('desktop:tray-action', h);
      return () => ipcRenderer.removeListener('desktop:tray-action', h);
    },
  },
});
