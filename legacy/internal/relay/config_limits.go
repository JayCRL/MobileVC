package relay

func loadRegisterTimeout(cfg *limitConfig) error {
	value, err := getEnvDuration("RELAY_AGENT_REGISTER_TIMEOUT", defaultAgentRegisterTimeout)
	if err != nil {
		return err
	}
	cfg.AgentRegisterTimeout = value
	return nil
}

func loadConnectionLimits(cfg *limitConfig) error {
	pairingFailures, err := getEnvInt("RELAY_PAIRING_MAX_FAILURES_PER_SESSION_IP", defaultMaxPairingFailuresPerIP)
	if err != nil {
		return err
	}
	cfg.MaxPairingFailuresPerIP = pairingFailures
	sessions, err := getEnvInt("RELAY_MAX_SESSIONS", defaultMaxSessions)
	if err != nil {
		return err
	}
	cfg.MaxSessions = sessions
	agentConns, err := getEnvInt("RELAY_MAX_AGENT_CONNS", defaultMaxAgentConns)
	if err != nil {
		return err
	}
	cfg.MaxAgentConns = agentConns
	clientConns, err := getEnvInt("RELAY_MAX_CLIENT_CONNS", defaultMaxClientConns)
	if err != nil {
		return err
	}
	cfg.MaxClientConns = clientConns
	perIPConns, err := getEnvInt("RELAY_MAX_CONNS_PER_IP", defaultMaxConnsPerIP)
	if err != nil {
		return err
	}
	cfg.MaxConnsPerIP = perIPConns
	return nil
}

func loadPingSettings(cfg *limitConfig) error {
	pingInterval, err := getEnvDuration("RELAY_PING_INTERVAL", defaultPingInterval)
	if err != nil {
		return err
	}
	pongTimeout, err := getEnvDuration("RELAY_PONG_TIMEOUT", defaultPongTimeout)
	if err != nil {
		return err
	}
	cfg.PingInterval = pingInterval
	cfg.PongTimeout = pongTimeout
	return nil
}

func loadFrameLimits(cfg *limitConfig) error {
	controlBytes, err := getEnvBytes("RELAY_MAX_CONTROL_FRAME_BYTES", defaultMaxControlFrameBytes)
	if err != nil {
		return err
	}
	payloadBytes, err := getEnvBytes("RELAY_MAX_PAYLOAD_BYTES", defaultMaxPayloadBytes)
	if err != nil {
		return err
	}
	queueSize, err := getEnvInt("RELAY_FORWARD_QUEUE_SIZE", defaultForwardQueueSize)
	if err != nil {
		return err
	}
	cfg.MaxControlFrameBytes = int64(controlBytes)
	cfg.MaxPayloadBytes = payloadBytes
	cfg.ForwardQueueSize = queueSize
	return nil
}
