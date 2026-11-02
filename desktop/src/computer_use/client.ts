// Client for CUA MCP communication
export function createClient() {
  return {
    async request(method: string, params?: any) {
      // Browser-based implementation
      const resp = await fetch('http://localhost:8796/rpc', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          jsonrpc: '2.0',
          id: Date.now(),
          method: `cua:${method}`,
          params
        })
      });
      
      const result = await resp.json();
      return result.result;
    }
  };
}
