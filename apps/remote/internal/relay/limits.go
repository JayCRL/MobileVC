package relay

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

func parseTrustedProxyCIDRs(raw string) ([]*net.IPNet, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	cidrs := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		_, cidr, err := net.ParseCIDR(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs, nil
}

func (s *Server) clientIP(r *http.Request) string {
	socketIP := remoteIP(r.RemoteAddr)
	if !s.isTrustedProxy(socketIP) {
		return socketIP
	}
	forwarded := firstForwardedIP(r)
	if forwarded == "" {
		return socketIP
	}
	return forwarded
}

func (s *Server) isTrustedProxy(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	for _, cidr := range s.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func firstForwardedIP(r *http.Request) string {
	candidates := []string{
		strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0],
		r.Header.Get("X-Real-IP"),
	}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if net.ParseIP(strings.Trim(trimmed, "[]")) != nil {
			return trimmed
		}
	}
	return ""
}

func (s *Server) acquireConn(role peerRole, remote string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connCountByIP[remote] >= s.cfg.MaxConnsPerIP {
		return errors.New("per-ip relay connection capacity reached")
	}
	if role == roleAgent && s.agentConns >= s.cfg.MaxAgentConns {
		return errors.New("agent connection capacity reached")
	}
	if role == roleClient && s.clientConns >= s.cfg.MaxClientConns {
		return errors.New("client connection capacity reached")
	}
	s.connCountByIP[remote]++
	if role == roleAgent {
		s.agentConns++
	} else {
		s.clientConns++
	}
	return nil
}

func (s *Server) releaseConn(role peerRole, remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connCountByIP[remote] > 0 {
		s.connCountByIP[remote]--
	}
	if role == roleAgent && s.agentConns > 0 {
		s.agentConns--
	}
	if role == roleClient && s.clientConns > 0 {
		s.clientConns--
	}
}
