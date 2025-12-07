package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// AdminPageData holds data for rendering the admin page
type AdminPageData struct {
	Databases   []DatabaseStatus
	LockedCount int
	TotalCount  int
	CPUUsage    string
	MemoryUsage string
}

// DatabaseStatus represents the status of a single database
type DatabaseStatus struct {
	Index       int
	ConnString  string
	IsLocked    bool
	Username    string
	LockedAt    string
	LockedSince string
}

// generateSessionID creates a random session ID
func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// isAdminLoggedIn checks if the request has a valid admin session
func (h *Handler) isAdminLoggedIn(req *http.Request) bool {
	cookie, err := req.Cookie("admin_session")
	if err != nil {
		return false
	}

	var lastActivity time.Time
	var exists bool
	h.withAdminSessionsRLock(func() {
		lastActivity, exists = h.adminSessions[cookie.Value]
	})

	if !exists {
		return false
	}

	// Session expires after 1 hour of inactivity
	if time.Since(lastActivity) > time.Hour {
		h.withAdminSessionsLock(func() {
			delete(h.adminSessions, cookie.Value)
		})
		return false
	}

	// Update last activity
	h.withAdminSessionsLock(func() {
		h.adminSessions[cookie.Value] = time.Now()
	})

	return true
}

func (h *Handler) handleAdmin(resp http.ResponseWriter, req *http.Request) {
	if !h.isAdminLoggedIn(req) {
		h.showLoginPage(resp, req, "")
		return
	}

	h.showAdminPage(resp, req)
}

func (h *Handler) showLoginPage(resp http.ResponseWriter, req *http.Request, errorMsg string) {
	tmpl := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>DB Locker Admin - Login</title>
    <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Cellipse cx='50' cy='25' rx='35' ry='12' fill='%23569cd6'/%3E%3Cellipse cx='50' cy='50' rx='35' ry='12' fill='%23569cd6'/%3E%3Cellipse cx='50' cy='75' rx='35' ry='12' fill='%23569cd6'/%3E%3Cpath d='M15 25 L15 75 Q15 87 50 87 Q85 87 85 75 L85 25' fill='none' stroke='%23569cd6' stroke-width='3'/%3E%3C/svg%3E">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: "Monaco", "Consolas", "Courier New", monospace;
            margin: 0;
            padding: 40px;
            background: #0a0a0a;
            color: #e8e8e8;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .login-container {
            max-width: 420px;
            width: 100%;
            background: #151515;
            padding: 40px;
            border-radius: 4px;
            border: 1px solid #3a3a3a;
        }
        .form-group { margin-bottom: 24px; }
        label {
            display: block;
            margin-bottom: 8px;
            font-weight: normal;
            color: #d4a0cf;
            font-size: 13px;
        }
        input[type="password"] {
            width: 100%;
            padding: 12px 14px;
            border: 1px solid #4a4a4a;
            background: #0a0a0a;
            color: #e8e8e8;
            border-radius: 4px;
            font-family: inherit;
            font-size: 14px;
            transition: all 0.2s;
        }
        input[type="password"]:focus {
            outline: none;
            border-color: #569cd6;
            background: rgba(86, 156, 214, 0.05);
        }
        button {
            background-color: transparent;
            color: #e8e8e8;
            padding: 12px 24px;
            border: 1px solid #4a4a4a;
            border-radius: 4px;
            cursor: pointer;
            width: 100%;
            font-family: inherit;
            font-weight: normal;
            font-size: 14px;
            transition: all 0.2s;
        }
        button:hover {
            border-color: #569cd6;
            color: #569cd6;
            background: rgba(86, 156, 214, 0.1);
        }
        .error {
            color: #EF4444;
            background: rgba(239, 68, 68, 0.15);
            padding: 12px;
            margin-bottom: 20px;
            border: 1px solid rgba(239, 68, 68, 0.3);
            border-radius: 4px;
            text-align: center;
            font-size: 13px;
        }
        h1 {
            text-align: left;
            color: #569cd6;
            margin-bottom: 30px;
            font-size: 20px;
            font-weight: normal;
            letter-spacing: 0.5px;
        }
    </style>
