// Main process IPC handlers for computer use
import { ipcMain } from 'electron';
import { ComputerUseServer } from './server';
import { CUASafetyGate } from '../safety/cua_sandbox';

const server = new ComputerUseServer();
const safetyGate = new CUASafetyGate();

ipcMain.handle('computer-use:screenshot', async (event, args) => {
  try {
    const result = await server.takeScreenshot();
    return { success: true, data: result };
  } catch (error: any) {
    return { error: error.message };
  }
});

ipcMain.handle('computer-use:click', async (event, { x, y }) => {
  try {
    await safetyGate.ExecuteWithSafety(
      global.context,
      'click',
      { x, y },
      async () => await server.click(x, y)
    );
    return { success: true };
  } catch (error: any) {
    return { error: error.message };
  }
});

ipcMain.handle('computer-use:type', async (event, { text }) => {
  try {
    await safetyGate.ExecuteWithSafety(
      global.context,
      'type_text',
      { text },
      async () => await server.typeText(text)
    );
    return { success: true };
  } catch (error: any) {
    return { error: error.message };
  }
});

ipcMain.handle('computer-use:open_app', async (event, { app_name }) => {
  try {
    await safetyGate.AllowAction(global.context, 'open_app', { app_name });
    await server.openApp(app_name);
    return { success: true };
  } catch (error: any) {
    return { error: error.message };
  }
});

ipcMain.handle('computer-use:health', async () => {
  return {
    available: server.isAvailable(),
    mode: server.getMode()
  };
});

ipcMain.handle('computer-use:history', async () => {
  return safetyGate.GetHistory(50);
});
