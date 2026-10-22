package mount

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

const quarkDownloadUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4 Safari/537.36"

func trustedDownloadHost(address string) bool {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{"quark.cn", "uc.cn", "aliyuncs.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// Quark download URLs require server-held cookies. Never expose them to the UI.
func (s *Service) DownloadQuark(ctx context.Context, id, parent, fid, rangeHeader string) (*http.Response, error) {
	listing, err := s.List(ctx, id, parent, false)
	if err != nil {
		return nil, err
	}
	var entry *Entry
	for _, e := range listing.Items {
		if e.ID == fid && !e.Directory {
			copy := e
			entry = &copy
			break
		}
	}
	if entry == nil {
		return nil, errors.New("文件不在当前挂载目录中")
	}
	a, c, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Provider != "quark" {
		return nil, errors.New("此网盘的下载尚未接入")
	}
	var out map[string]any
	encoded, _ := json.Marshal(map[string]any{"fids": []string{fid}})
	body := string(encoded)
	err = s.jsonRequest(ctx, "POST", "https://drive-pc.quark.cn/1/clouddrive/file/download?pr=ucpro&fr=pc", strings.NewReader(body), map[string]string{"Content-Type": "application/json", "Cookie": c.Cookie, "Referer": "https://pan.quark.cn/", "User-Agent": quarkDownloadUA}, &out)
	if err != nil {
		return nil, err
	}
	if number(out["status"]) != 200 {
		return nil, remoteError("夸克", number(out["code"]))
	}
	data := list(out["data"])
	if len(data) == 0 {
		return nil, errors.New("网盘未返回下载地址")
	}
	address := str(object(data[0])["download_url"])
	if !trustedDownloadHost(address) {
		return nil, errors.New("网盘返回了不支持的下载地址")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", quarkDownloadUA)
	req.Header.Set("Referer", "https://pan.quark.cn/")
	addCookie := func(r *http.Request) {
		h := r.URL.Hostname()
		if h == "quark.cn" || strings.HasSuffix(h, ".quark.cn") {
			r.Header.Set("Cookie", c.Cookie)
		} else {
			r.Header.Del("Cookie")
		}
	}
	addCookie(req)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	client := *s.client
	client.Timeout = 0
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !trustedDownloadHost(r.URL.String()) {
			return errors.New("下载跳转地址不受支持")
		}
		addCookie(r)
		return nil
	}
	// The request context cancels streaming when the browser disconnects.
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("无法读取网盘文件，请稍后重试")
	}
	if response.StatusCode != 200 && response.StatusCode != 206 && response.StatusCode != 416 {
		response.Body.Close()
		return nil, upstreamStatus(response.StatusCode)
	}
	response.Header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": entry.Name}))
	return response, nil
}
