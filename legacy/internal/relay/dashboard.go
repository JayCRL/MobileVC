package relay

import (
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type relayStats struct {
	GeneratedAt       time.Time          `json:"generatedAt"`
	AgentConnections  int                `json:"agentConnections"`
	ClientConnections int                `json:"clientConnections"`
	TotalConnections  int                `json:"totalConnections"`
	ActiveSessions    int                `json:"activeSessions"`
	Reconnectable     int                `json:"reconnectableSessions"`
	Systems           map[string]int     `json:"systems"`
	ConnectionsByIP   map[string]int     `json:"connectionsByIp"`
	Sessions          []relaySessionStat `json:"sessions"`
}

type relaySessionStat struct {
	ID              string            `json:"id"`
	AgentOnline     bool              `json:"agentOnline"`
	ClientOnline    bool              `json:"clientOnline"`
	ClientID        string            `json:"clientId,omitempty"`
	AgentSystem     string            `json:"agentSystem,omitempty"`
	AgentDevice     string            `json:"agentDevice,omitempty"`
	ClientSystem    string            `json:"clientSystem,omitempty"`
	ClientDevice    string            `json:"clientDevice,omitempty"`
	AgentRemote     string            `json:"agentRemote,omitempty"`
	ClientRemote    string            `json:"clientRemote,omitempty"`
	AgentUptimeSec  int64             `json:"agentUptimeSec,omitempty"`
	ClientUptimeSec int64             `json:"clientUptimeSec,omitempty"`
	Devices         []relayDeviceStat `json:"devices,omitempty"`
}

type relayDeviceStat struct {
	ClientID    string `json:"clientId"`
	Name        string `json:"name,omitempty"`
	System      string `json:"system,omitempty"`
	LastSeenAgo string `json:"lastSeenAgo,omitempty"`
	Revoked     bool   `json:"revoked"`
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(s.snapshotStats())
}

func (s *Server) snapshotStats() relayStats {
	now := time.Now().UTC()
	stats := relayStats{
		GeneratedAt:     now,
		Systems:         map[string]int{},
		ConnectionsByIP: map[string]int{},
		Sessions:        []relaySessionStat{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats.AgentConnections = s.agentConns
	stats.ClientConnections = s.clientConns
	stats.TotalConnections = s.agentConns + s.clientConns
	stats.ActiveSessions = len(s.sessions)
	for ip, count := range s.connCountByIP {
		if count > 0 {
			stats.ConnectionsByIP[maskIP(ip)] = count
		}
	}
	for _, state := range s.sessions {
		if state == nil {
			continue
		}
		item := relaySessionStat{
			ID:           shortID(state.id),
			AgentOnline:  state.agent != nil,
			ClientOnline: state.client != nil,
			ClientID:     shortID(state.clientID),
		}
		if state.hasReconnectableDevices() {
			stats.Reconnectable++
		}
		if state.agent != nil {
			item.AgentRemote = maskIP(state.agent.remote)
			item.AgentSystem = state.agent.system
			item.AgentDevice = state.agent.deviceName
			item.AgentUptimeSec = int64(now.Sub(state.agent.connectedAt).Seconds())
			stats.Systems[defaultSystem(item.AgentSystem)]++
		}
		if state.client != nil {
			item.ClientRemote = maskIP(state.client.remote)
			item.ClientSystem = state.client.system
			item.ClientUptimeSec = int64(now.Sub(state.client.connectedAt).Seconds())
		}
		for _, device := range state.devices {
			if device == nil {
				continue
			}
			deviceStat := relayDeviceStat{
				ClientID: shortID(device.ClientID),
				Name:     device.Name,
				System:   inferSystem("", device.Name),
				Revoked:  device.Revoked,
			}
			if !device.LastSeenAt.IsZero() {
				deviceStat.LastSeenAgo = formatAgo(now.Sub(device.LastSeenAt))
			}
			if item.ClientDevice == "" && device.ClientID == state.clientID {
				item.ClientDevice = device.Name
				if item.ClientSystem == "" || item.ClientSystem == "Unknown" {
					item.ClientSystem = deviceStat.System
				}
			}
			item.Devices = append(item.Devices, deviceStat)
		}
		sort.Slice(item.Devices, func(i, j int) bool {
			return item.Devices[i].ClientID < item.Devices[j].ClientID
		})
		stats.Sessions = append(stats.Sessions, item)
	}
	sort.Slice(stats.Sessions, func(i, j int) bool {
		return stats.Sessions[i].ID < stats.Sessions[j].ID
	})
	return stats
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" && r.URL.Path != "/dashboard/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = relayDashboardTemplate.Execute(w, nil)
}

func inferSystem(userAgent string, deviceName string) string {
	value := strings.ToLower(userAgent + " " + deviceName)
	switch {
	case strings.Contains(value, "iphone") || strings.Contains(value, "ipad") || strings.Contains(value, "ios"):
		return "iOS"
	case strings.Contains(value, "android"):
		return "Android"
	case strings.Contains(value, "windows"):
		return "Windows"
	case strings.Contains(value, "mac os") || strings.Contains(value, "macos") || strings.Contains(value, "darwin"):
		return "macOS"
	case strings.Contains(value, "linux") || strings.Contains(value, "ubuntu"):
		return "Linux"
	default:
		return "Unknown"
	}
}

func defaultSystem(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:8] + "…" + value[len(value)-4:]
}

func maskIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) > 2 {
			return parts[0] + ":" + parts[1] + ":…"
		}
	}
	parts := strings.Split(value, ".")
	if len(parts) == 4 {
		return parts[0] + "." + parts[1] + "." + parts[2] + ".x"
	}
	return value
}

