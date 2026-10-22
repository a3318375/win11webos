package mount

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type upstreamStatus int

func (e upstreamStatus) Error() string { return fmt.Sprintf("网盘服务返回 HTTP %d", e) }

func (s *Service) jsonRequest(ctx context.Context, method, address string, body io.Reader, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return errors.New("请求地址无效")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return errors.New("无法连接网盘服务，请检查网络后重试")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return upstreamStatus(response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	decoder.UseNumber()
	if err = decoder.Decode(out); err != nil {
		return errors.New("网盘响应格式异常")
	}
	return nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func list(v any) []any            { a, _ := v.([]any); return a }
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func number(v any) int64 { n, _ := strconv.ParseInt(str(v), 10, 64); return n }
func truth(v any) bool   { return v == true || str(v) == "1" }
func remoteError(provider string, code int64) error {
	switch code {
	case 401, 403, 110, 111, -6, 31045, 40140100, 40140115:
		return errors.New("授权无效或已过期，请重新授权")
	case -7, 20013, 31024:
		return errors.New("根目录不可访问，请检查应用授权范围或根目录设置")
	case 429, 20012, 31034:
		return errors.New("网盘请求过于频繁，请稍后重试")
	}
	return fmt.Errorf("%s 网盘请求失败（代码 %d），请检查授权与目录", provider, code)
}
func (s *Service) refresh(ctx context.Context, provider string, c Credentials) (Credentials, error) {
	if c.Refresh == "" {
		return c, errors.New("授权已过期，请重新获取 Token")
	}
	if provider == "115" {
		form := url.Values{"refresh_token": {c.Refresh}}
		var envelope map[string]any
		if err := s.jsonRequest(ctx, "POST", "https://passportapi.115.com/open/refreshToken", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, &envelope); err != nil {
			return c, err
		}
		data := object(envelope["data"])
		if !truth(envelope["state"]) || str(data["access_token"]) == "" {
			return c, errors.New("115 刷新授权失败，请重新授权")
		}
		c.Access = str(data["access_token"])
		if token := str(data["refresh_token"]); token != "" {
			c.Refresh = token
		}
		return c, nil
	}
	if s.broker == "" {
		return c, errors.New("百度 Token 需要刷新，请配置 CLOUD_OAUTH_URL 或重新填写访问令牌")
	}
	payload, _ := json.Marshal(map[string]string{"driver_type": "百度网盘Open", "refresh_token": c.Refresh})
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		} `json:"data"`
	}
	if err := s.jsonRequest(ctx, "POST", s.broker+"/api/oauth/refresh", strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json"}, &response); err != nil {
		return c, err
	}
	if !response.Success || response.Data.Access == "" {
		return c, errors.New("刷新授权失败，请重新获取 Token")
	}
	c.Access = response.Data.Access
	if response.Data.Refresh != "" {
		c.Refresh = response.Data.Refresh
	}
	return c, nil
}
func (s *Service) remoteList(ctx context.Context, a Account, c Credentials) ([]Entry, Credentials, error) {
	refreshNow := func() error {
		updated, err := s.refresh(ctx, a.Provider, c)
		if err != nil {
			return err
		}
		c = updated
		if a.ID != "" {
			return s.store.UpdateCredentials(ctx, a.ID, c)
		}
		return nil
	}
	if a.Provider != "quark" && c.Access == "" {
		err := refreshNow()
		if err != nil {
			return nil, c, err
		}
	}
	items := []Entry{}
	offset := 0
	refreshed := false
	for page := 1; page <= 200; page++ {
		if err := ctx.Err(); err != nil {
			return nil, c, err
		}
		query := url.Values{}
		headers := map[string]string{}
		address := ""
		switch a.Provider {
		case "115":
			address = "https://proapi.115.com/open/ufile/files"
			query = url.Values{"cid": {a.Config.Root}, "limit": {"200"}, "offset": {strconv.Itoa(offset)}, "show_dir": {"1"}}
			headers["Authorization"] = "Bearer " + c.Access
		case "baidu":
			address = "https://pan.baidu.com/rest/2.0/xpan/file"
			query = url.Values{"method": {"list"}, "dir": {a.Config.Root}, "start": {strconv.Itoa(offset)}, "limit": {"1000"}, "access_token": {c.Access}, "web": {"1"}}
		case "quark":
			address = "https://drive-pc.quark.cn/1/clouddrive/file/sort"
			query = url.Values{"pr": {"ucpro"}, "fr": {"pc"}, "pdir_fid": {a.Config.Root}, "_page": {strconv.Itoa(page)}, "_size": {"100"}, "_fetch_total": {"1"}}
			headers["Cookie"] = c.Cookie
			headers["Referer"] = "https://pan.quark.cn/"
			headers["User-Agent"] = "Mozilla/5.0"
		default:
			return nil, c, errors.New("不支持此网盘")
		}
		var envelope map[string]any
		if err := s.jsonRequest(ctx, "GET", address+"?"+query.Encode(), nil, headers, &envelope); err != nil {
			var status upstreamStatus
			if errors.As(err, &status) && (status == 401 || status == 403) && a.Provider != "quark" && !refreshed && c.Refresh != "" {
				if err = refreshNow(); err != nil {
					return nil, c, err
				}
				refreshed = true
				page--
				continue
			}
			return nil, c, err
		}
		var rows []any
		var code int64
		switch a.Provider {
		case "115":
			if !truth(envelope["state"]) {
				code = number(envelope["code"])
				if code == 0 {
					code = number(envelope["errno"])
				}
				if code == 0 {
					code = -1
				}
			}
			rows = list(envelope["data"])
		case "baidu":
			code = number(envelope["errno"])
			if _, ok := envelope["errno"]; !ok {
				code = number(envelope["error_code"])
				if code == 0 {
					code = -1
				}
			}
			rows = list(envelope["list"])
		case "quark":
			if number(envelope["status"]) != 200 {
				code = number(envelope["code"])
				if code == 0 {
					code = number(envelope["status"])
				}
				if code == 0 {
					code = -1
				}
			}
			rows = list(object(envelope["data"])["list"])
		}
		if code != 0 {
			if !refreshed && a.Provider != "quark" && c.Refresh != "" && (code == 110 || code == 111 || code == -6 || code == 31045 || code == 40140100) {
				err := refreshNow()
				if err != nil {
					return nil, c, err
				}
				refreshed = true
				page--
				continue
			}
			return nil, c, remoteError(a.Provider, code)
		}
		for _, row := range rows {
			m := object(row)
			var e Entry
			switch a.Provider {
			case "115":
				e.ID = str(m["fid"])
				e.Name = str(m["fn"])
				if e.Name == "" {
					e.Name = str(m["file_name"])
				}
				category := str(m["file_category"])
				if category == "" {
					category = str(m["fc"])
				}
				e.Directory = category == "0"
				if category == "" {
					e.Directory = str(m["file_category"]) == "0"
				}
				if e.ID == "" {
					e.ID = str(m["file_id"])
				}
				e.Size = number(m["fs"])
				if e.Size == 0 {
					for _, key := range []string{"size_byte", "s", "size"} {
						if e.Size = number(m[key]); e.Size > 0 {
							break
						}
					}
				}
				e.ModifiedAt = time.Unix(number(m["upt"]), 0)
			case "baidu":
				e.ID = str(m["path"])
				e.Name = str(m["server_filename"])
				e.Directory = truth(m["isdir"])
				e.Size = number(m["size"])
				e.ModifiedAt = time.Unix(number(m["server_mtime"]), 0)
			case "quark":
				e.ID = str(m["fid"])
				e.Name = str(m["file_name"])
				e.Directory = truth(m["dir"]) || str(m["file_type"]) == "0"
				e.Size = number(m["size"])
				e.ModifiedAt = time.UnixMilli(number(m["updated_at"]))
				if timestamp := number(m["updated_at"]); timestamp > 0 && timestamp < 1e12 {
					e.ModifiedAt = time.Unix(timestamp, 0)
				}
			}
			if e.ID == "" || e.Name == "" {
				return nil, c, errors.New("网盘返回的文件标识或名称缺失")
			}
			e.Kind = kind(e.Name)
			if e.Directory {
				e.Kind = "folder"
			}
			items = append(items, e)
		}
		offset += len(rows)
		limit := 200
		if a.Provider == "baidu" {
			limit = 1000
		} else if a.Provider == "quark" {
			limit = 100
		}
		if len(rows) < limit {
			return items, c, nil
		}
		if a.Provider == "115" && offset >= int(number(envelope["count"])) && number(envelope["count"]) > 0 {
			return items, c, nil
		}
		if a.Provider == "quark" && int64(offset) >= number(object(envelope["metadata"])["_total"]) && number(object(envelope["metadata"])["_total"]) > 0 {
			return items, c, nil
		}
	}
	return nil, c, errors.New("目录过大，超过本版读取上限，请挂载更小的子目录")
}
