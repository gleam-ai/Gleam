package hardware

import (
	"context"
	"fmt"

	"gleam/internal/tools/toolutil"
	"gleam/pkg/types"
)

// Tools bundles all hardware-related Gleam tools.
type Tools struct{}

// New creates a new Tools bundle.
func New() *Tools { return &Tools{} }

// RegisterAll registers all hardware tools with the given registry.
func (t *Tools) RegisterAll(reg interface {
	MustRegister(types.Tool)
}) {
	reg.MustRegister(&listDevicesTool{})
	reg.MustRegister(&connectTool{})
	reg.MustRegister(&disconnectTool{})
	reg.MustRegister(&sendTool{})
	reg.MustRegister(&receiveTool{})
	reg.MustRegister(&deviceInfoTool{})
}

// hardware.list

type listDevicesTool struct{}

func (t *listDevicesTool) Name() string { return "hardware.list" }
func (t *listDevicesTool) Description() string {
	return "List all registered AI hardware devices and their connection status."
}
func (t *listDevicesTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *listDevicesTool) Schema() map[string]any {
	return toolutil.Schema("List registered hardware devices", nil, map[string]any{
		"connected_only": toolutil.SchemaProp("If true, only return connected devices", "boolean"),
	})
}
func (t *listDevicesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	connectedOnly, _ := args["connected_only"].(bool)

	var devices []Device
	if connectedOnly {
		devices = ListInstances()
	} else {
		devices = List()
	}

	result := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		entry := map[string]any{
			"id":           d.ID(),
			"name":         d.Name(),
			"description":  d.Description(),
			"capabilities": d.Capabilities(),
			"state":        string(d.State()),
		}
		result = append(result, entry)
	}
	return map[string]any{"devices": result, "count": len(result)}, nil
}

// hardware.connect

type connectTool struct{}

func (t *connectTool) Name() string { return "hardware.connect" }
func (t *connectTool) Description() string {
	return "Connect to a hardware device. Requires device ID and optional configuration."
}
func (t *connectTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *connectTool) Schema() map[string]any {
	return toolutil.Schema("Connect to a hardware device", []string{"device_id"}, map[string]any{
		"device_id": toolutil.SchemaProp("Device identifier (e.g. xiaohong, qianwen_a2)", "string"),
		"config":    toolutil.SchemaProp("Device-specific configuration (port, API key, etc.)", "object"),
	})
}
func (t *connectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := toolutil.RequireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	config, _ := args["config"].(map[string]any)
	if config == nil {
		config = make(map[string]any)
	}

	if err := Connect(ctx, deviceID, config); err != nil {
		return nil, err
	}
	return map[string]any{"status": "connected", "device_id": deviceID}, nil
}

// hardware.disconnect

type disconnectTool struct{}

func (t *disconnectTool) Name() string                 { return "hardware.disconnect" }
func (t *disconnectTool) Description() string          { return "Disconnect from a hardware device." }
func (t *disconnectTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *disconnectTool) Schema() map[string]any {
	return toolutil.Schema("Disconnect from a hardware device", []string{"device_id"}, map[string]any{
		"device_id": toolutil.SchemaProp("Device identifier", "string"),
	})
}
func (t *disconnectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := toolutil.RequireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	if err := Disconnect(ctx, deviceID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "disconnected", "device_id": deviceID}, nil
}

// hardware.send

type sendTool struct{}

func (t *sendTool) Name() string { return "hardware.send" }
func (t *sendTool) Description() string {
	return "Send a command or data to a connected hardware device."
}
func (t *sendTool) Permission() types.Permission { return types.PermissionUserApproved }
func (t *sendTool) Schema() map[string]any {
	return toolutil.Schema("Send command to hardware device", []string{"device_id", "command"}, map[string]any{
		"device_id": toolutil.SchemaProp("Device identifier", "string"),
		"command":   toolutil.SchemaProp("Command name (device-specific)", "string"),
		"payload":   toolutil.SchemaProp("Command payload (device-specific)", "object"),
	})
}
func (t *sendTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := toolutil.RequireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	command, err := toolutil.RequireStr(args, "command")
	if err != nil {
		return nil, err
	}
	payload := args["payload"]

	d, ok := GetInstance(deviceID)
	if !ok {
		return nil, fmt.Errorf("hardware: device %q not connected", deviceID)
	}
	result, err := d.Send(ctx, command, payload)
	if err != nil {
		return nil, fmt.Errorf("hardware: send to %s: %w", deviceID, err)
	}
	return map[string]any{"device_id": deviceID, "command": command, "result": result}, nil
}

// hardware.receive

type receiveTool struct{}

func (t *receiveTool) Name() string { return "hardware.receive" }
func (t *receiveTool) Description() string {
	return "Receive data or events from a connected hardware device."
}
func (t *receiveTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *receiveTool) Schema() map[string]any {
	return toolutil.Schema("Receive data from hardware device", []string{"device_id"}, map[string]any{
		"device_id":  toolutil.SchemaProp("Device identifier", "string"),
		"timeout_ms": toolutil.SchemaProp("Timeout in milliseconds (default 5000)", "number"),
	})
}
func (t *receiveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := toolutil.RequireStr(args, "device_id")
	if err != nil {
		return nil, err
	}

	d, ok := GetInstance(deviceID)
	if !ok {
		return nil, fmt.Errorf("hardware: device %q not connected", deviceID)
	}
	result, err := d.Receive(ctx)
	if err != nil {
		return nil, fmt.Errorf("hardware: receive from %s: %w", deviceID, err)
	}
	return map[string]any{"device_id": deviceID, "data": result}, nil
}

// hardware.info

type deviceInfoTool struct{}

func (t *deviceInfoTool) Name() string { return "hardware.info" }
func (t *deviceInfoTool) Description() string {
	return "Get runtime information from a connected hardware device (battery, firmware, etc.)."
}
func (t *deviceInfoTool) Permission() types.Permission { return types.PermissionReadOnly }
func (t *deviceInfoTool) Schema() map[string]any {
	return toolutil.Schema("Get device runtime info", []string{"device_id"}, map[string]any{
		"device_id": toolutil.SchemaProp("Device identifier", "string"),
	})
}
func (t *deviceInfoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := toolutil.RequireStr(args, "device_id")
	if err != nil {
		return nil, err
	}

	d, ok := GetInstance(deviceID)
	if !ok {
		return nil, fmt.Errorf("hardware: device %q not connected", deviceID)
	}
	info := d.Info(ctx)
	info["device_id"] = deviceID
	info["name"] = d.Name()
	info["state"] = string(d.State())
	return info, nil
}
