import { ipcMain } from 'electron';
import { ComputerUseServer } from './server';

const server = new ComputerUseServer();

function failure(error: unknown): { error: string } {
  return { error: error instanceof Error ? error.message : String(error) };
}

ipcMain.handle('computer-use:screenshot', async () => {
  try {
    return { success: true, data: await server.takeScreenshot() };
  } catch (error) {
    return failure(error);
  }
});

ipcMain.handle(
  'computer-use:click',
  async (_event, args: { x?: unknown; y?: unknown }) => {
    try {
      if (
        typeof args?.x !== 'number' ||
        !Number.isFinite(args.x) ||
        typeof args?.y !== 'number' ||
        !Number.isFinite(args.y)
      ) {
        throw new Error('click coordinates must be finite numbers');
      }
      await server.click(args.x, args.y);
      return { success: true };
    } catch (error) {
      return failure(error);
    }
  },
);

ipcMain.handle(
  'computer-use:type',
  async (_event, args: { text?: unknown }) => {
    try {
      if (typeof args?.text !== 'string')
        throw new Error('text must be a string');
      await server.typeText(args.text);
      return { success: true };
    } catch (error) {
      return failure(error);
    }
  },
);

ipcMain.handle(
  'computer-use:open_app',
  async (_event, args: { app_name?: unknown }) => {
    try {
      if (typeof args?.app_name !== 'string' || !args.app_name.trim()) {
        throw new Error('app_name must be a non-empty string');
      }
      await server.openApp(args.app_name);
      return { success: true };
    } catch (error) {
      return failure(error);
    }
  },
);

ipcMain.handle('computer-use:health', () => ({
  available: server.isAvailable(),
  mode: server.getMode(),
}));
