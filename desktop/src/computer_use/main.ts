import { ipcMain } from 'electron';
import { ComputerUseServer, ComputerUseUnavailableError } from './server';

const server = new ComputerUseServer();

function failure(error: unknown): {
  success: false;
  error: string;
  unavailable?: true;
} {
  return {
    success: false,
    error: error instanceof Error ? error.message : String(error),
    ...(error instanceof ComputerUseUnavailableError && { unavailable: true }),
  };
}

// Every success carries mock: true when it came from the dry-run backend, and
// a machine with no backend answers success: false instead of pretending.
ipcMain.handle('computer-use:screenshot', async () => {
  try {
    return { success: true, ...(await server.takeScreenshot()) };
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
      return { success: true, ...(await server.click(args.x, args.y)) };
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
      return { success: true, ...(await server.typeText(args.text)) };
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
      return { success: true, ...(await server.openApp(args.app_name)) };
    } catch (error) {
      return failure(error);
    }
  },
);

ipcMain.handle('computer-use:health', () => ({
  available: server.isAvailable(),
  mode: server.getMode(),
  mock: server.isMock(),
}));
