package agent

import (
	"path/filepath"

	"gleam/internal/config"
	"gleam/internal/harness/credentials"
)

// MigrateMCPSecrets moves plaintext MCP env/header values from the overlay
// yaml into the encrypted credential store. The overlay keeps the keys with
// empty values so structure stays visible. Idempotent: a second run finds no
// non-empty values and does nothing.
func MigrateMCPSecrets(cfg *config.Config, creds *credentials.Store) error {
	if cfg == nil || creds == nil {
		return nil
	}
	changed := false
	for i := range cfg.MCP {
		srv := &cfg.MCP[i]
		envKeys, envSecrets := splitMCPSecrets(srv.Env)
		headerKeys, headerSecrets := splitMCPSecrets(srv.Headers)
		if envSecrets == nil && headerSecrets == nil {
			continue
		}
		if err := creds.SetMCPSecrets(srv.Name, credentials.MCPSecrets{Env: envSecrets, Headers: headerSecrets}); err != nil {
			return err
		}
		srv.Env, srv.Headers = envKeys, headerKeys
		changed = true
	}
	if !changed {
		return nil
	}
	return cfg.SaveOverlay(filepath.Join(cfg.DataDir, config.OverlayFile))
}

// ResolveMCPSecrets returns a copy of srv with empty env/header values filled
// from the encrypted store. Values present in the config win, so an explicit
// override (env var, hand-edited yaml) still works.
func ResolveMCPSecrets(creds *credentials.Store, srv config.MCPServerConfig) config.MCPServerConfig {
	if creds == nil {
		return srv
	}
	sec := creds.GetMCPSecrets(srv.Name)
	srv.Env = fillMCPSecrets(srv.Env, sec.Env)
	srv.Headers = fillMCPSecrets(srv.Headers, sec.Headers)
	return srv
}

func splitMCPSecrets(vals map[string]string) (keys, secrets map[string]string) {
	if len(vals) == 0 {
		return vals, nil
	}
	keys = make(map[string]string, len(vals))
	for k, v := range vals {
		keys[k] = ""
		if v != "" {
			if secrets == nil {
				secrets = map[string]string{}
			}
			secrets[k] = v
		}
	}
	return keys, secrets
}

func fillMCPSecrets(vals, stored map[string]string) map[string]string {
	if len(stored) == 0 {
		return vals
	}
	out := make(map[string]string, len(vals))
	for k, v := range vals {
		if v == "" {
			v = stored[k]
		}
		out[k] = v
	}
	return out
}
