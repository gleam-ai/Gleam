// VS Code panel for computer use operations
import * as vscode from 'vscode';

export class ComputerUsePanel {
  public static currentPanel: ComputerUsePanel | undefined;
  private disposables: vscode.Disposable[] = [];

  public static showView() {
    if (!ComputerUsePanel.currentPanel) {
      ComputerUsePanel.create();
    }
    ComputerUsePanel.currentPanel?.panel.reveal();
  }

  private static create() {
    const panel = vscode.window.createWebviewPanel(
      'computerUseView',
      'Computer Use',
      vscode.ViewColumn.One,
      { enableScripts: true },
    );
    const instance = new ComputerUsePanel(panel);
    ComputerUsePanel.currentPanel = instance;

    panel.webview.html = instance.getHtmlContent();
  }

  private constructor(public readonly panel: vscode.WebviewPanel) {}

  private getHtmlContent(): string {
    return `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Computer Use - Gleam</title>
  <style>
    body { 
      font-family: var(--vscode-font-family);
      padding: 20px;
      background: var(--vscode-widget-background);
      color: var(--vscode-foreground);
    }
    .screen-capture {
      width: 100%;
      height: 300px;
      background: var(--vscode-editor-background);
      border: 1px solid var(--vscode-border);
      display: flex;
      align-items: center;
      justify-content: center;
      margin-bottom: 20px;
    }
    button {
      background: var(--vscode-button-background);
      color: var(--vscode-button-foreground);
      border: none;
      padding: 8px 16px;
      margin: 5px;
      cursor: pointer;
      border-radius: 3px;
    }
    button:hover {
      background: var(--vscode-button-hoverBackground);
    }
    .actions {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 10px;
      margin-bottom: 20px;
    }
    .action-btn {
      padding: 15px;
      text-align: center;
    }
    input[type="text"] {
      width: 100%;
      padding: 8px;
      margin: 10px 0;
      background: var(--vscode-input-background);
      border: 1px solid var(--vscode-input-border);
      color: var(--vscode-input-foreground);
    }
    #screenshotOutput {
      max-width: 100%;
      max-height: 300px;
      display: none;
    }
  </style>
</head>
<body>
  <h2>🖥️ Computer Use</h2>
  
  <div class="screen-capture">
    <div id="screenshotContainer">
      <img id="screenshotOutput" />
      <p id="emptyState">Click "Take Screenshot" to capture current screen</p>
    </div>
  </div>

  <button onclick="takeScreenshot()">📸 Take Screenshot</button>
  <button onclick="showHistory()">📋 View History</button>

  <h3>Actions</h3>
  <div class="actions">
    <button class="action-btn" onclick="clickAtMouse()">🖱️ Click at Cursor</button>
    <button class="action-btn" onclick="openChrome()">🌐 Open Chrome</button>
    <button class="action-btn" onclick="openVSCode()">💻 Open VS Code</button>
    <button class="action-btn" onclick="closeActiveApp()">❌ Close App</button>
  </div>

  <h3>Type Text</h3>
  <input type="text" id="textInput" placeholder="Enter text to type..." />
  <button onclick="typeText()">Type</button>

  <script>
    async function takeScreenshot() {
      document.getElementById('emptyState').style.display = 'none';
      try {
        const response = await fetch('http://localhost:8796/api/cua/screenshot', {
          method: 'POST'
        });
        const data = await response.json();
        
        const img = document.getElementById('screenshotOutput');
        img.src = data.data;
        img.style.display = 'block';
      } catch (error) {
        console.error('Screenshot failed:', error);
      }
    }

    async function clickAtMouse() {
      // Get mouse coordinates from system (would need native bridge)
      console.log('Click at current cursor position');
    }

    async function openChrome() {
      try {
        await fetch('http://localhost:8796/api/cua/open_app', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ app_name: 'chrome' })
        });
      } catch (error) {
        console.error('Open app failed:', error);
      }
    }

    async function openVSCode() {
      try {
        await fetch('http://localhost:8796/api/cua/open_app', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ app_name: 'vscode' })
        });
      } catch (error) {
        console.error('Open app failed:', error);
      }
    }

    async function closeActiveApp() {
      console.log('Close active application');
    }

    async function typeText() {
      const text = document.getElementById('textInput').value;
      if (!text) return;

      try {
        await fetch('http://localhost:8796/api/cua/type_text', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text })
        });
      } catch (error) {
        console.error('Type text failed:', error);
      }
    }

    async function showHistory() {
      try {
        const response = await fetch('http://localhost:8796/api/cua/history');
        const history = await response.json();
        console.log('Action history:', history);
        alert('Last ' + history.length + ' actions shown in console');
      } catch (error) {
        console.error('Get history failed:', error);
      }
    }
  </script>
</body>
</html>
`;
  }

  public dispose() {
    ComputerUsePanel.currentPanel = undefined;
    this.disposables.forEach((d) => d.dispose());
  }
}
