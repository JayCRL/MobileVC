package e2ee

import "fmt"

const (
	RelayProtocolVersion  = 1
	TunnelProtocolVersion = 1
)

type CapabilitySet struct {
	RelayProtocolVersion     int    `json:"relayProtocolVersion"`
	E2EEProtocolVersion      int    `json:"e2eeProtocolVersion"`
	CryptoSuite              string `json:"cryptoSuite"`
	TunnelProtocolVersion    int    `json:"tunnelProtocolVersion"`
	SupportsMultiplexStreams bool   `json:"supportsMultiplexStreams"`
	SupportsFileDownload     bool   `json:"supportsFileDownloadStream"`
	SupportsDeviceManagement bool   `json:"supportsDeviceManagement"`
	RequiresE2EE             bool   `json:"requiresE2EE"`
	PlaintextTestMode        bool   `json:"plaintextTestMode"`
}

func ProductionCapabilities() CapabilitySet {
	return CapabilitySet{
		RelayProtocolVersion:     RelayProtocolVersion,
		E2EEProtocolVersion:      int(Version),
		CryptoSuite:              Suite,
		TunnelProtocolVersion:    TunnelProtocolVersion,
		SupportsMultiplexStreams: true,
		SupportsFileDownload:     true,
		SupportsDeviceManagement: true,
		RequiresE2EE:             true,
		PlaintextTestMode:        false,
	}
}

func PlaintextTestCapabilities() CapabilitySet {
	capabilities := ProductionCapabilities()
	capabilities.RequiresE2EE = false
	capabilities.PlaintextTestMode = true
	return capabilities
}

func ValidateProductionCapabilities(capabilities CapabilitySet) error {
	if err := validateCapabilityVersions(capabilities); err != nil {
		return err
	}
	if !capabilities.RequiresE2EE || capabilities.PlaintextTestMode {
		return fmt.Errorf("%w: e2ee production mode required", ErrHandshakeFailed)
	}
	if !supportsRequiredTunnelFeatures(capabilities) {
		return fmt.Errorf("%w: missing required tunnel capability", ErrHandshakeFailed)
	}
	return nil
}

func ValidatePlaintextTestCapabilities(capabilities CapabilitySet) error {
	if err := validateCapabilityVersions(capabilities); err != nil {
		return err
	}
	if capabilities.RequiresE2EE || !capabilities.PlaintextTestMode {
		return fmt.Errorf("%w: plaintext test mode must be explicit", ErrHandshakeFailed)
	}
	return nil
}

func (c CapabilitySet) ApplyToHandshake(input HandshakeInput) HandshakeInput {
	input.RelayProtocolVersion = c.RelayProtocolVersion
	input.E2EEProtocolVersion = c.E2EEProtocolVersion
	input.TunnelProtocolVersion = c.TunnelProtocolVersion
	input.CryptoSuite = c.CryptoSuite
	input.RequiresE2EE = c.RequiresE2EE
	input.PlaintextTestMode = c.PlaintextTestMode
	input.SupportsMultiplexStreams = c.SupportsMultiplexStreams
	input.SupportsFileDownload = c.SupportsFileDownload
	input.SupportsDeviceManagement = c.SupportsDeviceManagement
	return input
}

func validateCapabilityVersions(capabilities CapabilitySet) error {
	if capabilities.RelayProtocolVersion != RelayProtocolVersion ||
		capabilities.E2EEProtocolVersion != int(Version) ||
		capabilities.TunnelProtocolVersion != TunnelProtocolVersion ||
		capabilities.CryptoSuite != Suite {
		return fmt.Errorf("%w: unsupported capability version", ErrHandshakeFailed)
	}
	return nil
}

func supportsRequiredTunnelFeatures(capabilities CapabilitySet) bool {
	return capabilities.SupportsMultiplexStreams &&
		capabilities.SupportsFileDownload &&
		capabilities.SupportsDeviceManagement
}
