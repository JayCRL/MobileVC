package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	SelectedRouteHTTPDownload = "/download"
	SelectedRouteWSMobileVC   = "/ws"
)

type SelectedRoutePolicy struct {
	HTTPAllowedRoutes []RouteRule
	WSAllowedRoutes   []RouteRule
}

var (
	ErrSelectedRouteE2EERequired = errors.New(CodeE2EERequired)
	ErrSelectedRouteDenied       = errors.New(CodeDownloadDenied)
	ErrSelectedRouteProtocol     = errors.New(CodeProtocolError)
)

func (s *Server) HTTPRouteAllowed(method string, path string) bool {
	return routeAllowed(s.cfg.HTTPAllowedRoutes, method, path)
}

func (s *Server) WSRouteAllowed(method string, path string) bool {
	return routeAllowed(s.cfg.WSAllowedRoutes, method, path)
}

func (c Config) SelectedRoutePolicy() SelectedRoutePolicy {
	return NewSelectedRoutePolicy(c.HTTPAllowedRoutes, c.WSAllowedRoutes)
}

func NewSelectedRoutePolicy(httpRoutes []RouteRule, wsRoutes []RouteRule) SelectedRoutePolicy {
	return SelectedRoutePolicy{
		HTTPAllowedRoutes: normalizeRouteRules(httpRoutes),
		WSAllowedRoutes:   normalizeRouteRules(wsRoutes),
	}
}

func DefaultSelectedRoutePolicy() SelectedRoutePolicy {
	policy, err := SelectedRoutePolicyFromAllowlists(defaultHTTPAllowedRoutes, defaultWSAllowedRoutes)
	if err != nil {
		panic(err)
	}
	return policy
}

func SelectedRoutePolicyFromAllowlists(httpAllowlist string, wsAllowlist string) (SelectedRoutePolicy, error) {
	httpRules, err := parseRouteRules(defaultString(httpAllowlist, defaultHTTPAllowedRoutes))
	if err != nil {
		return SelectedRoutePolicy{}, fmt.Errorf("http selected route allowlist: %w", err)
	}
	wsRules, err := parseRouteRules(defaultString(wsAllowlist, defaultWSAllowedRoutes))
	if err != nil {
		return SelectedRoutePolicy{}, fmt.Errorf("ws selected route allowlist: %w", err)
	}
	return NewSelectedRoutePolicy(httpRules, wsRules), nil
}

func (p SelectedRoutePolicy) IsZero() bool {
	return p.HTTPAllowedRoutes == nil && p.WSAllowedRoutes == nil
}

func (p SelectedRoutePolicy) HTTPRouteAllowed(method string, path string) bool {
	return routeAllowed(p.HTTPAllowedRoutes, method, path)
}

func (p SelectedRoutePolicy) WSRouteAllowed(method string, path string) bool {
	return routeAllowed(p.WSAllowedRoutes, method, path)
}

func (p SelectedRoutePolicy) StreamAllowed(streamType string) bool {
	switch strings.TrimSpace(streamType) {
	case "file.download":
		return p.HTTPRouteAllowed(http.MethodGet, SelectedRouteHTTPDownload)
	case "mobilevc.ws":
		return p.WSRouteAllowed(http.MethodGet, SelectedRouteWSMobileVC)
	default:
		return false
	}
}

func (p SelectedRoutePolicy) ValidateStream(streamType string) error {
	switch strings.TrimSpace(streamType) {
	case "file.download":
		if !p.HTTPRouteAllowed(http.MethodGet, SelectedRouteHTTPDownload) {
			return fmt.Errorf("%w: selected route denied for file.download", ErrSelectedRouteDenied)
		}
		return nil
	case "mobilevc.ws":
		if !p.WSRouteAllowed(http.MethodGet, SelectedRouteWSMobileVC) {
			return fmt.Errorf("%w: selected route denied for mobilevc.ws", ErrSelectedRouteProtocol)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported selected route stream type %s", ErrSelectedRouteProtocol, streamType)
	}
}

func SelectedRouteErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrSelectedRouteE2EERequired):
		return CodeE2EERequired
	case errors.Is(err, ErrSelectedRouteDenied):
		return CodeDownloadDenied
	case errors.Is(err, ErrSelectedRouteProtocol):
		return CodeProtocolError
	default:
		return CodeProtocolError
	}
}

func routeAllowed(rules []RouteRule, method string, path string) bool {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	normalizedPath := cleanRoutePath(path)
	for _, rule := range rules {
		if rule.Method == normalizedMethod && rule.Path == normalizedPath {
			return true
		}
	}
	return false
}

func cleanRoutePath(path string) string {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return "/"
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	if normalized != "/" {
		normalized = strings.TrimRight(normalized, "/")
	}
	return normalized
}

func methodPathFromRequest(r *http.Request) (string, string) {
	if r == nil || r.URL == nil {
		return "", ""
	}
	return r.Method, r.URL.Path
}

func normalizeRouteRules(rules []RouteRule) []RouteRule {
	if rules == nil {
		return nil
	}
	out := make([]RouteRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, normalizeRouteRule(rule))
	}
	return out
}

func normalizeRouteRule(rule RouteRule) RouteRule {
	path := strings.TrimSpace(rule.Path)
	if path != "" {
		path = cleanRoutePath(path)
	}
	return RouteRule{
		Method: strings.ToUpper(strings.TrimSpace(rule.Method)),
		Path:   path,
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
