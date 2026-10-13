// Package hardware provides an abstraction layer for AI hardware devices.
//
// Devices are discovered and registered at startup. Each device exposes
// capabilities (sensors, actuators, streams) that map to Gleam tools.
//
// # Adding a new device
//
//  1. Implement the Device interface below
//  2. Call Register() in init() or from main.go
//  3. The device automatically appears in hardware.list and gains
//     hardware.send / hardware.receive tools
//
// # Example
//
//	type xiaohong struct{ port string }
//	func (x *xiaohong) ID() string { return "xiaohong" }
//	func (x *xiaohong) Name() string { return "AtomGit 小鸿" }
//	// ... implement other methods
//	func init() { hardware.Register(&xiaohong{}) }
package hardware

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// DeviceState represents the connection state of a device.
type DeviceState string

const (
	StateDisconnected DeviceState = "disconnected"
	StateConnecting   DeviceState = "connecting"
	StateConnected    DeviceState = "connected"
	StateError        DeviceState = "error"
)

// Capability describes what a device can do.
type Capability string

const (
	CapAudioIn    Capability = "audio_in"    // Microphone / audio input
	CapAudioOut   Capability = "audio_out"   // Speaker / audio output
	CapSensor     Capability = "sensor"      // Environmental sensors
	CapDisplay    Capability = "display"     // Screen / LED display
	CapButton     Capability = "button"      // Physical buttons
	CapStorage    Capability = "storage"     // Local storage / SD card
	CapNetwork    Capability = "network"     // WiFi / Ethernet
	CapUSB        Capability = "usb"         // USB host/device
	CapBLE        Capability = "ble"         // Bluetooth Low Energy
	CapTranscribe Capability = "transcribe"  // On-device speech-to-text
)

// Device is the interface every hardware device must implement.
type Device interface {
	// ID returns a unique identifier for this device type (e.g. "xiaohong", "qianwen_a2").
	ID() string

	// Name returns a human-readable name (e.g. "AtomGit 小鸿").
	Name() string

	// Description returns a brief description of the device.
	Description() string

	// Capabilities returns the list of capabilities this device supports.
	Capabilities() []Capability

	// Connect establishes a connection to the device. Called once before
	// any Send/Receive calls. Should be idempotent.
	Connect(ctx context.Context, config map[string]any) error

	// Disconnect releases resources. Called on shutdown or explicit disconnect.
	Disconnect(ctx context.Context) error

	// State returns the current connection state.
	State() DeviceState

	// Send transmits a command or data to the device.
	Send(ctx context.Context, command string, payload any) (any, error)

	// Receive reads data or events from the device. Returns nil if nothing available.
	Receive(ctx context.Context) (any, error)

	// Info returns runtime device info (battery, firmware version, etc.).
	Info(ctx context.Context) map[string]any
}

// registry holds all registered device drivers.
var (
	registryMu sync.RWMutex
	registry   = make(map[string]Device)
	instances  = make(map[string]Device) // active instances keyed by ID
)

// Register adds a device driver to the registry. Called at init time.
// Panics if a device with the same ID is already registered.
func Register(d Device) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[d.ID()]; exists {
		panic(fmt.Sprintf("hardware: device %q already registered", d.ID()))
	}
	registry[d.ID()] = d
}

// List returns all registered device drivers, sorted by ID.
func List() []Device {
	registryMu.RLock()
	defer registryMu.RUnlock()
	devices := make([]Device, 0, len(registry))
	for _, d := range registry {
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].ID() < devices[j].ID()
	})
	return devices
}

// Get returns a registered device driver by ID.
func Get(id string) (Device, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[id]
	return d, ok
}

// Connect activates a device instance. The device is stored in the instances map
// and becomes available for Send/Receive operations.
func Connect(ctx context.Context, id string, config map[string]any) error {
	registryMu.RLock()
	d, ok := registry[id]
	registryMu.RUnlock()
	if !ok {
		return fmt.Errorf("hardware: unknown device %q", id)
	}
	if err := d.Connect(ctx, config); err != nil {
		return fmt.Errorf("hardware: connect %s: %w", id, err)
	}
	registryMu.Lock()
	instances[id] = d
	registryMu.Unlock()
	return nil
}

// Disconnect deactivates a device instance.
func Disconnect(ctx context.Context, id string) error {
	registryMu.RLock()
	d, ok := instances[id]
	registryMu.RUnlock()
	if !ok {
		return fmt.Errorf("hardware: device %q not connected", id)
	}
	if err := d.Disconnect(ctx); err != nil {
		return fmt.Errorf("hardware: disconnect %s: %w", id, err)
	}
	registryMu.Lock()
	delete(instances, id)
	registryMu.Unlock()
	return nil
}

// GetInstance returns an active device instance by ID.
func GetInstance(id string) (Device, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := instances[id]
	return d, ok
}

// ListInstances returns all connected device instances, sorted by ID.
func ListInstances() []Device {
	registryMu.RLock()
	defer registryMu.RUnlock()
	devices := make([]Device, 0, len(instances))
	for _, d := range instances {
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].ID() < devices[j].ID()
	})
	return devices
}

// DisconnectAll disconnects all active device instances. Called on shutdown.
func DisconnectAll(ctx context.Context) {
	registryMu.RLock()
	ids := make([]string, 0, len(instances))
	for id := range instances {
		ids = append(ids, id)
	}
	registryMu.RUnlock()
	for _, id := range ids {
		Disconnect(ctx, id)
	}
}
