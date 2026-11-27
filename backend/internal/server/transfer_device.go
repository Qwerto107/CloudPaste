package server

import "cloudpaste/backend/internal/transfer"

type transferDeviceInput = transfer.DeviceInput
type transferDeviceBrand = transfer.DeviceBrand
type transferDevice = transfer.Device

func validateTransferDevice(input *transferDeviceInput) error {
	if err := input.Validate(); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

func parseTransferDevice(userAgent string, input *transferDeviceInput) *transferDevice {
	return transfer.ParseDevice(userAgent, input)
}
