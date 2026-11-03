// VS Code command handlers for computer use
import * as vscode from 'vscode';
import { getComputerUseAPI } from './computer_use';

export function registerComputerUseCommands(context: vscode.ExtensionContext) {
  const disposableScreenshot = vscode.commands.registerCommand(
    'computer-use.screenshot',
    async () => {
      const api = getComputerUseAPI();
      if (!api) {
        vscode.window.showErrorMessage('Computer Use is not available');
        return;
      }

      try {
        const screenshot = await api.takeScreenshot();
        const doc = await vscode.workspace.openTextDocument({
          content: screenshot,
          language: 'png',
        });
        await vscode.window.showTextDocument(doc);
      } catch (error: any) {
        vscode.window.showErrorMessage(`Screenshot failed: ${error.message}`);
      }
    },
  );

  const disposableClick = vscode.commands.registerCommand(
    'computer-use.click',
    async (x: number, y: number) => {
      const api = getComputerUseAPI();
      if (!api) {
        vscode.window.showErrorMessage('Computer Use is not available');
        return;
      }

      try {
        await api.click(x, y);
        vscode.window.showInformationMessage(`Clicked at (${x}, ${y})`);
      } catch (error: any) {
        vscode.window.showErrorMessage(`Click failed: ${error.message}`);
      }
    },
  );

  const disposableType = vscode.commands.registerCommand(
    'computer-use.type',
    async (text: string) => {
      const api = getComputerUseAPI();
      if (!api) {
        vscode.window.showErrorMessage('Computer Use is not available');
        return;
      }

      try {
        await api.typeText(text);
        vscode.window.showInformationMessage(`Typed: "${text}"`);
      } catch (error: any) {
        vscode.window.showErrorMessage(`Type failed: ${error.message}`);
      }
    },
  );

  const disposableOpenApp = vscode.commands.registerCommand(
    'computer-use.open_app',
    async (appName: string) => {
      const api = getComputerUseAPI();
      if (!api) {
        vscode.window.showErrorMessage('Computer Use is not available');
        return;
      }

      try {
        await api.openApp(appName);
        vscode.window.showInformationMessage(`Opened app: ${appName}`);
      } catch (error: any) {
        vscode.window.showErrorMessage(`Open app failed: ${error.message}`);
      }
    },
  );

  const disposableHealth = vscode.commands.registerCommand(
    'computer-use.health',
    async () => {
      const api = getComputerUseAPI();
      if (!api) {
        return { available: false };
      }

      try {
        const available = await api.isAvailable();
        vscode.window.showInformationMessage(
          `Computer Use ${available ? 'is' : 'is NOT'} available`,
        );
        return { available };
      } catch (error: any) {
        return { available: false, error: error.message };
      }
    },
  );

  const disposableHistory = vscode.commands.registerCommand(
    'computer-use.history',
    async () => {
      const api = getComputerUseAPI();
      if (!api) {
        vscode.window.showErrorMessage('Computer Use is not available');
        return [];
      }

      try {
        const history = await api.getHistory();
        console.log('Computer Use History:', JSON.stringify(history, null, 2));
        return history;
      } catch (error: any) {
        vscode.window.showErrorMessage(`Get history failed: ${error.message}`);
        return [];
      }
    },
  );

  context.subscriptions.push(
    disposableScreenshot,
    disposableClick,
    disposableType,
    disposableOpenApp,
    disposableHealth,
    disposableHistory,
  );
}