</head>
<body>
    <div class="login-container">
        <h1>⛃ DB Locker Admin</h1>
        {{if .ErrorMsg}}<div class="error">{{.ErrorMsg}}</div>{{end}}
        <form method="POST" action="/admin/login">
            <div class="form-group">
                <label for="password">&gt; Password:</label>
                <input type="password" id="password" name="password" required autofocus>
            </div>
            <button type="submit">Login</button>
        </form>
    </div>
</body>
</html>`

	t, err := template.New("login").Parse(tmpl)
	if err != nil {
		http.Error(resp, "Template error", http.StatusInternalServerError)
		return
	}

	data := struct {
		ErrorMsg string
	}{
		ErrorMsg: errorMsg,
	}

	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Execute(resp, data)
}

func (h *Handler) showAdminPage(resp http.ResponseWriter, req *http.Request) {
	// Collect database status
	var databases []DatabaseStatus

	lockedDbs := make(map[string]*LockInfo)
	h.withLocksRLock(func() {
		for k, v := range h.locks {
			lockedDbs[k] = v
		}
	})

	lockedCount := len(lockedDbs)
	totalCount := len(testDatabases)

	// Create sorted list of all databases
	var allConnStrings []string
	for connStr := range testDatabases {
		allConnStrings = append(allConnStrings, connStr)
	}
	sort.Strings(allConnStrings)

	for i, connStr := range allConnStrings {
		status := DatabaseStatus{
			Index:      i + 1,
			ConnString: connStr,
			IsLocked:   false,
		}

		if lockInfo, locked := lockedDbs[connStr]; locked {
			status.IsLocked = true
			status.Username = lockInfo.Username
			status.LockedAt = lockInfo.LockedAt.Format("2006-01-02 15:04:05")
			status.LockedSince = formatDuration(time.Since(lockInfo.LockedAt))
		}

		databases = append(databases, status)
	}

	// Get system information
	cpuUsage := getCPUUsage()
	memoryUsage := getMemoryUsage()

	tmpl := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>DB Locker ({{.LockedCount}}/{{.TotalCount}})</title>
    <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'%3E%3Cellipse cx='50' cy='25' rx='35' ry='12' fill='%23569cd6'/%3E%3Cellipse cx='50' cy='50' rx='35' ry='12' fill='%23569cd6'/%3E%3Cellipse cx='50' cy='75' rx='35' ry='12' fill='%23569cd6'/%3E%3Cpath d='M15 25 L15 75 Q15 87 50 87 Q85 87 85 75 L85 25' fill='none' stroke='%23569cd6' stroke-width='3'/%3E%3C/svg%3E">
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: "Monaco", "Consolas", "Courier New", monospace;
            margin: 0;
            padding: 0;
            background: #0a0a0a;
            color: #e8e8e8;
            min-height: 100vh;
        }
        .container { max-width: 1600px; margin: 0 auto; padding: 0 20px 20px 20px; }
        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 30px;
            padding: 17px 0 20px 0;
            border-bottom: 1px solid #3a3a3a;
        }
        h1 {
            color: #569cd6;
            font-size: 18px;
            font-weight: normal;
            letter-spacing: 0.5px;
        }
        .header-actions { display: flex; gap: 12px; align-items: center; }
        .last-refresh {
            color: #9a9a9a;
            font-size: 12px;
            margin-right: 12px;
        }
        .auto-refresh-toggle {
            background: transparent;
            color: #e8e8e8;
            border: 1px solid #4a4a4a;
            padding: 8px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-family: inherit;
            font-size: 13px;
            transition: all 0.2s;
        }
        .auto-refresh-toggle:hover {
            border-color: #569cd6;
            color: #569cd6;
            background: rgba(86, 156, 214, 0.1);
        }
        .auto-refresh-toggle.active {
            background: rgba(78, 201, 176, 0.2);
            border-color: #4ec9b0;
            color: #4ec9b0;
            animation: pulse 2s ease-in-out infinite;
        }
        @keyframes pulse {
            0%, 100% {
                background: rgba(78, 201, 176, 0.15);
                box-shadow: 0 0 0 rgba(78, 201, 176, 0);
            }
            50% {
                background: rgba(78, 201, 176, 0.25);
                box-shadow: 0 0 12px rgba(78, 201, 176, 0.4);
            }
        }
        .logout {
            background: transparent;
            color: #e8e8e8;
            padding: 8px 16px;
            text-decoration: none;
            border-radius: 4px;
            border: 1px solid #4a4a4a;
            font-family: inherit;
            font-size: 13px;
            transition: all 0.2s;
        }
        .logout:hover {
            border-color: #e5a57a;
            color: #e5a57a;
            background: rgba(229, 165, 122, 0.1);
        }
        .system-info {
            margin-bottom: 24px;
            padding: 16px;
            background: #151515;
            border: 1px solid #3a3a3a;
            border-radius: 4px;
        }
        .system-info h3 {
            color: #d4a0cf;
            margin-bottom: 12px;
            font-size: 13px;
            font-weight: normal;
        }
        .system-info p {
            margin: 6px 0;
            color: #e8e8e8;
            font-size: 12px;
        }
        .system-info p strong {
            color: #569cd6;
        }
        .unlock-username-section {
            margin-bottom: 24px;
            padding: 16px;
            background: #151515;
            border: 1px solid #3a3a3a;
            border-radius: 4px;
        }
        .unlock-username-section h3 {
            color: #d4a0cf;
            margin-bottom: 12px;
            font-size: 13px;
            font-weight: normal;
        }
        .unlock-username-form { display: flex; gap: 10px; align-items: center; }
        .unlock-username-form input[type="text"] {
            padding: 8px 12px;
            border: 1px solid #4a4a4a;
            background: #0a0a0a;
            color: #e8e8e8;
            border-radius: 4px;
            flex: 1;
            max-width: 300px;
            font-family: inherit;
            font-size: 13px;
            transition: all 0.2s;
        }
        .unlock-username-form input[type="text"]:focus {
            outline: none;
            border-color: #569cd6;
            background: rgba(86, 156, 214, 0.05);
        }
        .unlock-username-form button {
            background: transparent;
            color: #e8e8e8;
            padding: 8px 16px;
            border: 1px solid #4a4a4a;
            border-radius: 4px;
            cursor: pointer;
            font-family: inherit;
            font-size: 13px;
            transition: all 0.2s;
        }
        .unlock-username-form button:hover {
            border-color: #569cd6;
            color: #569cd6;
            background: rgba(86, 156, 214, 0.1);
        }
        table {
            border-collapse: collapse;
            width: 100%;
            background: #3a3a3a;
            border: 1px solid #3a3a3a;
            border-radius: 4px;
            overflow: hidden;
        }
        th, td {
            border: 1px solid #3a3a3a;
            padding: 12px 14px;
            text-align: left;
            font-size: 12px;
        }
        th {
            background: #0a0a0a;
            color: #569cd6;
            font-weight: normal;
            border-bottom: 1px solid #3a3a3a;
        }
        tr {
            transition: all 0.15s;
        }
        tr:hover {
            background: rgba(0, 0, 0, 0.5) !important;
        }
        .locked {
            background: #151515;
        }
        .unlocked {
            background: #151515;
        }
        .force-unlock {
            background: transparent;
            color: #EF4444;
            border: 1px solid #EF4444;
            padding: 4px 10px;
            border-radius: 3px;
            cursor: pointer;
            font-family: inherit;
            font-size: 11px;
            transition: all 0.2s;
        }
        .force-unlock:hover {
            background: rgba(239, 68, 68, 0.2);
            color: #EF4444;
            border-color: #EF4444;
        }
        .status-locked {
            background: #EF4444;
            color: #fff;
            padding: 5px 12px;
            border-radius: 3px;
            font-weight: bold;
            display: inline-block;
            font-size: 11px;
            border: none;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
        }
        .status-unlocked {
            background: #4CAF50;
            color: #fff;
            padding: 5px 12px;
            border-radius: 3px;
            font-weight: bold;
            display: inline-block;
            font-size: 11px;
            border: none;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.3);
        }
        .conn-string {
            color: #e5a57a;
            font-size: 12px;
            max-width: 400px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .copy-btn {
            background: transparent;
            border: none;
            cursor: pointer;
            font-size: 14px;
            padding: 2px 4px;
            margin-left: 6px;
            opacity: 0.6;
            transition: opacity 0.2s;
            vertical-align: middle;
        }
        .copy-btn:hover {
            opacity: 1;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>⛃ DB Locker ({{.LockedCount}}/{{.TotalCount}})</h1>
            <div class="header-actions">
                <span id="lastRefresh" class="last-refresh"></span>
                <button id="autoRefreshBtn" class="auto-refresh-toggle" onclick="toggleAutoRefresh()">
                    Auto-refresh: OFF
                </button>
                <a href="/admin/logout" class="logout">Logout</a>
            </div>
        </div>

        <div class="system-info">
            <h3>&gt; System Resources</h3>
            <p><strong>CPU:</strong> {{.CPUUsage}}</p>
            <p><strong>Memory:</strong> {{.MemoryUsage}}</p>
        </div>

        <div class="unlock-username-section">
            <h3>&gt; Unlock All Databases by Username</h3>
            <form method="POST" action="/admin/unlock-by-username" class="unlock-username-form">
                <input type="text" name="username" placeholder="Enter username" required>
                <button type="submit" onclick="return confirm('Are you sure you want to unlock all databases locked by this user?')">Unlock All by Username</button>
            </form>
        </div>

        <table>
            <thead>
                <tr>
                    <th>#</th>
                    <th>Connection String</th>
                    <th>Status</th>
                    <th>Username</th>
                    <th>Locked At</th>
                    <th>Duration</th>
                    <th>Action</th>
                </tr>
            </thead>
            <tbody>
                {{range .Databases}}
                <tr class="{{if .IsLocked}}locked{{else}}unlocked{{end}}">
                    <td>{{.Index}}</td>
                    <td class="conn-string" title="{{.ConnString}}">{{.ConnString}}</td>
                    <td>
                        {{if .IsLocked}}
                            <span class="status-locked">LOCKED</span>
                        {{else}}
                            <span class="status-unlocked">AVAILABLE</span>
                        {{end}}
                    </td>
                    <td>
                        {{if .Username}}
                            <span>{{.Username}}</span>
                            <button class="copy-btn" onclick="copyUsername('{{.Username}}')" title="Copy username">📋</button>
                        {{end}}
                    </td>
                    <td>{{.LockedAt}}</td>
                    <td>{{.LockedSince}}</td>
                    <td>
                        {{if .IsLocked}}
                            <form method="POST" action="/admin/force-unlock" style="display: inline;">
                                <input type="hidden" name="conn" value="{{.ConnString}}">
                                <button type="submit" class="force-unlock" onclick="return confirm('Are you sure you want to force unlock this database?')">Force Unlock</button>
                            </form>
                        {{else}}
                            <span style="color: #666;">-</span>
                        {{end}}
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>

    <script>
        let autoRefreshEnabled = false;
        let autoRefreshInterval = null;
        let autoRefreshTimeoutId = null;
        let lastRefreshTime = new Date();
        let refreshTimerInterval = null;

        function copyUsername(username) {
            navigator.clipboard.writeText(username).then(function() {
                // Visual feedback - could add a tooltip or flash effect
                console.log('Copied: ' + username);
            }).catch(function(err) {
                console.error('Failed to copy: ', err);
            });
        }

        function updateLastRefreshTime() {
            const now = new Date();
            const secondsAgo = Math.floor((now - lastRefreshTime) / 1000);
            const elem = document.getElementById('lastRefresh');

            if (secondsAgo < 60) {
                elem.textContent = 'Last refresh: ' + secondsAgo + 's ago';
            } else if (secondsAgo < 3600) {
                const minutes = Math.floor(secondsAgo / 60);
                const seconds = secondsAgo % 60;
                elem.textContent = 'Last refresh: ' + minutes + 'm ' + seconds + 's ago';
            } else {
                const hours = Math.floor(secondsAgo / 3600);
                const minutes = Math.floor((secondsAgo % 3600) / 60);
                elem.textContent = 'Last refresh: ' + hours + 'h ' + minutes + 'm ago';
            }
        }

        function startRefreshTimer() {
            if (refreshTimerInterval) {
                clearInterval(refreshTimerInterval);
            }
            updateLastRefreshTime();
            refreshTimerInterval = setInterval(updateLastRefreshTime, 1000);
        }

        function toggleAutoRefresh() {
            autoRefreshEnabled = !autoRefreshEnabled;
            const btn = document.getElementById('autoRefreshBtn');

            if (autoRefreshEnabled) {
                btn.classList.add('active');
                btn.textContent = 'Auto-refresh: ON (5s)';
                sessionStorage.setItem('autoRefreshEnabled', 'true');
                sessionStorage.setItem('autoRefreshStartTime', Date.now().toString());
                startAutoRefresh();
            } else {
                btn.classList.remove('active');
                btn.textContent = 'Auto-refresh: OFF';
                sessionStorage.removeItem('autoRefreshEnabled');
                sessionStorage.removeItem('autoRefreshStartTime');
                stopAutoRefresh();
            }
        }

        function startAutoRefresh() {
            if (autoRefreshInterval) {
                clearInterval(autoRefreshInterval);
            }
            if (autoRefreshTimeoutId) {
                clearTimeout(autoRefreshTimeoutId);
            }

            autoRefreshInterval = setInterval(function() {
                window.location.reload();
            }, 5000); // 5 seconds

            // Auto-disable after 30 minutes (1800000 milliseconds)
            autoRefreshTimeoutId = setTimeout(function() {
                autoRefreshEnabled = false;
                sessionStorage.removeItem('autoRefreshEnabled');
                sessionStorage.removeItem('autoRefreshStartTime');
                stopAutoRefresh();
                window.location.reload(); // Reload to update UI
            }, 1800000); // 30 minutes
        }

        function stopAutoRefresh() {
            if (autoRefreshInterval) {
                clearInterval(autoRefreshInterval);
                autoRefreshInterval = null;
            }
            if (autoRefreshTimeoutId) {
                clearTimeout(autoRefreshTimeoutId);
                autoRefreshTimeoutId = null;
            }
        }

        // Initialize the refresh timer and restore auto-refresh state when the page loads
        window.addEventListener('DOMContentLoaded', function() {
            startRefreshTimer();

            // Restore auto-refresh state from sessionStorage (per-tab storage)
            const savedAutoRefresh = sessionStorage.getItem('autoRefreshEnabled');
            const startTimeStr = sessionStorage.getItem('autoRefreshStartTime');

            if (savedAutoRefresh === 'true' && startTimeStr) {
                const startTime = parseInt(startTimeStr);
                const elapsed = Date.now() - startTime;

                // Only restore if less than 30 minutes have passed
                if (elapsed < 1800000) {
                    autoRefreshEnabled = true;
                    const btn = document.getElementById('autoRefreshBtn');
                    btn.classList.add('active');
                    btn.textContent = 'Auto-refresh: ON (5s)';

                    // Start with remaining time
                    if (autoRefreshInterval) {
                        clearInterval(autoRefreshInterval);
                    }
                    autoRefreshInterval = setInterval(function() {
                        window.location.reload();
                    }, 5000);

                    const remainingTime = 1800000 - elapsed;
                    autoRefreshTimeoutId = setTimeout(function() {
                        autoRefreshEnabled = false;
                        sessionStorage.removeItem('autoRefreshEnabled');
                        sessionStorage.removeItem('autoRefreshStartTime');
                        stopAutoRefresh();
                        window.location.reload();
                    }, remainingTime);
                } else {
                    // Time expired, clean up
                    sessionStorage.removeItem('autoRefreshEnabled');
                    sessionStorage.removeItem('autoRefreshStartTime');
                }
            }
        });
    </script>
</body>
</html>`

	t, err := template.New("admin").Parse(tmpl)
	if err != nil {
		http.Error(resp, "Template error", http.StatusInternalServerError)
		return
	}

	data := AdminPageData{
		Databases:   databases,
		LockedCount: lockedCount,
		TotalCount:  totalCount,
		CPUUsage:    cpuUsage,
		MemoryUsage: memoryUsage,
	}

	resp.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Execute(resp, data)
}

