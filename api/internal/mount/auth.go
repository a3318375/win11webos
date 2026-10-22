package mount

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type authSession struct {
	mu         sync.Mutex
	Provider   string
	Expires    time.Time
	Status     string
	UpstreamID string
	Creds      Credentials
	Client     *http.Client
	Message    string
}

const quarkDesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

func quarkRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func quarkQRURL(token string) string {
	return "https://su.quark.cn/4_eMHBJ?" + url.Values{"token": {token}, "client_id": {"532"}, "ssb": {"weblogin"}, "uc_param_str": {""}, "uc_biz_str": {"S:custom|OPT:SAREA@0|OPT:IMMERSIVE@1|OPT:BACK_BTN_STYLE@0"}}.Encode()
}

type Authorization struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	URL       string    `json:"url,omitempty"`
	QR        string    `json:"qr,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	Message   string    `json:"message,omitempty"`
}

// RetainTest keeps refreshed/rotated credentials between testing and saving.
func (s *Service) RetainTest(provider string, c Credentials, existing string) Authorization {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	now := time.Now()
	for id, state := range s.sessions {
		if now.After(state.Expires) {
			delete(s.sessions, id)
		}
	}
	if existing != "" {
		if state := s.sessions[existing]; state != nil && state.Provider == provider {
			state.mu.Lock()
			state.Creds = c
			state.Status = "success"
			state.mu.Unlock()
			return Authorization{ID: existing, Status: "success", ExpiresAt: state.Expires}
		}
	}
	id := ID()
	if len(s.sessions) >= 64 {
		var oldest string
		var expiry time.Time
		for id, state := range s.sessions {
			if oldest == "" || state.Expires.Before(expiry) {
				oldest = id
				expiry = state.Expires
			}
		}
		delete(s.sessions, oldest)
	}
	expires := now.Add(5 * time.Minute)
	s.sessions[id] = &authSession{Provider: provider, Expires: expires, Status: "success", Creds: c}
	return Authorization{ID: id, Status: "success", ExpiresAt: expires}
}

func (s *Service) AuthAvailable(provider string) bool {
	return provider == "quark" || ((provider == "115" || provider == "baidu") && s.broker != "")
}
func (s *Service) StartAuth(ctx context.Context, provider string) (Authorization, error) {
	if !s.AuthAvailable(provider) {
		return Authorization{}, errors.New("自动授权尚未配置，请设置可信的 CLOUD_OAUTH_URL，或使用手动凭据")
	}
	s.authMu.Lock()
	for id, session := range s.sessions {
		if time.Now().After(session.Expires) {
			delete(s.sessions, id)
		}
	}
	if len(s.sessions) >= 64 {
		s.authMu.Unlock()
		return Authorization{}, errors.New("授权会话过多，请稍后重试")
	}
	s.authMu.Unlock()
	state := &authSession{Provider: provider, Expires: time.Now().Add(5 * time.Minute), Status: "pending"}
	out := Authorization{ID: ID(), Status: "pending", ExpiresAt: state.Expires}
	if provider == "quark" {
		jar, _ := cookiejar.New(nil)
		client := *s.client
		client.Jar = jar
		state.Client = &client
		query := url.Values{"client_id": {"532"}, "v": {"1.2"}, "request_id": {quarkRequestID()}}
		var response map[string]any
		if err := qrJSON(ctx, &client, "https://uop.quark.cn/cas/ajax/getTokenForQrcodeLogin?"+query.Encode(), &response); err != nil {
			return out, err
		}
		state.UpstreamID = str(object(object(response["data"])["members"])["token"])
		if number(response["status"]) != 2000000 || state.UpstreamID == "" {
			return out, errors.New("获取夸克二维码失败")
		}
		qrURL := quarkQRURL(state.UpstreamID)
		png, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
		if err != nil {
			return out, err
		}
		out.QR = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	} else {
		name := "115网盘Open"
		if provider == "baidu" {
			name = "百度网盘Open"
		}
		payload, _ := json.Marshal(map[string]any{"driver_type": name, "server_use": true, "callback_url": s.broker + "/callback-popup"})
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				ID  string `json:"session_id"`
				URL string `json:"oauth_url"`
			} `json:"data"`
		}
		if err := s.jsonRequest(ctx, "POST", s.broker+"/api/oauth/start", strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json"}, &response); err != nil {
			return out, err
		}
		u, err := url.Parse(response.Data.URL)
		if err != nil || u.Scheme != "https" || u.User != nil || !response.Success || response.Data.ID == "" {
			return out, errors.New("授权服务未返回有效的 HTTPS 授权地址")
		}
		state.UpstreamID = response.Data.ID
		out.URL = u.String()
	}
	s.authMu.Lock()
	s.sessions[out.ID] = state
	s.authMu.Unlock()
	return out, nil
}
func (s *Service) PollAuth(ctx context.Context, id string) (Authorization, error) {
	s.authMu.Lock()
	session := s.sessions[id]
	s.authMu.Unlock()
	if session == nil {
		return Authorization{}, errors.New("授权会话不存在，请重新开始")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	out := Authorization{ID: id, Status: session.Status, ExpiresAt: session.Expires, Message: session.Message}
	if time.Now().After(session.Expires) {
		session.Creds = Credentials{}
		session.Status = "expired"
		out.Status = "expired"
		return out, nil
	}
	if session.Status != "pending" {
		return out, nil
	}
	if session.Provider == "quark" {
		query := url.Values{"client_id": {"532"}, "v": {"1.2"}, "token": {session.UpstreamID}, "request_id": {quarkRequestID()}}
		var response map[string]any
		if err := qrJSON(ctx, session.Client, "https://uop.quark.cn/cas/ajax/getServiceTicketByQrcodeToken?"+query.Encode(), &response); err != nil {
			return out, err
		}
		ticket := str(object(object(response["data"])["members"])["service_ticket"])
		status := number(response["status"])
		if ticket != "" {
			var info map[string]any
			if err := qrJSON(ctx, session.Client, "https://pan.quark.cn/account/info?"+url.Values{"st": {ticket}, "lw": {"scan"}}.Encode(), &info); err != nil {
				return out, err
			}
			req, _ := http.NewRequestWithContext(ctx, "GET", "https://pan.quark.cn/", nil)
			req.Header.Set("User-Agent", quarkDesktopUA)
			resp, err := session.Client.Do(req)
			if err == nil {
				io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
				resp.Body.Close()
			}
			var bootstrap map[string]any
			_ = qrJSON(ctx, session.Client, "https://drive-pc.quark.cn/1/clouddrive/file/sort?pr=ucpro&fr=pc&pdir_fid=0&_page=1&_size=1", &bootstrap)
			cookies := map[string]string{}
			for _, host := range []string{"https://uop.quark.cn", "https://pan.quark.cn", "https://drive-pc.quark.cn"} {
				u, _ := url.Parse(host)
				for _, cookie := range session.Client.Jar.Cookies(u) {
					cookies[cookie.Name] = cookie.Value
				}
			}
			pairs := []string{}
			for name, value := range cookies {
				pairs = append(pairs, name+"="+value)
			}
			if len(pairs) == 0 {
				return out, errors.New("扫码完成但未取得网盘 Cookie，请重试")
			}
			session.Creds.Cookie = strings.Join(pairs, "; ")
			session.Status = "success"
		} else if status == 50004002 {
			session.Status = "expired"
			session.Message = "二维码已过期，请重新获取后使用夸克 App 扫码"
		} else if status == 50004003 || status == 50004004 {
			session.Status = "failed"
			session.Message = "扫码登录已取消或失败，请重新获取"
		} else if status != 50004001 && status != 2000000 {
			return out, fmt.Errorf("夸克扫码状态异常（%d），请重新获取二维码", status)
		}
	} else {
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Status string            `json:"status"`
				Token  map[string]string `json:"token_data"`
			} `json:"data"`
		}
		if err := s.jsonRequest(ctx, "GET", s.broker+"/api/oauth/status/"+url.PathEscape(session.UpstreamID), nil, nil, &response); err != nil {
			return out, err
		}
		if !response.Success {
			return out, errors.New("授权状态查询失败，请稍后重试")
		}
		if response.Data.Status == "success" {
			session.Creds = Credentials{Access: response.Data.Token["access_token"], Refresh: response.Data.Token["refresh_token"]}
			if session.Creds.Access == "" && session.Creds.Refresh == "" {
				return out, errors.New("授权结果缺少 Token")
			}
			session.Status = "success"
			var ignored any
			_ = s.jsonRequest(ctx, "POST", s.broker+"/api/oauth/confirm-received/"+url.PathEscape(session.UpstreamID), nil, nil, &ignored)
		} else if response.Data.Status == "error" {
			session.Status = "failed"
			out.Message = "授权失败，请重新开始"
		}
	}
	out.Status = session.Status
	out.Message = session.Message
	return out, nil
}
func (s *Service) CancelAuth(id string) { s.authMu.Lock(); delete(s.sessions, id); s.authMu.Unlock() }
func (s *Service) sessionCredentials(id, provider string) (Credentials, error) {
	s.authMu.Lock()
	state := s.sessions[id]
	s.authMu.Unlock()
	if state == nil {
		return Credentials{}, errors.New("授权会话已失效")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Provider != provider || state.Status != "success" || time.Now().After(state.Expires) {
		return Credentials{}, errors.New("授权未完成或已过期，请重新授权")
	}
	return state.Creds, nil
}
func qrJSON(ctx context.Context, client *http.Client, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", quarkDesktopUA)
	req.Header.Set("Referer", "https://pan.quark.cn/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Origin", "https://pan.quark.cn")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("夸克授权网络异常，请重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("夸克授权返回 HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	decoder.UseNumber()
	if err = decoder.Decode(out); err != nil {
		return errors.New("夸克授权响应异常")
	}
	return nil
}