func formatAgo(duration time.Duration) string {
	if duration < time.Minute {
		return "刚刚"
	}
	if duration < time.Hour {
		return formatInt(int(duration.Minutes())) + " 分钟前"
	}
	if duration < 24*time.Hour {
		return formatInt(int(duration.Hours())) + " 小时前"
	}
	return formatInt(int(duration.Hours()/24)) + " 天前"
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

var relayDashboardTemplate = template.Must(template.New("relay-dashboard").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MobileVC Relay</title>
<style>
:root{color-scheme:dark;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b1020;color:#e5e7eb}
body{margin:0;padding:24px}
.wrap{max-width:1120px;margin:0 auto}
.top{display:flex;align-items:flex-end;justify-content:space-between;gap:16px;margin-bottom:20px}
h1{font-size:28px;margin:0}
.muted{color:#94a3b8;font-size:13px}
.grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:16px}
.card{background:rgba(15,23,42,.88);border:1px solid rgba(148,163,184,.18);border-radius:18px;padding:16px;box-shadow:0 18px 40px rgba(0,0,0,.22)}
.num{font-size:32px;font-weight:800;margin-top:6px}
.label{color:#94a3b8;font-size:13px}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.pill{border:1px solid rgba(148,163,184,.24);border-radius:999px;padding:5px 9px;color:#cbd5e1;font-size:12px;background:rgba(30,41,59,.55)}
table{width:100%;border-collapse:collapse;overflow:hidden;border-radius:16px}
th,td{text-align:left;padding:12px;border-bottom:1px solid rgba(148,163,184,.14);font-size:13px}
th{color:#94a3b8;font-weight:600;background:rgba(15,23,42,.95)}
tr:last-child td{border-bottom:0}
.ok{color:#86efac}.off{color:#fca5a5}
@media(max-width:820px){body{padding:14px}.grid{grid-template-columns:repeat(2,minmax(0,1fr))}.top{display:block}}
</style>
</head>
<body>
<main class="wrap">
<div class="top"><div><h1>MobileVC Relay</h1><div class="muted">实时连接统计，每 3 秒刷新</div></div><div class="muted" id="updated">加载中…</div></div>
<section class="grid">
<div class="card"><div class="label">总连接</div><div class="num" id="total">-</div></div>
<div class="card"><div class="label">Agent</div><div class="num" id="agent">-</div></div>
<div class="card"><div class="label">Client</div><div class="num" id="client">-</div></div>
<div class="card"><div class="label">会话</div><div class="num" id="sessions">-</div></div>
</section>
<section class="card" style="margin-bottom:16px"><div class="label">电脑端系统分布</div><div class="row" id="systems" style="margin-top:10px"></div></section>
<section class="card"><table><thead><tr><th>会话</th><th>Agent</th><th>Client</th><th>电脑系统</th><th>电脑名称</th><th>手机系统</th><th>Client ID</th><th>在线时长</th></tr></thead><tbody id="rows"></tbody></table></section>
</main>
<script>
const $=id=>document.getElementById(id);
function up(sec){if(!sec)return '-';if(sec<60)return sec+'s';if(sec<3600)return Math.floor(sec/60)+'m';return Math.floor(sec/3600)+'h '+Math.floor(sec%3600/60)+'m'}
async function refresh(){
  const res=await fetch('/stats',{cache:'no-store'});
  const data=await res.json();
  $('total').textContent=data.totalConnections;
  $('agent').textContent=data.agentConnections;
  $('client').textContent=data.clientConnections;
  $('sessions').textContent=data.activeSessions;
  $('updated').textContent='更新于 '+new Date(data.generatedAt).toLocaleTimeString();
  const systems=data.systems||{};
  $('systems').innerHTML=Object.keys(systems).length?Object.entries(systems).map(([k,v])=>'<span class="pill">'+k+': '+v+'</span>').join(''):'<span class="muted">暂无在线电脑端</span>';
  $('rows').innerHTML=(data.sessions||[]).map(s=>'<tr>'
    +'<td>'+(s.id||'-')+'</td>'
    +'<td class="'+(s.agentOnline?'ok':'off')+'">'+(s.agentOnline?'在线':'离线')+' '+(s.agentRemote||'')+'</td>'
    +'<td class="'+(s.clientOnline?'ok':'off')+'">'+(s.clientOnline?'在线':'离线')+' '+(s.clientRemote||'')+'</td>'
    +'<td>'+(s.agentSystem||'-')+'</td><td>'+(s.agentDevice||'-')+'</td><td>'+(s.clientSystem||'-')+'</td><td>'+(s.clientId||'-')+'</td>'
    +'<td>A '+up(s.agentUptimeSec)+' / C '+up(s.clientUptimeSec)+'</td>'
  +'</tr>').join('')||'<tr><td colspan="7" class="muted">暂无会话</td></tr>';
}
refresh();setInterval(refresh,3000);
</script>
</body>
</html>`))
