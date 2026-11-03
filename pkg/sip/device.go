package sip

import "sync"

// Device indicates the device
type Device struct {
	DeviceType string
	SipAddress *Address
}

// DeviceManager handles a devices for a defined user.
type DeviceManager struct {
	m       sync.Mutex
	devices []*Device
}

// AddDevice adds a new online device
func (d *DeviceManager) AddDevice(device *Device) {
	d.m.Lock()
	defer d.m.Unlock()
	d.devices = append(d.devices, device)
}
