package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var sessionSecret []byte

func initSessionSecret() {
	sessionSecret = make([]byte, 32)
	_, _ = rand.Read(sessionSecret)
}

func makeSessionToken(user, pass string) string {
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(user + ":" + pass))
	return hex.EncodeToString(mac.Sum(nil))
}

func makeCaptchaToken(ansStr string) string {
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte("CAPTCHA:" + strings.TrimSpace(ansStr)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Replaces browser Basic Auth popup with Cookie Session & custom Login Page redirect
func checkSessionAuth(w http.ResponseWriter, r *http.Request, panelPath string) bool {
	if panelPath == "sentinel" && strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	if panelUser == "" && panelPass == "" {
		return true
	}
	if c, err := r.Cookie("ghq_session"); err == nil {
		expected := makeSessionToken(panelUser, panelPass)
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(expected)) == 1 {
			return true
		}
	}
	// Allow API fallback via BasicAuth if ever called programmatically
	if u, p, ok := r.BasicAuth(); ok {
		uOk := subtle.ConstantTimeCompare([]byte(u), []byte(panelUser)) == 1
		pOk := subtle.ConstantTimeCompare([]byte(p), []byte(panelPass)) == 1
		if uOk && pOk {
			return true
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return false
	}
	loginURL := "/login"
	if panelPath != "" {
		loginURL = "/" + panelPath + "/login"
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
	return false
}

func handleLoginRoute(w http.ResponseWriter, r *http.Request, panelPath string) {
	errMsg := ""
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		u := strings.TrimSpace(r.FormValue("username"))
		p := strings.TrimSpace(r.FormValue("password"))
		capAns := strings.TrimSpace(r.FormValue("captcha_ans"))
		capSig := strings.TrimSpace(r.FormValue("captcha_sig"))

		expectedSig := makeCaptchaToken(capAns)
		capValid := subtle.ConstantTimeCompare([]byte(capSig), []byte(expectedSig)) == 1
		uValid := subtle.ConstantTimeCompare([]byte(u), []byte(panelUser)) == 1
		pValid := subtle.ConstantTimeCompare([]byte(p), []byte(panelPass)) == 1

		if !capValid {
			errMsg = "❌ Incorrect math security answer. Please try again."
		} else if !uValid || !pValid {
			errMsg = "❌ Invalid username or password."
		} else {
			http.SetCookie(w, &http.Cookie{
				Name:     "ghq_session",
				Value:    makeSessionToken(panelUser, panelPass),
				Path:     "/",
				HttpOnly: true,
				MaxAge:   86400 * 30,
			})
			dest := "/"
			if panelPath != "" {
				dest = "/" + panelPath + "/"
			}
			http.Redirect(w, r, dest, http.StatusFound)
			return
		}
	}

	n1Big, _ := rand.Int(rand.Reader, big.NewInt(9))
	n2Big, _ := rand.Int(rand.Reader, big.NewInt(9))
	a := int(n1Big.Int64()) + 2
	b := int(n2Big.Int64()) + 1
	sig := makeCaptchaToken(strconv.Itoa(a + b))

	errBanner := ""
	if errMsg != "" {
		errBanner = fmt.Sprintf(`<div style="background:rgba(244,63,94,0.15);border:1px solid #f43f5e;color:#fda4af;padding:10px 12px;border-radius:10px;font-size:12px;margin-bottom:14px;">%s</div>`, errMsg)
	}

	actionURL := "/login"
	if panelPath != "" {
		actionURL = "/" + panelPath + "/login"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Sign In — Ghoghnoos-lite %s</title>
<style>
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#0c131f;color:#e2e8f0;font-family:system-ui,-apple-system,sans-serif;padding:16px;}
  .card{background:#131e2f;border:1px solid #1e2e46;border-radius:20px;max-width:390px;width:100%%;padding:28px;box-shadow:0 20px 50px rgba(0,0,0,0.6);}
  .logo{width:46px;height:46px;border-radius:14px;background:rgba(56,189,248,0.15);color:#38bdf8;display:flex;align-items:center;justify-content:center;font-size:24px;margin:0 auto 12px;}
  h2{text-align:center;margin:0;font-size:18px;color:#f8fafc;}
  .sub{text-align:center;font-size:12px;color:#94a3b8;margin-top:4px;margin-bottom:20px;font-family:monospace;}
  label{display:block;font-size:12px;color:#94a3b8;margin-bottom:5px;font-weight:600;}
  input{width:100%%;box-sizing:border-box;background:#080d14;border:1px solid #1e2e46;color:#f1f5f9;padding:10px 12px;border-radius:10px;font-size:13px;margin-bottom:14px;outline:none;}
  input:focus{border-color:#38bdf8;}
  .cap-box{background:#080d14;border:1px solid rgba(56,189,248,0.35);border-radius:12px;padding:12px;margin-bottom:18px;display:flex;align-items:center;justify-content:space-between;gap:10px;}
  .cap-q{font-family:monospace;font-size:14px;font-weight:bold;color:#38bdf8;white-space:nowrap;}
  .cap-in{margin-bottom:0;width:110px;text-align:center;font-family:monospace;font-weight:bold;}
  button{width:100%%;background:#38bdf8;color:#000;border:0;padding:11px;border-radius:12px;font-weight:bold;font-size:13px;cursor:pointer;transition:opacity 0.2s;}
  button:hover{opacity:0.9;}
</style></head><body>
<div class="card">
  <div class="logo">🦅</div>
  <h2>Ghoghnoos-lite Control Panel</h2>
  <div class="sub">LiteTun 3-Engine Relay • %s</div>
  %s
  <form method="POST" action="%s">
    <input type="hidden" name="captcha_sig" value="%s">
    <label>Panel Username</label>
    <input type="text" name="username" placeholder="admin" required autofocus>
    <label>Panel Password</label>
    <input type="password" name="password" placeholder="••••••••" required>
    <div class="cap-box">
      <span class="cap-q">🛡️ Security: %d + %d = ?</span>
      <input type="number" name="captcha_ans" class="cap-in" placeholder="Answer" required>
    </div>
    <button type="submit">🔒 Sign In to Dashboard</button>
  </form>
</div></body></html>`, appVersion, appVersion, errBanner, actionURL, sig, a, b)
}

func handleLogoutRoute(w http.ResponseWriter, r *http.Request, panelPath string) {
	http.SetCookie(w, &http.Cookie{Name: "ghq_session", Value: "", Path: "/", MaxAge: -1})
	loginURL := "/login"
	if panelPath != "" {
		loginURL = "/" + panelPath + "/login"
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func handleSelfUpdateRoute(w http.ResponseWriter, r *http.Request, panelPath string) {
	if !checkSessionAuth(w, r, panelPath) || r.Method != http.MethodPost {
		return
	}
	targetSrvID := r.URL.Query().Get("server_id")
	if targetSrvID != "" && targetSrvID != "local" {
		mu.Lock()
		srv := findServerByID(targetSrvID)
		mu.Unlock()
		if srv != nil && srv.IP != "" {
			client := &http.Client{Timeout: 4 * time.Second}
			if resp, err := client.Post(fmt.Sprintf("http://%s:%s/api/update", srv.IP, srv.PanelPort), "application/json", nil); err == nil {
				resp.Body.Close()
				addLog(fmt.Sprintf("UPDATE-OK [%s]: Triggered remote binary update on %s!", srv.Name, srv.IP))
				w.WriteHeader(http.StatusOK)
				return
			}
			mu.Lock()
			pendingRemoteCmds[srv.ID] = append(pendingRemoteCmds[srv.ID], "self-update")
			mu.Unlock()
			addLog(fmt.Sprintf("UPDATE-QUEUED [%s]: Queued binary self-update via Control Link.", srv.Name))
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	addLog("UPDATE: Downloading latest release binary from GitHub...")
	w.WriteHeader(http.StatusOK)
	go triggerLocalBinaryUpdate()
}

func triggerLocalBinaryUpdate() {
	time.Sleep(400 * time.Millisecond)
	cmd := "curl -Ls https://github.com/sahand363/ghoghnoos_light/releases/latest/download/ghoghnoos_lite -o /opt/litetun/ghoghnoos_lite.new && " +
		"chmod +x /opt/litetun/ghoghnoos_lite.new && " +
		"mv -f /opt/litetun/ghoghnoos_lite.new /opt/litetun/ghoghnoos_lite && " +
		"systemctl restart litetun"
	if err := exec.Command("bash", "-c", cmd).Run(); err != nil {
		addLog(fmt.Sprintf("UPDATE-ERR: Failed to download latest binary -> %v", err))
		return
	}
	os.Exit(0)
}