func (h *Handler) handleAdminLogin(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	password := req.FormValue("password")
	if password != dbLockerPassword {
		h.showLoginPage(resp, req, "Invalid password")
		return
	}

	// Create session
	sessionID := generateSessionID()
	h.withAdminSessionsLock(func() {
		h.adminSessions[sessionID] = time.Now()
	})

	// Set cookie
	http.SetCookie(resp, &http.Cookie{
		Name:     "admin_session",
		Value:    sessionID,
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
	})

	log.Info().Msg("Admin login successful")
	http.Redirect(resp, req, "/admin", http.StatusSeeOther)
}

func (h *Handler) handleAdminLogout(resp http.ResponseWriter, req *http.Request) {
	cookie, err := req.Cookie("admin_session")
	if err == nil {
		h.withAdminSessionsLock(func() {
			delete(h.adminSessions, cookie.Value)
		})
	}

	// Clear cookie
	http.SetCookie(resp, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		HttpOnly: true,
		Path:     "/",
		MaxAge:   -1,
	})

	log.Info().Msg("Admin logout")
	http.Redirect(resp, req, "/admin", http.StatusSeeOther)
}

func (h *Handler) handleAdminForceUnlock(resp http.ResponseWriter, req *http.Request) {
	if !h.isAdminLoggedIn(req) {
		http.Error(resp, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connStr := req.FormValue("conn")
	if connStr == "" {
		http.Error(resp, "Connection string required", http.StatusBadRequest)
		return
	}

	// Check if this database is actually locked
	var lockInfo *LockInfo
	var exists bool
	h.withLocksLock(func() {
		lockInfo, exists = h.locks[connStr]
		if exists {
			delete(h.locks, connStr)
		}
	})

	if !exists {
		log.Info().Str("connStr", connStr).Msg("ADMIN FORCE-UNLOCK attempted on unlocked database")
	} else {
		// Return the database to the available pool
		h.cLockedDbConn <- connStr
		log.Info().Str("connStr", connStr).Str("originalUser", lockInfo.Username).Msg("ADMIN FORCE-UNLOCK")
	}

	http.Redirect(resp, req, "/admin", http.StatusSeeOther)
}

func (h *Handler) handleAdminUnlockByUsername(resp http.ResponseWriter, req *http.Request) {
	if !h.isAdminLoggedIn(req) {
		http.Error(resp, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := req.FormValue("username")
	if username == "" {
		http.Error(resp, "Username required", http.StatusBadRequest)
		return
	}

	// Find all databases locked by this username and unlock them
	var unlockedDbs []string
	h.withLocksLock(func() {
		for connStr, lockInfo := range h.locks {
			if lockInfo.Username == username {
				delete(h.locks, connStr)
				unlockedDbs = append(unlockedDbs, connStr)
			}
		}
	})

	// Return the databases to the available pool after releasing the lock
	for _, connStr := range unlockedDbs {
		h.cLockedDbConn <- connStr
	}

	if len(unlockedDbs) == 0 {
		log.Info().Str("username", username).Msg("ADMIN UNLOCK-BY-USERNAME: No databases locked by this user")
	} else {
		log.Info().Str("username", username).Int("count", len(unlockedDbs)).Msg("ADMIN UNLOCK-BY-USERNAME")
	}

	http.Redirect(resp, req, "/admin", http.StatusSeeOther)
}

// cleanupExpiredSessions removes expired admin sessions
func (h *Handler) cleanupExpiredSessions() {
	ticker := time.NewTicker(10 * time.Minute) // Clean up every 10 minutes
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		h.withAdminSessionsLock(func() {
			for sessionID, lastActivity := range h.adminSessions {
				if now.Sub(lastActivity) > time.Hour {
					delete(h.adminSessions, sessionID)
					log.Info().Str("sessionID", sessionID).Msg("Admin session expired")
				}
			}
		})
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}

// getMemoryUsage returns current memory usage information
func getMemoryUsage() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Convert bytes to MB
	allocMB := m.Alloc / 1024 / 1024
	totalAllocMB := m.TotalAlloc / 1024 / 1024
	sysMB := m.Sys / 1024 / 1024

	return fmt.Sprintf("Alloc: %d MB, Total: %d MB, Sys: %d MB", allocMB, totalAllocMB, sysMB)
}

// getCPUUsage returns CPU usage information (simplified version)
func getCPUUsage() string {
	// Simple CPU usage estimate using goroutines and GC stats
	numGoroutines := runtime.NumGoroutine()
	numCPU := runtime.NumCPU()

	// Try to read /proc/loadavg on Linux for load average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			return fmt.Sprintf("Load: %s %s %s, CPUs: %d, Goroutines: %d",
				parts[0], parts[1], parts[2], numCPU, numGoroutines)
		}
	}

	// Fallback if /proc/loadavg is not available
	return fmt.Sprintf("CPUs: %d, Goroutines: %d", numCPU, numGoroutines)
}
